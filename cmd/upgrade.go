package cmd

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/Rafaelhdsg/safecut/internal/pricing_tiers"
	"github.com/Rafaelhdsg/safecut/internal/telemetry"
	"github.com/Rafaelhdsg/safecut/pkg/report"
	"github.com/spf13/cobra"
)

var upgradeCmd = &cobra.Command{
	Use:     "upgrade",
	Aliases: []string{"cloud"},
	Short:   "Compare SafeCut plans and join the founding waitlist",
	Long: `SafeCut CLI is free forever for read-only scans, policy simulate,
policy lint, history, rightsizing, and RI suggestions — all shipping today.
SafeCut Cloud (automation, scheduled scans, Slack alerts, white-label
reports) ships with v1.1 and is currently in founding-customer early access.

Use the flags to jump straight to the right conversion path:

  safecut upgrade --start-trial solo    # Freelancer / single-sub CTO
  safecut upgrade --start-trial team    # Startup / scale-up (up to 10 seats)
  safecut upgrade --book-demo           # Enterprise (from $799/mo or 8% of savings)
  safecut upgrade --partner             # MSP / consultant track (20% revshare)

Without flags this command just prints the pricing table.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		openFlag, _ := cmd.Flags().GetBool("open")
		startTrial, _ := cmd.Flags().GetString("start-trial")
		bookDemo, _ := cmd.Flags().GetBool("book-demo")
		partner, _ := cmd.Flags().GetBool("partner")

		w := cmd.OutOrStdout()

		if target, url, ok := resolveUpgradeAction(startTrial, bookDemo, partner); ok {
			tier, action := telemetryTagsFor(startTrial, bookDemo, partner)
			telemetry.CTAClicked(tier, action, "upgrade_cmd")
			fmt.Fprintln(w)
			fmt.Fprintf(w, "  %s %s\n", report.BoldGreen("→"), report.Bold(target))
			fmt.Fprintf(w, "  %s\n", report.Cyan(url))
			fmt.Fprintln(w)
			if openFlag {
				if err := openBrowser(url); err != nil {
					fmt.Fprintf(w, "  %s could not open browser: %s\n", report.Yellow("!"), err.Error())
					fmt.Fprintln(w, "  Copy the link above manually.")
				}
			}
			return nil
		}

		telemetry.CTAShown("pricing", "upgrade_cmd", 0)
		renderPricingTable(cmd, w)
		if openFlag {
			if err := openBrowser(pricing_tiers.PricingURL); err != nil {
				fmt.Fprintf(w, "  %s could not open browser: %s\n", report.Yellow("!"), err.Error())
			}
		}
		return nil
	},
}

// telemetryTagsFor derives (tier, action) labels for conversion telemetry
// from the upgrade flags. Kept alongside resolveUpgradeAction so both stay in
// sync when new CTAs land.
func telemetryTagsFor(startTrial string, bookDemo, partner bool) (string, string) {
	switch {
	case partner:
		return "partner", "partner_apply"
	case bookDemo:
		return "enterprise", "book_demo"
	case startTrial != "":
		switch strings.ToLower(startTrial) {
		case "solo":
			return "solo", "start_trial"
		case "team":
			return "team", "start_trial"
		default:
			return "pricing", "open_pricing"
		}
	}
	return "pricing", "open_pricing"
}

// resolveUpgradeAction maps --start-trial/--book-demo/--partner flags to a
// specific label and URL. Only one action is honored per run; precedence is
// partner > book-demo > start-trial so operators can chain flags safely in
// scripted environments (ok = false means no action requested).
//
// Labels intentionally say "join founding waitlist" rather than "start
// trial" while Cloud (v1.1) is in early access. The flags keep their
// --start-trial names so CI snippets users may already have don't break
// when checkout goes live.
func resolveUpgradeAction(startTrial string, bookDemo, partner bool) (string, string, bool) {
	switch {
	case partner:
		return "Join the partner / MSP waitlist", pricing_tiers.PartnerURL, true
	case bookDemo:
		return "Book an enterprise discovery call", pricing_tiers.EnterpriseURL, true
	case startTrial != "":
		switch strings.ToLower(startTrial) {
		case "solo":
			return "Join Solo founding waitlist ($29/mo locked in)", pricing_tiers.CheckoutSoloURL, true
		case "team":
			return "Join Team founding waitlist ($199/mo locked in)", pricing_tiers.CheckoutTeamURL, true
		default:
			return "Compare all plans", pricing_tiers.PricingURL, true
		}
	}
	return "", "", false
}

func renderPricingTable(cmd *cobra.Command, w interface {
	Write(p []byte) (n int, err error)
}) {
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s\n", report.Header("SafeCut Cloud — Pricing"))
	fmt.Fprintf(w, "  %s\n", report.Dim("========================="))
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s %s\n", report.BoldGreen("✓"), report.Bold("CLI is free forever — ships today"))
	fmt.Fprintln(w, "      read-only scans, policy simulate, policy lint, rightsizing, RI suggestions, local history")
	fmt.Fprintf(w, "  %s %s\n", report.Yellow("⚡"), report.Bold("Cloud tiers (Solo, Team, Enterprise, Partner) ship with v1.1"))
	fmt.Fprintln(w, "      founding customers on the waitlist lock in today's price for the lifetime of the sub")
	fmt.Fprintln(w)

	for _, tier := range pricing_tiers.All() {
		fmt.Fprintf(w, "  %s  %s\n",
			report.BoldCyan(padRight(tier.Name, 12)),
			report.Bold(tier.Price))
		fmt.Fprintf(w, "    %s %s\n", report.Dim("Target :"), tier.Target)
		fmt.Fprintf(w, "    %s %s\n", report.Dim("Scope  :"), tier.Quota)
		for _, h := range tier.Highlights {
			fmt.Fprintf(w, "    %s %s\n", report.Dim("•"), h)
		}
		fmt.Fprintf(w, "    %s %s\n", report.Yellow("→"), report.Cyan(fmt.Sprintf("%s  (%s)", tier.CTA, tier.URL)))
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "  %s %s\n", report.BoldYellow("★"),
		report.Bold("Founding customer pricing locks in while we're pre-1.0. Lock yours today."))
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s\n", report.Dim("Quick actions:"))
	fmt.Fprintf(w, "    %s safecut upgrade --start-trial solo\n", report.Dim("›"))
	fmt.Fprintf(w, "    %s safecut upgrade --start-trial team\n", report.Dim("›"))
	fmt.Fprintf(w, "    %s safecut upgrade --book-demo\n", report.Dim("›"))
	fmt.Fprintf(w, "    %s safecut upgrade --partner\n", report.Dim("›"))
	fmt.Fprintln(w)
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
	if cmd == nil {
		return fmt.Errorf("no browser launcher for %s", runtime.GOOS)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func init() {
	rootCmd.AddCommand(upgradeCmd)
	upgradeCmd.Flags().Bool("open", false, "Open the relevant URL in your default browser")
	upgradeCmd.Flags().String("start-trial", "", "Start a trial for [solo|team]")
	upgradeCmd.Flags().Bool("book-demo", false, "Jump to the enterprise demo booking page")
	upgradeCmd.Flags().Bool("partner", false, "Open the partner/MSP program application")
}
