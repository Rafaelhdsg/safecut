package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Rafaelhdsg/inframind-cli/internal/engine"
	"github.com/Rafaelhdsg/inframind-cli/internal/pipeline"
	"github.com/Rafaelhdsg/inframind-cli/internal/providers/azure"
	"github.com/Rafaelhdsg/inframind-cli/pkg/report"
	"github.com/spf13/cobra"
)

var policyCmd = &cobra.Command{
	Use:   "policy",
	Short: "Manage and simulate governance policies",
	Long:  `Inspect, simulate, and validate InfraMind governance policies before applying changes.`,
}

var policySimulateCmd = &cobra.Command{
	Use:   "simulate",
	Short: "Simulate the blast radius of a policy change",
	Long: `Runs a what-if analysis showing how a hypothetical policy change would
affect resources, recommendations, and cost savings — without applying anything.

Examples:
  inframind policy simulate --resource-group prod-sap --set criticality=high
  inframind policy simulate --subscription my-sub --set mode=protect --set external=true
  inframind policy simulate --resource-group dev-test --set template=development`,
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

		azProvider := azure.New("YOUR_AZURE_SUBSCRIPTION_ID")
		p := pipeline.New(azProvider)

		ctx := context.Background()
		resourceTypes := []string{
			"Microsoft.Compute/virtualMachines",
			"Microsoft.Compute/disks",
			"Microsoft.Network/publicIPAddresses",
		}

		result, err := p.SimulatePolicy(ctx, resourceTypes, input, months)
		if err != nil {
			return fmt.Errorf("policy simulation failed: %w", err)
		}

		if err := report.RenderPolicySim(os.Stdout, result, report.Format(outputFormat)); err != nil {
			return err
		}

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
		}

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

func init() {
	rootCmd.AddCommand(policyCmd)
	policyCmd.AddCommand(policySimulateCmd)

	policySimulateCmd.Flags().String("resource-group", "", "Target resource group for the simulated change")
	policySimulateCmd.Flags().String("subscription", "", "Target subscription for the simulated change")
	policySimulateCmd.Flags().StringSlice("set", nil, "Policy values to simulate (e.g. criticality=high, mode=protect)")
	policySimulateCmd.Flags().Int("forecast-months", 12, "Number of months for savings forecast")
	policySimulateCmd.Flags().String("export", "", "Export report to file (.md or .html)")
}
