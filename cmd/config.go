package cmd

import (
	"fmt"

	"github.com/Rafaelhdsg/inframind-cli/internal/telemetry"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage CLI settings and telemetry preferences",
	Long: `View and modify InfraMind CLI settings.

Examples:
  inframind config --telemetry status    Show telemetry state and what is collected
  inframind config --telemetry disable   Opt out of anonymous telemetry
  inframind config --telemetry enable    Opt back in`,
	RunE: func(cmd *cobra.Command, args []string) error {
		tel, _ := cmd.Flags().GetString("telemetry")
		if tel == "" {
			return cmd.Help()
		}

		switch tel {
		case "status":
			return showTelemetryStatus()
		case "disable":
			return disableTelemetry()
		case "enable":
			return enableTelemetry()
		default:
			return fmt.Errorf("unknown --telemetry value %q (valid: status, enable, disable)", tel)
		}
	},
}

func showTelemetryStatus() error {
	cfg, _ := telemetry.LoadConfig()

	fmt.Println("Telemetry Status")
	fmt.Println("================")
	fmt.Println()

	if cfg == nil {
		fmt.Println("  Status:          not initialized (no config file)")
		fmt.Println("  Will be:         enabled on first run")
		fmt.Println()
		fmt.Printf("  Config path:     %s\n", telemetry.ConfigPath())
		fmt.Println()
		fmt.Println("  Disable before first run:")
		fmt.Println("    export INFRAMIND_NO_TELEMETRY=1")
		return nil
	}

	status := "enabled"
	if !cfg.TelemetryEnabled {
		status = "disabled"
	}
	fmt.Printf("  Status:          %s\n", status)
	fmt.Printf("  Installation ID: %s\n", cfg.InstallationID)
	fmt.Printf("  First seen:      %s\n", cfg.FirstSeen.Format("2006-01-02"))
	fmt.Printf("  Config path:     %s\n", telemetry.ConfigPath())
	fmt.Println()

	fmt.Println("  What we collect:")
	fmt.Println("    - Command name and duration")
	fmt.Println("    - Resource counts and savings amounts")
	fmt.Println("    - Impact levels and recommendation counts")
	fmt.Println("    - Export format (md/html)")
	fmt.Println("    - OS, architecture, CLI version")
	fmt.Println()
	fmt.Println("  What we NEVER collect:")
	fmt.Println("    - Resource IDs or names")
	fmt.Println("    - Subscription IDs")
	fmt.Println("    - Cloud credentials or tag values")
	fmt.Println("    - IP addresses")
	fmt.Println()
	fmt.Println("  Override with environment variables:")
	fmt.Println("    INFRAMIND_NO_TELEMETRY=1  or  DO_NOT_TRACK=1")

	return nil
}

func disableTelemetry() error {
	if err := telemetry.Disable(); err != nil {
		return fmt.Errorf("failed to disable telemetry: %w", err)
	}
	fmt.Println("Telemetry disabled. No data will be collected.")
	fmt.Printf("Config saved to %s\n", telemetry.ConfigPath())
	return nil
}

func enableTelemetry() error {
	if err := telemetry.Enable(); err != nil {
		return fmt.Errorf("failed to enable telemetry: %w", err)
	}
	fmt.Println("Telemetry enabled. Thank you for helping improve InfraMind.")
	fmt.Printf("Config saved to %s\n", telemetry.ConfigPath())
	return nil
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.Flags().String("telemetry", "", "Manage telemetry: status, enable, disable")
}
