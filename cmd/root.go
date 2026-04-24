package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/Rafaelhdsg/inframind-cli/internal/defaults"
	"github.com/Rafaelhdsg/inframind-cli/internal/telemetry"
	"github.com/Rafaelhdsg/inframind-cli/internal/version"
	"github.com/Rafaelhdsg/inframind-cli/pkg/report"
	"github.com/spf13/cobra"
)

var cmdStartTime time.Time

// DefaultResourceTypes is re-exported from [defaults.DefaultResourceTypes] for commands.
var DefaultResourceTypes = defaults.DefaultResourceTypes

var rootCmd = &cobra.Command{
	Use:   "inframind",
	Short: "Decision engine for cloud infrastructure — read-only, safe, explainable",
	Long: `InfraMind CLI — Find idle resources, simulate changes safely, and stop
paying for what you're not using. 100% read-only. Nothing is modified. Ever.

  quick-scan         Instant scan — find waste fast (use --resource-group to narrow scope)
  policy simulate    What-if analysis before changing anything
  apply [Cloud]      Automate optimizations via InfraMind Cloud

InfraMind runs a 6-layer pipeline (Discovery → Pricing → Graph → Engine →
Simulation → Forecast) and explains every recommendation with signals,
blast radius, and confidence scoring.`,
	Version: version.Full(),
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		cmdStartTime = time.Now()

		if noColor, _ := cmd.Flags().GetBool("no-color"); noColor {
			report.SetColor(false)
		}

		if telemetry.IsFirstRun() {
			printFirstRunNotice()
		}

		noTelemetry, _ := cmd.Flags().GetBool("no-telemetry")
		if noTelemetry {
			return
		}
		telemetry.Init()
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		duration := time.Since(cmdStartTime)
		telemetry.Track("cli_command", map[string]interface{}{
			"command":     cmd.CommandPath(),
			"duration_ms": duration.Milliseconds(),
		})
		telemetry.Close()
	},
}

func printFirstRunNotice() {
	cfg, created, err := telemetry.EnsureConfig()
	if err != nil {
		return
	}
	if !created {
		return
	}

	w := os.Stderr
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s\n", report.Header("Welcome to InfraMind CLI"))
	fmt.Fprintf(w, "  %s\n", report.Dim("========================"))
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  InfraMind analyzes your infrastructure %s.\n", report.BoldGreen("read-only"))
	fmt.Fprintf(w, "  %s. Ever.\n", report.Bold("Nothing is modified"))
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s\n", report.Dim("Your workflow:"))
	fmt.Fprintf(w, "    %s  %s  %s\n",
		report.Cyan("quick-scan"),
		report.Dim("→"),
		report.Dim("find waste fast (minutes on large subs; --resource-group helps)"))
	fmt.Fprintf(w, "    %s  %s  %s\n",
		report.Cyan("policy simulate"),
		report.Dim("→"),
		report.Dim("what-if before changing anything"))
	fmt.Fprintf(w, "    %s  %s  %s\n",
		report.Cyan("apply"),
		report.Dim("→"),
		report.Dim("automate via InfraMind Cloud"))
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s\n", report.Bold("Telemetry"))
	fmt.Fprintf(w, "  %s\n", report.Dim("---------"))
	fmt.Fprintf(w, "  %s\n", report.Dim("We send a minimal, anonymous event per scan to improve InfraMind:"))
	fmt.Fprintf(w, "    %s anonymous installation_id (random UUID, not tied to you or Azure)\n", report.Dim("·"))
	fmt.Fprintf(w, "    %s command name (e.g. \"quick-scan\", \"policy simulate\")\n", report.Dim("·"))
	fmt.Fprintf(w, "    %s CLI version, OS, architecture\n", report.Dim("·"))
	fmt.Fprintf(w, "    %s scan duration (ms), total resources scanned, idle resources detected\n", report.Dim("·"))
	fmt.Fprintf(w, "    %s rule hit counts (e.g. \"orphan_disk: 3\")\n", report.Dim("·"))
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s\n", report.Dim("We never send:"))
	fmt.Fprintf(w, "    %s subscription IDs, tenant IDs, or any Azure account identifier\n", report.Dim("·"))
	fmt.Fprintf(w, "    %s resource names, IDs, tags, or resource-group names\n", report.Dim("·"))
	fmt.Fprintf(w, "    %s raw savings amounts, costs, or any dollar figures\n", report.Dim("·"))
	fmt.Fprintf(w, "    %s IP addresses, secrets, credentials, metric values, or logs\n", report.Dim("·"))
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  Installation ID: %s\n", report.Dim(cfg.InstallationID))
	fmt.Fprintf(w, "  %s  %s\n",
		report.Yellow("Opt out anytime:"),
		report.Bold("inframind config --telemetry disable"))
	fmt.Fprintf(w, "  %s  %s\n",
		report.Dim("Honored env vars:"),
		report.Dim("INFRAMIND_NO_TELEMETRY=1 or DO_NOT_TRACK=1"))
	fmt.Fprintln(w)
}

// resolveSubscriptionID returns the Azure subscription ID from flag, env vars,
// or error. Supports AZURE_SUBSCRIPTION_ID and ARM_SUBSCRIPTION_ID (Terraform).
// If both env vars are set with different values, it errors to prevent silent
// misuse. --subscription flag always wins.
func resolveSubscriptionID(flagVal string) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}

	azureVal := os.Getenv("AZURE_SUBSCRIPTION_ID")
	armVal := os.Getenv("ARM_SUBSCRIPTION_ID")

	if azureVal != "" && armVal != "" && azureVal != armVal {
		return "", fmt.Errorf(
			"conflicting subscription IDs:\n"+
				"  AZURE_SUBSCRIPTION_ID = %s\n"+
				"  ARM_SUBSCRIPTION_ID   = %s\n"+
				"Resolve by unsetting one, or use --subscription to override",
			azureVal, armVal,
		)
	}

	if azureVal != "" {
		return azureVal, nil
	}
	if armVal != "" {
		return armVal, nil
	}
	return "", fmt.Errorf("subscription ID required: use --subscription, set AZURE_SUBSCRIPTION_ID, or ARM_SUBSCRIPTION_ID")
}

func Execute() error {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		return err
	}
	return nil
}

func init() {
	rootCmd.PersistentFlags().StringP("output", "o", "table", "Output format: table, json, ascii")
	rootCmd.PersistentFlags().Bool("no-telemetry", false, "Disable telemetry for this invocation")
	rootCmd.PersistentFlags().Bool("no-color", false, "Disable ANSI colors (equivalent to NO_COLOR=1)")
}
