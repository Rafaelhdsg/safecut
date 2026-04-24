package cmd

import (
	"fmt"

	"github.com/Rafaelhdsg/inframind-cli/internal/pricing_tiers"
	"github.com/Rafaelhdsg/inframind-cli/internal/telemetry"
	"github.com/Rafaelhdsg/inframind-cli/pkg/report"
	"github.com/spf13/cobra"
)

var partnerCmd = &cobra.Command{
	Use:   "partner",
	Short: "Preview the MSP / white-label track (Partner program)",
	Long: `InfraMind Partner is the MSP / consultancy track: 20% recurring revshare,
white-label reports, per-client subscription management, and co-marketing.

This command is a preview in v1.0 — the white-label PDF pipeline and
per-client dashboards ship in v1.1. Use --brand and --client to simulate the
header that future scans will render on your reports.

Examples:
  inframind partner --brand "Acme Consulting"
  inframind partner --brand "Acme Consulting" --client "Contoso Ltd"
  inframind partner --apply`,
	RunE: func(cmd *cobra.Command, args []string) error {
		brand, _ := cmd.Flags().GetString("brand")
		client, _ := cmd.Flags().GetString("client")
		apply, _ := cmd.Flags().GetBool("apply")

		w := cmd.OutOrStdout()
		fmt.Fprintln(w)
		fmt.Fprintf(w, "  %s\n", report.Header("InfraMind Partner — Preview"))
		fmt.Fprintf(w, "  %s\n", report.Dim("==========================="))
		fmt.Fprintln(w)

		if brand == "" && client == "" && !apply {
			telemetry.CTAShown("partner", "partner_cmd_pitch", 0)
			renderPartnerPitch(w)
			return nil
		}

		if brand != "" || client != "" {
			renderPartnerBrandPreview(w, brand, client)
		}

		if apply {
			telemetry.CTAClicked("partner", "partner_apply", "partner_cmd")
			fmt.Fprintf(w, "  %s %s\n", report.BoldGreen("→"), report.Bold("Join the partner / MSP waitlist"))
			fmt.Fprintf(w, "  %s\n", report.Cyan(pricing_tiers.PartnerURL))
			fmt.Fprintln(w)
		} else {
			telemetry.CTAShown("partner", "partner_cmd", 0)
			fmt.Fprintf(w, "  %s %s\n",
				report.Yellow("→"),
				report.Dim("Ready to join? ")+report.Cyan("inframind partner --apply"))
			fmt.Fprintln(w)
		}
		return nil
	},
}

func renderPartnerPitch(w interface {
	Write(p []byte) (n int, err error)
}) {
	fmt.Fprintf(w, "  %s %s\n", report.BoldCyan("⚡"),
		report.Bold("Turn InfraMind into a recurring revenue stream."))
	fmt.Fprintln(w)
	highlights := []string{
		"20% recurring revenue share on every client you bring",
		"White-label PDF / Markdown reports with your logo and colors",
		"Per-client subscription management under a single partner account",
		"Co-marketing (case studies, shared landing pages)",
	}
	for _, h := range highlights {
		fmt.Fprintf(w, "  %s %s\n", report.Dim("•"), h)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s %s\n", report.Dim("Who it's for:"),
		"MSPs, FinOps consultancies, and freelancers serving 2+ Azure clients.")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s %s\n", report.Dim("Preview your branding:"),
		report.Cyan(`inframind partner --brand "Acme Consulting" --client "Contoso Ltd"`))
	fmt.Fprintf(w, "  %s %s\n", report.Dim("Apply now:           "),
		report.Cyan("inframind partner --apply"))
	fmt.Fprintln(w)
}

func renderPartnerBrandPreview(w interface {
	Write(p []byte) (n int, err error)
}, brand, client string) {
	fmt.Fprintf(w, "  %s %s\n", report.Dim("Preview header (will render on PDF/HTML reports in v1.1):"), "")
	fmt.Fprintln(w)
	if brand != "" {
		fmt.Fprintf(w, "    %s %s\n", report.Dim("Prepared by :"), report.Bold(brand))
	}
	if client != "" {
		fmt.Fprintf(w, "    %s %s\n", report.Dim("Prepared for:"), report.Bold(client))
	}
	fmt.Fprintf(w, "    %s %s\n", report.Dim("Report      :"), report.Bold("Azure cost scan — v1.0"))
	fmt.Fprintf(w, "    %s %s\n", report.Dim("Footer      :"),
		report.Dim("Powered by InfraMind (hidden on Partner plan)"))
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s %s\n", report.Yellow("ℹ"),
		report.Dim("In v1.0 these flags only preview the header. PDF export ships in v1.1."))
	fmt.Fprintln(w)
}

func init() {
	rootCmd.AddCommand(partnerCmd)
	partnerCmd.Flags().String("brand", "", "Your consultancy / MSP brand name")
	partnerCmd.Flags().String("client", "", "Client / customer name for this preview")
	partnerCmd.Flags().Bool("apply", false, "Show the partner program application URL")
}
