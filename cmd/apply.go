package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/Rafaelhdsg/safecut/internal/pipeline"
	"github.com/Rafaelhdsg/safecut/internal/pricing"
	"github.com/Rafaelhdsg/safecut/internal/providers/azure"
	"github.com/Rafaelhdsg/safecut/pkg/progress"
	"github.com/Rafaelhdsg/safecut/pkg/report"
	"github.com/spf13/cobra"
)

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply optimizations [Cloud]",
	Long: `Runs the same full read-only scan as quick-scan, then lists actions
that are safe to automate. Actual execution requires SafeCut Cloud.

Use --resource-group to scope to one RG for a faster run.

Join the waitlist at https://safecut.dev/#waitlist`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cloud, _ := cmd.Flags().GetString("cloud")
		if cloud != "" && cloud != "azure" {
			return runCloudStub(cmd, cloud)
		}

		flagSub, _ := cmd.Flags().GetString("subscription")
		rgScope, _ := cmd.Flags().GetString("resource-group")
		sub, err := resolveSubscriptionID(flagSub)
		if err != nil {
			return err
		}

		shortSub := sub
		if len(shortSub) > 12 {
			shortSub = sub[:8] + "..."
		}

		azProvider := azure.New(sub)
		pp := pricing.NewAzureRetailPricing()
		azProvider.SetPricing(pp)

		p := pipeline.New(azProvider)
		p.SetPricing(pp)
		p.ResourceGroup = rgScope

		fmt.Fprintf(os.Stderr, "  %s\n", report.Dim("Running full read-only scan (same engine as quick-scan). Nothing is modified."))
		if rgScope != "" {
			fmt.Fprintf(os.Stderr, "  %s\n\n", report.Dim("Resource group: "+rgScope))
		} else {
			fmt.Fprintln(os.Stderr)
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
		result, err := p.Run(ctx, DefaultResourceTypes, 12)
		if err != nil {
			tracker.Finish("Scan failed")
			return fmt.Errorf("scan failed: %w", err)
		}

		totalResources := len(result.Snapshot.Resources) + len(result.Protected)
		tracker.Finish(fmt.Sprintf("Scanned %d resources", totalResources))

		var autoActions []autoAction
		totalSaving := 0.0
		for _, rec := range result.Simulation.Applied {
			if rec.AutoExecute {
				autoActions = append(autoActions, autoAction{
					action: strings.ToUpper(rec.Action),
					name:   shortResourceName(rec.ResourceID),
					saving: rec.MonthlySave,
				})
				totalSaving += rec.MonthlySave
			}
		}

		fmt.Println()
		if len(autoActions) == 0 {
			fmt.Printf("  %s  No actions safe to auto-execute found.\n", report.Green("✓"))
			fmt.Printf("  Run %s for detailed analysis.\n\n", report.Cyan("safecut quick-scan"))
			return nil
		}

		fmt.Printf("  %s %s\n\n",
			report.BoldCyan("⚡"),
			report.Section(fmt.Sprintf("%d actions ready to automate:", len(autoActions))))

		for i, a := range autoActions {
			fmt.Printf("     %s  %-10s %-36s %s\n",
				report.Dim(fmt.Sprintf("%2d.", i+1)),
				actionColor(a.action),
				a.name,
				report.BoldGreen(fmt.Sprintf("$%.2f/mo", a.saving)))
		}

		fmt.Println()
		fmt.Printf("     %s  %s\n",
			report.Dim("TOTAL"),
			report.BoldGreen(fmt.Sprintf("%s/mo (%s/yr)", report.Money(totalSaving), report.Money0(totalSaving*12))))
		fmt.Println()

		fmt.Printf("  %s\n", report.Dim(strings.Repeat("─", 62)))
		fmt.Println()
		fmt.Printf("  %s  %s\n",
			report.BoldCyan("⚡"),
			report.Bold("Auto-execution requires SafeCut Cloud"))
		fmt.Printf("     Automate all %d actions with one click, rollback protection included.\n", len(autoActions))
		fmt.Printf("     %s  %s\n\n",
			report.Dim("→"),
			report.Cyan(report.WaitlistURL))

		return nil
	},
}

type autoAction struct {
	action string
	name   string
	saving float64
}

func init() {
	rootCmd.AddCommand(applyCmd)
	applyCmd.Flags().String("subscription", "", "Azure subscription ID (or set AZURE_SUBSCRIPTION_ID)")
	applyCmd.Flags().String("resource-group", "", "Limit scan to one resource group (faster)")
	applyCmd.Flags().String("cloud", "azure", "Target cloud (azure | aws | gcp). aws/gcp are waitlist-only in v1.0.")
}
