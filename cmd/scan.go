package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan cloud infrastructure for optimization opportunities",
	Long:  `Analyzes your cloud resources to detect orphaned disks, idle VMs, unattached IPs, and other waste.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Scanning infrastructure...")
		// TODO: integrate with internal/engine and internal/providers
		return nil
	},
}

func init() {
	rootCmd.AddCommand(scanCmd)
	scanCmd.Flags().String("subscription", "", "Azure subscription ID to scan")
}
