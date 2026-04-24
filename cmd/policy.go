package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Rafaelhdsg/inframind-cli/internal/engine"
	"github.com/Rafaelhdsg/inframind-cli/internal/pipeline"
	"github.com/Rafaelhdsg/inframind-cli/internal/pricing"
	"github.com/Rafaelhdsg/inframind-cli/internal/providers/azure"
	"github.com/Rafaelhdsg/inframind-cli/internal/telemetry"
	"github.com/Rafaelhdsg/inframind-cli/pkg/progress"
	"github.com/Rafaelhdsg/inframind-cli/pkg/report"
	"github.com/spf13/cobra"
)

var policyCmd = &cobra.Command{
	Use:   "policy",
	Short: "Simulate and manage governance policies",
	Long:  `Run what-if analysis on governance policies. See blast radius, decision diffs, and savings impact before changing anything.`,
}

var policySimulateCmd = &cobra.Command{
	Use:   "simulate",
	Short: "Simulate the blast radius of a policy change",
	Long: `Runs a what-if analysis showing how a hypothetical policy change would
affect resources, recommendations, and cost savings — without applying anything.

Examples:
  inframind policy simulate --resource-group prod-sap --set criticality=high
  inframind policy simulate --subscription my-sub --set mode=protect --set external=true
  inframind policy simulate --resource-group dev-test --set template=development

Prefer --resource-group when possible: subscription-wide simulation scans the
entire subscription and can take much longer on large environments.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		rg, _ := cmd.Flags().GetString("resource-group")
		sub, _ := cmd.Flags().GetString("subscription")
		sets, _ := cmd.Flags().GetStringSlice("set")

		if rg == "" && sub == "" {
			return fmt.Errorf("must specify --resource-group or --subscription")
		}
		if rg != "" && sub != "" {
			return fmt.Errorf("specify only one of --resource-group or --subscription")
		}
		if len(sets) == 0 {
			return fmt.Errorf("must specify at least one --set key=value")
		}

		tags, err := parseSets(sets)
		if err != nil {
			return err
		}

		input := engine.PolicySimInput{SetTags: tags}
		if rg != "" {
			input.Scope = engine.ScopeResourceGroup
			input.ScopeName = rg
		} else {
			input.Scope = engine.ScopeSubscription
			input.ScopeName = sub
		}

		outputFormat, _ := cmd.Flags().GetString("output")
		months, _ := cmd.Flags().GetInt("forecast-months")

		azSub, err := resolveSubscriptionID("")
		if err != nil {
			return err
		}
		azProvider := azure.New(azSub)
		pp := pricing.NewAzureRetailPricing()
		azProvider.SetPricing(pp)

		p := pipeline.New(azProvider)
		p.SetPricing(pp)

		shortSub := azSub
		if len(shortSub) > 12 {
			shortSub = azSub[:8] + "..."
		}

		tracker := progress.New()
		p.OnProgress = func(stage, detail string, current, total int) {
			tracker.Stage(stage)
			if detail != "" {
				tracker.Detail(detail)
			}
		}
		tracker.Start(fmt.Sprintf("Scanning subscription %s", shortSub))

		ctx := context.Background()

		result, err := p.SimulatePolicy(ctx, DefaultResourceTypes, input, months)
		if err != nil {
			tracker.Finish("Simulation failed")
			return fmt.Errorf("policy simulation failed: %w", err)
		}
		tracker.Finish(fmt.Sprintf("Simulated %d resources", result.AffectedCount))

		if err := report.RenderPolicySim(os.Stdout, result, report.Format(outputFormat)); err != nil {
			return err
		}

		exported := false
		exportFormat := ""
		exportPath, _ := cmd.Flags().GetString("export")
		if exportPath != "" {
			ext := strings.ToLower(filepath.Ext(exportPath))
			if ext != ".md" && ext != ".html" {
				return fmt.Errorf("--export supports .md or .html (got %q)", ext)
			}
			f, err := os.Create(exportPath)
			if err != nil {
				return fmt.Errorf("cannot create export file: %w", err)
			}
			defer f.Close()
			if err := report.ExportPolicySim(f, result, exportPath); err != nil {
				return fmt.Errorf("export failed: %w", err)
			}
			fmt.Fprintf(os.Stderr, "\nReport exported to %s\n", exportPath)
			exported = true
			exportFormat = ext[1:]
			telemetry.Track("report_exported", map[string]interface{}{
				"format": exportFormat,
			})
		}

		// Savings are reported as bucketed magnitude + direction to
		// honour the privacy promise in the first-run notice (no raw
		// dollar figures ever leave the user's machine).
		savingsDirection := "zero"
		switch {
		case result.SavingsDelta > 0:
			savingsDirection = "positive"
		case result.SavingsDelta < 0:
			savingsDirection = "negative"
		}
		magnitude := result.SavingsDelta
		if magnitude < 0 {
			magnitude = -magnitude
		}
		telemetry.Track("policy_simulate_completed", map[string]interface{}{
			"impact_level":            string(result.Impact),
			"affected_count":          result.AffectedCount,
			"decision_changes":        len(result.DecisionDiffs),
			"savings_delta_direction": savingsDirection,
			"savings_delta_bucket":    telemetry.SavingsBucket(magnitude),
			"external_affected":       result.ExternalAffected,
			"exported":                exported,
			"export_format":           exportFormat,
		})

		return nil
	},
}

func parseSets(sets []string) (map[string]string, error) {
	tags := make(map[string]string, len(sets))
	for _, s := range sets {
		parts := strings.SplitN(s, "=", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("invalid --set format %q, expected key=value", s)
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		switch key {
		case "mode", "criticality", "external", "template":
			tags[key] = strings.TrimSpace(parts[1])
		default:
			return nil, fmt.Errorf("unknown policy key %q (valid: mode, criticality, external, template)", key)
		}
	}
	return tags, nil
}

var policyLintCmd = &cobra.Command{
	Use:   "lint",
	Short: "Validate inframind-* tag values and detect drift (no metrics)",
	Long: `Runs discovery + policy resolution only — no metrics, no rules, no simulation.
Fast enough to gate in CI.

Flags any resource whose inframind-mode / criticality / template / external
tag carries an unsupported value, plus drift between resource-level tags and
their RG / subscription parent.

Examples:
  inframind policy lint
  inframind policy lint --resource-group prod-sap
  inframind policy lint -o json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		rg, _ := cmd.Flags().GetString("resource-group")
		outputFormat, _ := cmd.Flags().GetString("output")

		sub, err := resolveSubscriptionID("")
		if err != nil {
			return err
		}
		azProvider := azure.New(sub)
		p := pipeline.New(azProvider)
		p.ResourceGroup = rg

		tracker := progress.New()
		if outputFormat == "json" {
			tracker.SetSilent(true)
		}
		p.OnProgress = func(stage, detail string, current, total int) {
			tracker.Stage(stage)
			if detail != "" {
				tracker.Detail(detail)
			}
		}
		tracker.Start("Linting policies")

		result, err := p.Lint(context.Background(), DefaultResourceTypes)
		if err != nil {
			tracker.Finish("Lint failed")
			return fmt.Errorf("policy lint failed: %w", err)
		}
		tracker.Finish(fmt.Sprintf("Scanned %d resources", result.TotalResources))

		if outputFormat == "json" {
			return renderLintJSON(result)
		}
		renderLintTable(result)
		if len(result.InvalidTags) > 0 {
			return fmt.Errorf("%d invalid tag value(s) detected", len(result.InvalidTags))
		}
		return nil
	},
}

func renderLintTable(r *pipeline.LintResult) {
	fmt.Println()
	fmt.Printf("  %s\n", report.Header("Policy Lint"))
	fmt.Printf("  %s\n", report.Dim("==========="))
	fmt.Println()
	fmt.Printf("  %s %d resources analyzed\n", report.Dim("Total:"), r.TotalResources)
	fmt.Printf("  %s %d drifts\n", report.Dim("Drifts:"), len(r.Drifts))
	fmt.Printf("  %s %d invalid tag values\n\n", report.Dim("Invalid:"), len(r.InvalidTags))

	if len(r.InvalidTags) == 0 && len(r.Drifts) == 0 {
		fmt.Printf("  %s No tag issues or drift detected.\n\n", report.BoldGreen("✓"))
		return
	}

	if len(r.InvalidTags) > 0 {
		fmt.Printf("  %s\n", report.Section("Invalid tag values"))
		for _, it := range r.InvalidTags {
			fmt.Printf("    %s %s = %q\n",
				report.BoldRed("✗"),
				report.Bold(it.Key),
				it.Value)
			fmt.Printf("      %s\n", report.Dim(shortResourceName(it.ResourceID)))
			fmt.Printf("      %s %s\n", report.Yellow("hint:"), it.Reason)
		}
		fmt.Println()
	}

	if len(r.Drifts) > 0 {
		fmt.Printf("  %s\n", report.Section("Policy drifts"))
		for _, d := range r.Drifts {
			fmt.Printf("    %s %s: resource=%s vs %s=%s (%s)\n",
				report.Yellow("!"),
				d.Field,
				report.Bold(d.ResourceValue),
				d.ParentSource,
				d.ParentValue,
				report.Dim(d.ParentName))
		}
		fmt.Println()
	}
}

func renderLintJSON(r *pipeline.LintResult) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	out := map[string]interface{}{
		"total_resources": r.TotalResources,
		"drifts":          r.Drifts,
		"invalid_tags":    r.InvalidTags,
	}
	return enc.Encode(out)
}

func init() {
	rootCmd.AddCommand(policyCmd)
	policyCmd.AddCommand(policySimulateCmd)
	policyCmd.AddCommand(policyLintCmd)

	policySimulateCmd.Flags().String("resource-group", "", "Target resource group for the simulated change")
	policySimulateCmd.Flags().String("subscription", "", "Target subscription for the simulated change")
	policySimulateCmd.Flags().StringSlice("set", nil, "Policy values to simulate (e.g. criticality=high, mode=protect)")
	policySimulateCmd.Flags().Int("forecast-months", 12, "Number of months for savings forecast")
	policySimulateCmd.Flags().String("export", "", "Export report to file (.md or .html)")

	policyLintCmd.Flags().String("resource-group", "", "Limit lint to one resource group")
}
