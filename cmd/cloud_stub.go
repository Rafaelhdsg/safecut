package cmd

import (
	"fmt"
	"strings"

	"github.com/Rafaelhdsg/safecut/pkg/report"
	"github.com/spf13/cobra"
)

// runCloudStub prints a friendly "coming soon" message for non-Azure clouds.
// It does not error so CI/demo scripts can probe the flag safely.
func runCloudStub(cmd *cobra.Command, cloud string) error {
	c := strings.ToLower(strings.TrimSpace(cloud))
	w := cmd.OutOrStdout()

	label := map[string]string{
		"aws": "Amazon Web Services",
		"gcp": "Google Cloud Platform",
	}[c]
	if label == "" {
		return fmt.Errorf("unsupported --cloud %q (valid: azure, aws, gcp)", cloud)
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s %s\n", report.BoldCyan("⚡"), report.Bold(label+" support — coming soon"))
	fmt.Fprintln(w, "  "+report.Dim("SafeCut v1.0 ships Azure-first. The engine, rules, and"))
	fmt.Fprintln(w, "  "+report.Dim("policy model are cloud-agnostic — "+c+" adapters arrive next."))
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s  %s\n",
		report.Yellow("→"),
		report.Cyan(report.WaitlistURL))
	fmt.Fprintln(w, "  "+report.Dim("Get notified the moment "+c+" quick-scan goes live."))
	fmt.Fprintln(w)
	return nil
}
