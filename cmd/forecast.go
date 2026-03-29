package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var forecastCmd = &cobra.Command{
	Use:   "forecast",
	Short: "Forecast cost savings and ROI from optimizations",
	Long:  `Calculates projected savings and return on investment based on detected optimization opportunities.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Generating forecast...")
		// TODO: integrate with internal/forecast
		return nil
	},
}

func init() {
	rootCmd.AddCommand(forecastCmd)
	forecastCmd.Flags().Int("months", 12, "Number of months to project savings")
}
