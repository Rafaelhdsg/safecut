package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "inframind",
	Short: "Decision engine for cloud infrastructure",
	Long:  `InfraMind CLI — Analyze, simulate, and optimize cloud costs with safety and explainability.`,
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
}
