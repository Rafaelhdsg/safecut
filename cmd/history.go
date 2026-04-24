package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/Rafaelhdsg/inframind-cli/internal/history"
	"github.com/Rafaelhdsg/inframind-cli/pkg/report"
	"github.com/spf13/cobra"
)

var historyCmd = &cobra.Command{
	Use:   "history",
	Short: "Show the last scans for a subscription (local, 7-day window)",
	Long: `Prints a compact table of local scan records. Useful to confirm InfraMind
persisted the last scan and to see trends without re-hitting Azure.

InfraMind keeps 7 days of local history. Longer windows and anomaly
detection ship with InfraMind Cloud.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		flagSub, _ := cmd.Flags().GetString("subscription")
		sub, err := resolveSubscriptionID(flagSub)
		if err != nil {
			return err
		}
		format, _ := cmd.Flags().GetString("output")

		records := loadHistoryForSub(sub)
		if format == "json" {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(records)
		}
		renderHistoryTable(sub, records)
		return nil
	},
}

func loadHistoryForSub(sub string) []history.ScanRecord {
	var out []history.ScanRecord

	prev := history.LoadPrevious(sub)
	if prev != nil {
		out = append(out, *prev)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp.After(out[j].Timestamp) })
	return out
}

func renderHistoryTable(sub string, records []history.ScanRecord) {
	fmt.Println()
	fmt.Printf("  %s %s\n", report.Header("History for"), report.Dim(sub))
	fmt.Println()
	if len(records) == 0 {
		fmt.Printf("  %s No local history yet. Run %s first.\n\n",
			report.Dim("·"),
			report.Cyan("inframind quick-scan"))
		fmt.Printf("  %s  %s\n\n",
			report.Dim("30/60/90-day trends & anomaly alerts →"),
			report.Bold("InfraMind Cloud")+" "+report.Dim(report.WaitlistURL))
		return
	}
	for _, r := range records {
		fmt.Printf("  %s  %d resources · %d recs · %s/mo\n",
			report.Dim(r.Timestamp.Format(time.RFC3339)),
			r.TotalResources,
			r.RecCount,
			report.BoldGreen(report.Money(r.MonthlySaving)))
	}
	fmt.Println()
	fmt.Printf("  %s  %s\n\n",
		report.Dim("30/60/90-day trends & anomaly alerts →"),
		report.Bold("InfraMind Cloud")+" "+report.Dim(report.WaitlistURL))
}

func init() {
	rootCmd.AddCommand(historyCmd)
	historyCmd.Flags().String("subscription", "", "Azure subscription ID (or set AZURE_SUBSCRIPTION_ID)")
}
