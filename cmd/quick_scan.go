package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Rafaelhdsg/safecut/internal/engine"
	"github.com/Rafaelhdsg/safecut/internal/history"
	"github.com/Rafaelhdsg/safecut/internal/pipeline"
	"github.com/Rafaelhdsg/safecut/internal/pricing"
	"github.com/Rafaelhdsg/safecut/internal/pricing_tiers"
	"github.com/Rafaelhdsg/safecut/internal/providers/azure"
	"github.com/Rafaelhdsg/safecut/internal/telemetry"
	"github.com/Rafaelhdsg/safecut/pkg/progress"
	"github.com/Rafaelhdsg/safecut/pkg/report"
	"github.com/spf13/cobra"
)

var quickScanCmd = &cobra.Command{
	Use:   "quick-scan",
	Short: "Instant scan — find waste fast, zero config",
	Long: `Scans 10 resource types with zero configuration. Shows an executive
dashboard with savings potential, safety scoring, and detailed target
analysis. Runtime depends on subscription size (often under a minute; large
subscriptions can take several minutes). Use --resource-group to scope a
single RG for faster runs. With -o json, use --progress to print stage
lines to stderr while JSON is written to stdout at the end.

Export summary with --export report.md.

Run this first. Think later.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cloud, _ := cmd.Flags().GetString("cloud")
		if cloud != "" && cloud != "azure" {
			return runCloudStub(cmd, cloud)
		}

		flagSub, _ := cmd.Flags().GetString("subscription")
		sub, err := resolveSubscriptionID(flagSub)
		if err != nil {
			return err
		}

		outputFormat, _ := cmd.Flags().GetString("output")
		progressJSON, _ := cmd.Flags().GetBool("progress")
		rgScope, _ := cmd.Flags().GetString("resource-group")

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

		tracker := progress.New()
		if outputFormat == "json" && !progressJSON {
			tracker.SetSilent(true)
		}
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

		if outputFormat == "json" {
			return renderQuickScanJSON(result, sub)
		}

		previous := history.LoadPrevious(sub)
		renderQuickScan(result, sub, previous)

		exportPath, _ := cmd.Flags().GetString("export")
		if exportPath != "" {
			exportQuickScanMD(result, sub, exportPath)
		}

		saveHistory(result, sub)
		trackQuickScan(result)
		return nil
	},
}

// ─── JSON output ────────────────────────────────────────────────────────────

type quickScanJSON struct {
	Subscription   string             `json:"subscription"`
	TotalResources int                `json:"total_resources"`
	AnnualSavings  float64            `json:"annual_savings"`
	MonthlySavings float64            `json:"monthly_savings"`
	SafetyScore    float64            `json:"safety_score"`
	Findings       []quickScanFinding `json:"findings"`
}

type quickScanFinding struct {
	ResourceID   string  `json:"resource_id"`
	ResourceName string  `json:"resource_name"`
	ResourceType string  `json:"resource_type"`
	Action       string  `json:"action"`
	Risk         string  `json:"risk"`
	MonthlySave  float64 `json:"monthly_save"`
	AutoExecute  bool    `json:"auto_execute"`
	Reason       string  `json:"reason"`
}

func renderQuickScanJSON(out *pipeline.Output, sub string) error {
	totalResources := len(out.Snapshot.Resources) + len(out.Protected)
	totalSavings := 0.0
	for _, r := range out.Decisions {
		totalSavings += r.MonthlySave
	}

	totalActions := len(out.Simulation.Applied) + len(out.Simulation.Skipped)
	safeCount := len(out.Simulation.Applied)
	safetyPct := 0.0
	if totalActions > 0 {
		safetyPct = float64(safeCount) / float64(totalActions) * 100
	}

	findings := make([]quickScanFinding, 0, len(out.Decisions))
	for _, r := range out.Decisions {
		findings = append(findings, quickScanFinding{
			ResourceID:   r.ResourceID,
			ResourceName: shortResourceName(r.ResourceID),
			ResourceType: r.ResourceType,
			Action:       r.Action,
			Risk:         r.Risk.String(),
			MonthlySave:  r.MonthlySave,
			AutoExecute:  r.AutoExecute,
			Reason:       r.Reason,
		})
	}

	result := quickScanJSON{
		Subscription:   sub,
		TotalResources: totalResources,
		AnnualSavings:  totalSavings * 12,
		MonthlySavings: totalSavings,
		SafetyScore:    safetyPct,
		Findings:       findings,
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

// ─── Data types ─────────────────────────────────────────────────────────────

type actionGroup struct {
	action      string
	typeLabel   string
	resourceKey string
	recs        []engine.Recommendation
	totalSaving float64
	pattern     string
}

// ─── Box-drawing helpers ────────────────────────────────────────────────────

const boxWidth = 56

func boxTop() string {
	return fmt.Sprintf("  +%s+", strings.Repeat("-", boxWidth+2))
}

func boxDiv() string {
	return boxTop()
}

func boxBot() string {
	return boxTop()
}

func boxRow(content string) string {
	vLen := visualLen(content)
	pad := boxWidth - vLen
	if pad < 0 {
		pad = 0
	}
	return fmt.Sprintf("  | %s%s |", content, strings.Repeat(" ", pad))
}

func visualLen(s string) int {
	inEsc := false
	count := 0
	for _, r := range s {
		if r == '\033' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		count++
	}
	return count
}

func sectionSep(label string) {
	const totalWidth = 62
	prefix := "─── "
	suffix := " "
	used := len(prefix) + len(label) + len(suffix)
	remaining := totalWidth - used
	if remaining < 3 {
		remaining = 3
	}
	fmt.Printf("\n  %s\n", report.Dim(prefix+label+suffix+strings.Repeat("─", remaining)))
}

// ─── Main render ────────────────────────────────────────────────────────────

func renderQuickScan(out *pipeline.Output, subscriptionID string, previous *history.ScanRecord) {
	totalResources := len(out.Snapshot.Resources) + len(out.Protected)
	monthlySavings := out.Simulation.TotalSaving
	yearlySavings := out.Forecast.TotalSaving
	applied := out.Simulation.Applied
	skipped := out.Simulation.Skipped

	idleCount := 0
	totalConfidence := 0.0
	for _, a := range out.Analyses {
		if a != nil && a.Score >= 0.85 {
			idleCount++
			totalConfidence += a.Confidence
		}
	}

	// ── PIPELINE SUMMARY ──
	renderPipelineBlock(out, totalResources, idleCount, subscriptionID)

	// Surface any systemic warnings from the pipeline *before* the
	// numbers. Showing "12 policy drifts detected" under a pretty
	// dashboard is a trust issue — these belong up top.
	renderReliabilityWarnings(out)

	if monthlySavings == 0 {
		fmt.Println()
		fmt.Println(boxTop())
		fmt.Println(boxRow(report.BoldCyan("SAFECUT") + "  |  " + report.BoldGreen("CLEAN INFRASTRUCTURE")))
		fmt.Println(boxBot())
		fmt.Printf("\n  %s\n\n", report.Green("No waste detected. Your infrastructure looks clean."))
		return
	}

	groups := buildActionGroups(applied)
	autoExecCount := 0
	for _, rec := range applied {
		if rec.AutoExecute {
			autoExecCount++
		}
	}

	safeCount := len(applied)
	heldCount := len(skipped)
	safeTotal := safeCount + heldCount
	safetyPct := 100
	if safeTotal > 0 {
		safetyPct = safeCount * 100 / safeTotal
	}

	avgConfidence := 0.0
	if idleCount > 0 {
		avgConfidence = totalConfidence / float64(idleCount)
	}
	confidenceLabel := "LOW"
	if avgConfidence >= 0.8 {
		confidenceLabel = "HIGH"
	} else if avgConfidence >= 0.5 {
		confidenceLabel = "MEDIUM"
	}

	// ── EXECUTIVE DASHBOARD ──
	sectionSep("DASHBOARD")
	rgSet := make(map[string]bool)
	for _, rec := range applied {
		rg := extractResourceGroup(rec.ResourceID)
		if rg != "" {
			rgSet[rg] = true
		}
	}
	rgCount := len(rgSet)
	singleRGName := ""
	if rgCount == 1 {
		for rg := range rgSet {
			singleRGName = rg
		}
	}
	renderDashboard(yearlySavings, safeCount, safeTotal, safetyPct, confidenceLabel, autoExecCount, rgCount, singleRGName)

	// ── PRICING SOURCE WARNING ──
	fallbackCount := 0
	for _, r := range out.Snapshot.Resources {
		if r.PriceFallback {
			fallbackCount++
		}
	}
	if fallbackCount > 0 {
		fmt.Printf("  %s %s\n",
			report.Yellow("⚠"),
			report.Dim(fmt.Sprintf("%d/%d prices are estimates (API unavailable for these SKUs)", fallbackCount, totalResources)))
	}

	// ── TREND DELTA ──
	sectionSep("TREND")
	if previous != nil {
		current := buildScanRecord(out, subscriptionID)
		delta := history.ComputeDelta(current, *previous)
		renderTrendDelta(delta)
	} else {
		fmt.Printf("\n  %s  %s\n\n",
			report.Dim("TREND"),
			report.Dim("No previous scan within 7 days. Run again to track changes."))
	}
	fmt.Printf("  %s  %s  %s\n\n",
		report.Dim("📈"),
		report.Dim("Full 90-day trend with anomaly detection →"),
		report.Bold("SafeCut Cloud")+" "+report.Dim(report.WaitlistURL))

	// ── TOP TARGETS ──
	sectionSep("TARGETS")
	renderTargets(groups, out)

	// ── INSIGHT ──
	sectionSep("INSIGHT")
	renderInsight(out)

	// ── NEXT STEP + CTA ──
	fmt.Printf("  %s\n", report.Dim(strings.Repeat("─", 62)))
	fmt.Printf("  %s  %s\n",
		report.Section("NEXT"),
		report.Cyan("safecut policy simulate --resource-group <rg> --set mode=protect")+
			" "+report.Dim("what-if analysis"))
	fmt.Println()

	// Multi-subscription detection
	azVal := os.Getenv("AZURE_SUBSCRIPTION_ID")
	armVal := os.Getenv("ARM_SUBSCRIPTION_ID")
	if azVal != "" && armVal != "" && azVal != armVal {
		fmt.Printf("  %s  %s\n",
			report.Dim("🏢"),
			report.Dim("Multiple subscriptions detected (AZURE_SUBSCRIPTION_ID ≠ ARM_SUBSCRIPTION_ID)"))
		fmt.Printf("     %s  %s  %s\n\n",
			report.Dim("→"),
			report.Dim("Cross-subscription scanning & unified dashboard →"),
			report.Bold("SafeCut Cloud")+" "+report.Dim(report.WaitlistURL))
	}

	fmt.Printf("  %s Full evidence report for all %d resources, exportable\n",
		report.Dim("🔒"),
		totalResources)
	fmt.Printf("  %s\n\n",
		report.Dim("dashboards & Slack alerts →")+" "+report.Bold("SafeCut Cloud")+" "+report.Dim(report.WaitlistURL))

	renderROISnapshot(monthlySavings, len(applied), rgCount)
}

// renderROISnapshot prints a conversion block anchored in real numbers: how
// much you'd net by paying $29/mo Solo, how many recs could be automated in
// one click, and (for multi-RG operators) the partner/MSP trail. We skip this
// block entirely when savings are too small for an honest ROI pitch.
func renderROISnapshot(monthlySavings float64, recCount, rgCount int) {
	if monthlySavings < pricing_tiers.SoloMonthlyUSD {
		return
	}
	telemetry.CTAShown("solo", "quick_scan_roi", monthlySavings)
	net := monthlySavings - pricing_tiers.SoloMonthlyUSD
	roi := pricing_tiers.ROIMultiplier(monthlySavings)

	fmt.Printf("  %s\n", report.Section("ROI SNAPSHOT"))
	fmt.Printf("  %s\n", report.Dim("============"))
	fmt.Printf("  %s %s/mo identified in safe recommendations.\n",
		report.BoldGreen("→"), report.Savings(monthlySavings))
	fmt.Printf("  %s Pay Cloud %s  →  %s net savings/mo (%s ROI)\n",
		report.Dim("•"),
		report.Bold(fmt.Sprintf("%s/mo", report.Money0(pricing_tiers.SoloMonthlyUSD))),
		report.Savings(net),
		report.BoldGreen(fmt.Sprintf("%.0fx", roi)))
	if recCount > 0 {
		fmt.Printf("  %s Automate %s in one click  →  %s\n",
			report.Dim("•"),
			report.Bold(fmt.Sprintf("%d recommendation(s)", recCount)),
			report.Cyan("safecut upgrade --start-trial solo"))
	}
	if rgCount >= 2 {
		fmt.Printf("  %s Managing %d resource groups / clients?  →  %s\n",
			report.Dim("•"),
			rgCount,
			report.Cyan("safecut upgrade --partner"))
	}
	fmt.Printf("  %s Enterprise alternative: pay %s (greater applies).  %s\n",
		report.Dim("•"),
		report.Bold("8% of verified savings"),
		report.Cyan("safecut upgrade --book-demo"))
	fmt.Println()
}

// renderReliabilityWarnings emits a yellow "heads-up" block for any
// systemic issues the pipeline flagged: unreachable pricing APIs,
// policy drift, invalid tags, hierarchy-tag probe failures, and recs
// collapsed by the dedup pass. Each section is suppressed when empty
// so a clean run produces no output. This function never modifies
// the pipeline output — it is presentation only.
func renderReliabilityWarnings(out *pipeline.Output) {
	if out == nil {
		return
	}
	hasPricing := len(out.PricingWarnings) > 0
	hasTags := len(out.TagsWarnings) > 0
	hasInvalid := len(out.InvalidTags) > 0
	hasDrift := len(out.Drifts) > 0
	hasSuppressed := len(out.Simulation.SuppressedByDedup) > 0

	if !hasPricing && !hasTags && !hasInvalid && !hasDrift && !hasSuppressed {
		return
	}

	fmt.Println()
	fmt.Printf("  %s\n", report.Section("RELIABILITY WARNINGS"))
	fmt.Printf("  %s\n", report.Dim("====================="))

	if hasPricing {
		fmt.Printf("  %s Pricing API unavailable (%d entries) — affected resources are excluded from TotalSaving.\n",
			report.Yellow("!"), len(out.PricingWarnings))
		for i, msg := range out.PricingWarnings {
			if i >= 5 {
				fmt.Printf("     %s ...and %d more.\n", report.Dim("·"), len(out.PricingWarnings)-5)
				break
			}
			fmt.Printf("     %s %s\n", report.Dim("·"), msg)
		}
	}

	if hasTags {
		fmt.Printf("  %s Hierarchy tag probe failed (%d entries) — policy resolution may fall back to resource-level tags only.\n",
			report.Yellow("!"), len(out.TagsWarnings))
		for i, msg := range out.TagsWarnings {
			if i >= 3 {
				fmt.Printf("     %s ...and %d more.\n", report.Dim("·"), len(out.TagsWarnings)-3)
				break
			}
			fmt.Printf("     %s %s\n", report.Dim("·"), msg)
		}
	}

	if hasInvalid {
		fmt.Printf("  %s Invalid tag values detected on %d resource(s).\n",
			report.Yellow("!"), len(out.InvalidTags))
		for i, t := range out.InvalidTags {
			if i >= 3 {
				fmt.Printf("     %s ...and %d more.\n", report.Dim("·"), len(out.InvalidTags)-3)
				break
			}
			fmt.Printf("     %s %s: %s=%q (%s)\n",
				report.Dim("·"), t.ResourceID, t.Key, t.Value, t.Reason)
		}
	}

	if hasDrift {
		fmt.Printf("  %s Policy drift detected on %d resource(s).\n",
			report.Yellow("!"), len(out.Drifts))
		for i, d := range out.Drifts {
			if i >= 3 {
				fmt.Printf("     %s ...and %d more.\n", report.Dim("·"), len(out.Drifts)-3)
				break
			}
			fmt.Printf("     %s %s: %q (resource) vs %q (from %s %q)\n",
				report.Dim("·"), d.Field, d.ResourceValue, d.ParentValue, d.ParentSource, d.ParentName)
		}
	}

	if hasSuppressed {
		fmt.Printf("  %s Deduplicated recommendations (%d) — multiple rules targeted the same resource; savings counted once.\n",
			report.Yellow("!"), len(out.Simulation.SuppressedByDedup))
		for i, s := range out.Simulation.SuppressedByDedup {
			if i >= 3 {
				fmt.Printf("     %s ...and %d more.\n", report.Dim("·"), len(out.Simulation.SuppressedByDedup)-3)
				break
			}
			fmt.Printf("     %s %s (%s) — %s\n",
				report.Dim("·"), s.Rec.ResourceID, s.Rec.Action, s.Reason)
		}
	}
}

// ─── PIPELINE SUMMARY BLOCK ─────────────────────────────────────────────────

func renderPipelineBlock(out *pipeline.Output, totalResources, idleCount int, subscriptionID string) {
	type stage struct {
		label, desc, ctx string
	}

	orphanCount := len(out.Graph.Orphans())
	linkedCount := totalResources - orphanCount
	graphCtx := fmt.Sprintf("%d linked, %d isolated", linkedCount, orphanCount)
	if orphanCount == totalResources {
		graphCtx = "All nodes isolated"
	} else if linkedCount == totalResources {
		graphCtx = "All nodes connected"
	}

	riskCount := len(out.Simulation.Skipped)
	safetyCtx := fmt.Sprintf("%d risks found", riskCount)
	if riskCount == 0 {
		safetyCtx = "0 risks found"
	}

	regionSet := make(map[string]bool)
	fallbackCount := 0
	for _, r := range out.Snapshot.Resources {
		if r.Location != "" {
			regionSet[r.Location] = true
		}
		if r.PriceFallback {
			fallbackCount++
		}
	}
	regionCount := len(regionSet)
	pricingCtx := "real-time retail prices"
	if fallbackCount > 0 {
		pricingCtx = fmt.Sprintf("%d regions, %d estimated", regionCount, fallbackCount)
	} else if regionCount > 0 {
		pricingCtx = fmt.Sprintf("%d regions, all real-time", regionCount)
	}

	stages := []stage{
		{"DISCOVERY", fmt.Sprintf("Scanning subscription %s", subscriptionID), fmt.Sprintf("%d resources found", totalResources)},
		{"PRICING", "Loading Azure Retail Prices", pricingCtx},
		{"GRAPH", "Building dependency tree", graphCtx},
		{"ENGINE", "Analyzing 14-day telemetry signals", fmt.Sprintf("%d idle detected", idleCount)},
		{"SAFETY", "Simulating blast radius", safetyCtx},
		{"FORECAST", "Projecting 12-month savings", fmt.Sprintf("%s/yr recoverable", report.Money0(out.Forecast.TotalSaving))},
	}

	maxLabel := 0
	for _, s := range stages {
		if len(s.label) > maxLabel {
			maxLabel = len(s.label)
		}
	}

	fmt.Println()
	for _, s := range stages {
		label := fmt.Sprintf("[%s]", s.label)
		descLen := len(s.desc)
		dots := 44 - descLen
		if dots < 3 {
			dots = 3
		}
		fmt.Printf("  %s  %s%s %s %s\n",
			report.Dim(fmt.Sprintf("%-*s", maxLabel+2, label)),
			s.desc,
			strings.Repeat(".", dots),
			report.Green("Done."),
			report.Cyan(s.ctx))
	}
}

// ─── EXECUTIVE DASHBOARD ────────────────────────────────────────────────────

func renderDashboard(yearlySavings float64, safeCount, safeTotal, safetyPct int, confidenceLabel string, autoExecCount int, rgCount int, singleRGName string) {
	headline := buildDynamicHeadline(yearlySavings, safeCount, safeTotal, rgCount, singleRGName)
	title := report.BoldCyan("SAFECUT") + "  |  " + report.BoldWhite(headline)
	savingsLine := report.Bold("ANNUAL SAVINGS") + "    " + report.BoldGreen(fmt.Sprintf("%s / yr", report.Money0(yearlySavings)))
	safetyLine := report.Bold("SAFETY SCORE") + "      " +
		report.BoldGreen(fmt.Sprintf("%d/%d safe", safeCount, safeTotal)) +
		"  " + report.Cyan(fmt.Sprintf("[%s CONFIDENCE]", confidenceLabel))
	autoLine := report.Bold("AUTO-EXECUTE") + "      " +
		fmt.Sprintf("%d actions ready to automate", autoExecCount)

	fmt.Println()
	fmt.Println(boxTop())
	fmt.Println(boxRow(title))
	fmt.Println(boxDiv())
	fmt.Println(boxRow(savingsLine))
	fmt.Println(boxRow(safetyLine))
	fmt.Println(boxRow(autoLine))
	fmt.Println(boxBot())
	fmt.Println()
}

func buildDynamicHeadline(yearlySavings float64, safeCount, safeTotal, rgCount int, singleRGName string) string {
	savings := fmt.Sprintf("%s/yr waste", report.Money0(yearlySavings))

	var location string
	if rgCount == 1 && singleRGName != "" {
		location = fmt.Sprintf("in '%s'", singleRGName)
	} else {
		location = fmt.Sprintf("across %d resource groups", rgCount)
	}

	var safety string
	if safeCount == safeTotal {
		safety = "all safe to automate"
	} else {
		safety = fmt.Sprintf("%d of %d safe to automate", safeCount, safeTotal)
	}

	return fmt.Sprintf("%s %s — %s", savings, location, safety)
}

// ─── TOP TARGETS ────────────────────────────────────────────────────────────

const maxDetailedTargets = 3

func renderTargets(groups []actionGroup, out *pipeline.Output) {
	var actionable []actionGroup
	var reviewOnly []actionGroup
	for _, g := range groups {
		if g.totalSaving > 0 {
			actionable = append(actionable, g)
		} else {
			reviewOnly = append(reviewOnly, g)
		}
	}

	fmt.Printf("  %s\n\n", report.Section("TOP TARGETS"))

	for i, g := range actionable {
		if i < maxDetailedTargets {
			renderTargetCard(i+1, g, out)
		} else {
			renderLockedTargetLine(i+1, g)
		}
	}

	if len(actionable) > maxDetailedTargets {
		lockedCount := len(actionable) - maxDetailedTargets
		lockedResources := 0
		lockedSaving := 0.0
		for i := maxDetailedTargets; i < len(actionable); i++ {
			lockedResources += len(actionable[i].recs)
			lockedSaving += actionable[i].totalSaving
		}
		fmt.Printf("  %s  %s\n",
			report.Dim("🔒"),
			report.Dim(fmt.Sprintf("Full SIGNALS + BLAST analysis for %d more targets (%d resources, $%.0f/mo)",
				lockedCount, lockedResources, lockedSaving)))
		fmt.Printf("     %s  %s\n\n",
			report.Dim("→"),
			report.Bold("SafeCut Cloud")+" "+report.Dim(report.WaitlistURL))
	}

	if len(reviewOnly) > 0 {
		totalReview := 0
		for _, g := range reviewOnly {
			totalReview += len(g.recs)
		}
		fmt.Printf("  %s %s\n\n",
			report.Dim("ℹ"),
			report.Dim(fmt.Sprintf("%d additional resources flagged for review (no confirmed savings)", totalReview)))
	}
}

func renderLockedTargetLine(num int, g actionGroup) {
	action := strings.ToUpper(g.action)
	const actionCol = 60

	var name, fType string
	if len(g.recs) == 1 {
		name = smartTruncate(shortResourceName(g.recs[0].ResourceID))
		fType = friendlyType(g.recs[0].ResourceType)
	} else {
		name = fmt.Sprintf("%d %s", len(g.recs), g.typeLabel)
		fType = ""
	}

	label := name
	if fType != "" {
		label = fmt.Sprintf("%s (%s)", name, fType)
	}

	tIcon := typeIcon(g.recs[0].ResourceType)
	prefix := fmt.Sprintf("  %d. %s ", num, tIcon)
	visLen := visualLen(prefix) + len(label)

	savingStr := fmt.Sprintf("$%.2f/mo", g.totalSaving)
	gap := actionCol - visLen - len(action)
	if gap < 2 {
		gap = 2
	}

	fmt.Printf("%s%s%s%s  %s\n",
		prefix,
		report.Dim(label),
		strings.Repeat(" ", gap),
		report.Dim(action),
		report.Dim(savingStr))
}

func renderTargetCard(num int, g actionGroup, out *pipeline.Output) {
	action := strings.ToUpper(g.action)

	const actionCol = 60

	if len(g.recs) == 1 {
		rec := g.recs[0]
		name := shortResourceName(rec.ResourceID)
		fType := friendlyType(rec.ResourceType)

		prefix := fmt.Sprintf("  %d. %s ", num, typeIcon(rec.ResourceType))
		label := fmt.Sprintf("%s (%s)", name, fType)
		visLen := visibleLen(prefix) + len(label)
		gap := actionCol - visLen - len(action)
		if gap < 2 {
			gap = 2
		}
		fmt.Printf("%s%s%s%s\n",
			prefix,
			report.Bold(label),
			strings.Repeat(" ", gap),
			actionColor(action))

		if rec.Action == "rightsize" || rec.Action == "reserve" {
			// Rightsizing and RI cards show reason directly
			fmt.Printf("     ├─ %s  %s\n", report.Dim("REASON: "), rec.Reason)
			if rec.PolicyNote != "" {
				fmt.Printf("     ├─ %s  %s\n", report.Dim("NOTE:   "), report.Dim(rec.PolicyNote))
			}
		} else {
			sigLine := buildSignalLine(rec)
			fmt.Printf("     ├─ %s  %s\n", report.Dim("SIGNALS:"), sigLine)

			blastLine := buildBlastLine(rec.ResourceID, rec.ResourceType, out)
			fmt.Printf("     ├─ %s  %s\n", report.Dim("BLAST:  "), blastLine)
		}

		fmt.Printf("     ├─ %s  %s\n", report.Dim("SAFETY: "), buildSafetyLine(rec, out))

		savingLine := report.BoldGreen(fmt.Sprintf("$%.2f/mo", rec.MonthlySave))
		daily := rec.MonthlySave / 30
		if daily >= 10.0 {
			savingLine += "  " + report.BoldRed(fmt.Sprintf("($%.2f/day burning idle)", daily))
		} else if daily >= 1.0 {
			savingLine += "  " + report.Yellow(fmt.Sprintf("($%.2f/day burning idle)", daily))
		}
		fmt.Printf("     └─ %s  %s\n", report.Dim("SAVING: "), savingLine)

	} else {
		desc := fmt.Sprintf("%d %s", len(g.recs), g.typeLabel)
		tIcon := typeIcon(g.recs[0].ResourceType)
		prefix := fmt.Sprintf("  %d. %s ", num, tIcon)
		visLen := visibleLen(prefix) + len(desc)
		gap := actionCol - visLen - len(action)
		if gap < 2 {
			gap = 2
		}
		fmt.Printf("%s%s%s%s\n",
			prefix,
			report.Bold(desc),
			strings.Repeat(" ", gap),
			actionColor(action))

		renderGroupTargetList(g.recs)

		patternLine := g.pattern
		if patternLine == "" {
			patternLine = groupReason(g)
		}
		fmt.Printf("     ├─ %s  %s\n", report.Dim("PATTERN:"), patternLine)

		blastLine := buildGroupBlastLine(g, out)
		fmt.Printf("     ├─ %s  %s\n", report.Dim("BLAST:  "), blastLine)

		// SAVING
		savingLine := report.BoldGreen(fmt.Sprintf("$%.2f/mo", g.totalSaving))
		daily := g.totalSaving / 30
		if daily >= 10.0 {
			savingLine += "  " + report.BoldRed(fmt.Sprintf("($%.2f/day wasted)", daily))
		} else if daily >= 1.0 {
			savingLine += "  " + report.Yellow(fmt.Sprintf("($%.2f/day wasted)", daily))
		}
		fmt.Printf("     └─ %s  %s\n", report.Dim("SAVING: "), savingLine)
	}

	fmt.Println()
}

func renderGroupTargetList(recs []engine.Recommendation) {
	const sampleSize = 3

	names := make([]string, len(recs))
	for i, r := range recs {
		names[i] = smartTruncate(shortResourceName(r.ResourceID))
	}
	sort.Strings(names)

	show := sampleSize
	if len(names) <= sampleSize+1 {
		show = len(names)
	}

	for i := 0; i < show; i++ {
		fmt.Printf("     %s %s %s\n", report.Dim("│"), report.Dim("•"), names[i])
	}
	if remaining := len(names) - show; remaining > 0 {
		fmt.Printf("     %s %s\n", report.Dim("│"), report.Dim(fmt.Sprintf("[+ %d more]", remaining)))
	}
}

func smartTruncate(name string) string {
	const maxLen = 38
	if len(name) <= maxLen {
		return name
	}
	dashIdx := strings.LastIndex(name, "-")
	if dashIdx > 0 {
		suffix := name[dashIdx:]
		if len(suffix) <= maxLen-4 {
			prefixBudget := maxLen - len(suffix) - 2
			if prefixBudget < 4 {
				prefixBudget = 4
			}
			return name[:prefixBudget] + ".." + suffix
		}
	}
	return name[:maxLen-3] + "..."
}

func typeIcon(resourceType string) string {
	t := strings.ToLower(resourceType)
	switch {
	case strings.Contains(t, "virtualmachines"):
		return "🖥️ "
	case strings.Contains(t, "publicipaddresses"):
		return "🌐"
	case strings.Contains(t, "disks"):
		return "💾"
	case strings.Contains(t, "microsoft.web/sites"):
		return "🌍"
	case strings.Contains(t, "microsoft.sql"):
		return "🗄️ "
	case strings.Contains(t, "storageaccounts"):
		return "📁"
	case strings.Contains(t, "loadbalancers"):
		return "⚖️ "
	case strings.Contains(t, "natgateways"):
		return "🚪"
	case strings.Contains(t, "containergroups"):
		return "📦"
	default:
		return "📦"
	}
}

func visibleLen(s string) int {
	inEsc := false
	n := 0
	for _, r := range s {
		if r == '\033' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		n++
	}
	return n
}

func buildSignalLine(rec engine.Recommendation) string {
	if rec.Analysis != nil && len(rec.Analysis.Signals) > 0 {
		return formatSignalSummary(rec.Analysis.Signals, rec.ResourceType)
	}
	return singleReason(rec)
}

// buildSafetyLine explains why a recommendation is safe (or not) to apply.
// It summarizes auto-execute gating, locks, snapshots, and dependent blocks
// so the operator can trust or question the recommendation at a glance.
func buildSafetyLine(rec engine.Recommendation, out *pipeline.Output) string {
	var parts []string

	if rec.AutoExecute {
		parts = append(parts, report.Green("auto-apply safe"))
	} else {
		parts = append(parts, report.Yellow("manual review required"))
	}

	switch rec.Risk {
	case engine.RiskLow:
		parts = append(parts, report.Dim("risk=low"))
	case engine.RiskMedium:
		parts = append(parts, report.Yellow("risk=medium"))
	case engine.RiskHigh:
		parts = append(parts, report.BoldRed("risk=high"))
	}

	if out != nil && out.Graph != nil {
		if node, ok := out.Graph.GetNode(rec.ResourceID); ok && len(node.Children) > 0 {
			parts = append(parts, report.Yellow(fmt.Sprintf("%d dependents", len(node.Children))))
		}
	}

	if rec.PolicyNote != "" {
		note := strings.ToLower(rec.PolicyNote)
		switch {
		case strings.Contains(note, "locked"):
			parts = append(parts, report.BoldRed("lock present"))
		case strings.Contains(note, "snapshot"):
			parts = append(parts, report.Green("snapshot available"))
		}
	}

	return strings.Join(parts, " · ")
}

func buildBlastLine(resourceID, resourceType string, out *pipeline.Output) string {
	node, ok := out.Graph.GetNode(resourceID)
	if !ok {
		return report.Green(isolatedBlastNarrative(resourceType))
	}
	if node.Parent == nil && len(node.Children) == 0 {
		return report.Green(isolatedBlastNarrative(resourceType))
	}

	var parts []string
	if node.Parent != nil {
		parts = append(parts, fmt.Sprintf("owned by %s", report.Bold(shortResourceName(node.Parent.ID))))
	}
	if len(node.Children) > 0 {
		names := make([]string, len(node.Children))
		for i, c := range node.Children {
			names[i] = shortResourceName(c.ID)
		}
		parts = append(parts, fmt.Sprintf("%d dependents: %s", len(node.Children), strings.Join(names, ", ")))
	}

	if len(node.Children) > 0 {
		return report.BoldRed("⚠ BLOCKED — ") + report.Yellow(strings.Join(parts, " · "))
	}
	return report.Yellow("Connected: " + strings.Join(parts, " · "))
}

func isolatedBlastNarrative(resourceType string) string {
	t := strings.ToLower(resourceType)
	switch {
	case strings.Contains(t, "publicipaddresses"):
		return "No NIC references this IP — likely leftover from deleted deployment"
	case strings.Contains(t, "disks"):
		return "Disk detached — the VM it belonged to was deleted or never existed"
	case strings.Contains(t, "virtualmachines"):
		return "Standalone VM — no attached resources depend on it"
	case strings.Contains(t, "natgateways"):
		return "No subnets attached — gateway is disconnected from network"
	case strings.Contains(t, "loadbalancers"):
		return "No backends configured — balancer has nothing to balance"
	case strings.Contains(t, "microsoft.web/sites"):
		return "Standalone App Service — no dependent resources detected"
	case strings.Contains(t, "microsoft.sql"):
		return "Standalone database — no linked services depend on it"
	case strings.Contains(t, "containergroups"):
		return "Standalone container group — no dependencies detected"
	default:
		return "Isolated. No downstream dependencies detected."
	}
}

func buildGroupBlastLine(g actionGroup, out *pipeline.Output) string {
	connected := 0
	parentNames := make(map[string]bool)
	for _, rec := range g.recs {
		node, ok := out.Graph.GetNode(rec.ResourceID)
		if !ok {
			continue
		}
		if node.Parent != nil || len(node.Children) > 0 {
			connected++
		}
		if node.Parent != nil {
			parentNames[shortResourceName(node.Parent.ID)] = true
		}
	}
	if connected == 0 {
		return report.Green(isolatedGroupBlastNarrative(g.recs[0].ResourceType))
	}
	if len(parentNames) > 0 {
		parents := make([]string, 0, len(parentNames))
		for p := range parentNames {
			parents = append(parents, p)
		}
		return report.Yellow(fmt.Sprintf("%d of %d connected (owners: %s)", connected, len(g.recs), strings.Join(parents, ", ")))
	}
	return report.Yellow(fmt.Sprintf("%d of %d have active dependencies — review before bulk action.", connected, len(g.recs)))
}

func isolatedGroupBlastNarrative(resourceType string) string {
	t := strings.ToLower(resourceType)
	switch {
	case strings.Contains(t, "disks"):
		return "All detached. No VMs depend on these disks."
	case strings.Contains(t, "publicipaddresses"):
		return "All orphaned. No NICs reference these IPs."
	case strings.Contains(t, "loadbalancers"):
		return "All empty. No backends behind these balancers."
	case strings.Contains(t, "natgateways"):
		return "All disconnected. No subnets attached to these gateways."
	case strings.Contains(t, "containergroups"):
		return "All stopped/failed. No live services depend on them."
	default:
		return "All isolated. No downstream impact."
	}
}

// ─── INSIGHT ENGINE ─────────────────────────────────────────────────────────

func renderInsight(out *pipeline.Output) {
	applied := out.Simulation.Applied
	if len(applied) == 0 {
		return
	}

	totalSaving := 0.0
	rgSavings := make(map[string]float64)
	typeSavings := make(map[string]float64)
	hasGovernanceTags := false

	for _, rec := range applied {
		totalSaving += rec.MonthlySave
		rg := extractResourceGroup(rec.ResourceID)
		if rg != "" {
			rgSavings[rg] += rec.MonthlySave
		}
		ft := friendlyType(rec.ResourceType)
		typeSavings[ft] += rec.MonthlySave
	}

	for _, pol := range out.Policies {
		if pol != nil && (pol.Mode != "" || pol.Criticality != "") {
			hasGovernanceTags = true
			break
		}
	}

	// Priority 1: RG concentration >60%
	for rg, saving := range rgSavings {
		if totalSaving > 0 && saving/totalSaving >= 0.60 {
			pct := int(saving / totalSaving * 100)
			fmt.Printf("  %s %s\n", report.BoldYellow("[!]"), report.Section("WASTE HOTSPOT DETECTED"))
			fmt.Printf("  %s of recoverable cost is concentrated in resource group %s.\n",
				report.Bold(fmt.Sprintf("%d%%", pct)),
				report.BoldCyan("'"+rg+"'"))
			fmt.Printf("  This is not random — it's a systemic gap. Apply the %s policy\n", report.Cyan("'development'"))
			fmt.Printf("  template to auto-classify and prevent future accumulation.\n\n")
			return
		}
	}

	// Priority 2: Type skew >80%
	for typ, saving := range typeSavings {
		if totalSaving > 0 && saving/totalSaving >= 0.80 {
			pct := int(saving / totalSaving * 100)
			fmt.Printf("  %s %s\n", report.BoldYellow("[!]"), report.Section("RESOURCE LEAK PATTERN"))
			fmt.Printf("  %s of your waste is a single category: %s.\n",
				report.Bold(fmt.Sprintf("%d%%", pct)),
				report.BoldCyan(strings.ToLower(typ)+"s"))
			fmt.Printf("  This signals a broken decommission pipeline. A CI/CD post-deploy\n")
			fmt.Printf("  hook would eliminate this class of waste permanently.\n\n")
			return
		}
	}

	// Priority 3: No governance tags
	if !hasGovernanceTags {
		fmt.Printf("  %s %s\n", report.BoldYellow("[!]"), report.Section("GOVERNANCE GAP"))
		fmt.Printf("  Your infrastructure is running %s — no governance tags detected.\n",
			report.BoldRed("\"blind\""))
		fmt.Printf("  Safety filters are defaulted to conservative. Run %s to\n",
			report.Cyan("safecut policy simulate"))
		fmt.Printf("  unlock up to %s more aggressive savings.\n\n",
			report.BoldGreen("2x"))
		return
	}

	// Priority 4: Rightsizing opportunity
	rightsizeCount := 0
	rightsizeSaving := 0.0
	riCount := 0
	riSaving := 0.0
	for _, rec := range applied {
		switch rec.Action {
		case "rightsize":
			rightsizeCount++
			rightsizeSaving += rec.MonthlySave
		case "reserve":
			riCount++
			riSaving += rec.MonthlySave
		}
	}
	if rightsizeCount > 0 && rightsizeSaving > riSaving {
		fmt.Printf("  %s %s\n", report.BoldYellow("⚙"), report.Section("RIGHTSIZING OPPORTUNITY"))
		fmt.Printf("  %d VMs are over-provisioned — using less than 30%% of allocated CPU.\n",
			rightsizeCount)
		fmt.Printf("  Downsizing saves %s without touching your architecture.\n\n",
			report.BoldGreen(fmt.Sprintf("$%.0f/mo", rightsizeSaving)))
		return
	}
	if riCount > 0 {
		fmt.Printf("  %s %s\n", report.BoldGreen("📋"), report.Section("COMMITMENT ADVISOR"))
		fmt.Printf("  %d VMs run 24/7 and could save %s with a 1-year Reserved Instance.\n",
			riCount, report.BoldGreen(fmt.Sprintf("$%.0f/mo", riSaving)))
		fmt.Printf("  No architecture change needed — just a billing switch.\n\n")
		return
	}

	// Priority 5: Clean bill
	if totalSaving < 50 {
		fmt.Printf("  %s %s\n", report.Green("✓"), report.Section("CLEAN INFRASTRUCTURE"))
		fmt.Printf("  No significant waste detected. Re-run periodically or\n")
		fmt.Printf("  get continuous monitoring with %s\n\n",
			report.Bold("SafeCut Cloud")+" "+report.Dim(report.WaitlistURL))
	}
}

// ─── Grouping and pattern detection ─────────────────────────────────────────

func deduplicateByResource(applied []engine.Recommendation) []engine.Recommendation {
	best := make(map[string]engine.Recommendation)
	for _, rec := range applied {
		if existing, ok := best[rec.ResourceID]; ok {
			if rec.MonthlySave > existing.MonthlySave {
				best[rec.ResourceID] = rec
			}
		} else {
			best[rec.ResourceID] = rec
		}
	}
	result := make([]engine.Recommendation, 0, len(best))
	for _, rec := range best {
		result = append(result, rec)
	}
	return result
}

func buildActionGroups(applied []engine.Recommendation) []actionGroup {
	applied = deduplicateByResource(applied)

	sort.Slice(applied, func(i, j int) bool {
		return applied[i].MonthlySave > applied[j].MonthlySave
	})

	key := func(r engine.Recommendation) string {
		return strings.ToLower(r.Action + "|" + friendlyType(r.ResourceType))
	}

	groupMap := make(map[string]*actionGroup)
	var order []string

	for _, rec := range applied {
		k := key(rec)
		if g, ok := groupMap[k]; ok {
			g.recs = append(g.recs, rec)
			g.totalSaving += rec.MonthlySave
		} else {
			order = append(order, k)
			groupMap[k] = &actionGroup{
				action:      rec.Action,
				typeLabel:   friendlyTypePlural(rec.ResourceType),
				resourceKey: strings.ToLower(rec.ResourceType),
				recs:        []engine.Recommendation{rec},
				totalSaving: rec.MonthlySave,
			}
		}
	}

	// Detect patterns per group
	for _, g := range groupMap {
		g.pattern = detectPattern(g)
	}

	// Pull out the costliest individual resource as its own group
	// if it represents >35% of total savings and its group has >1 item.
	totalSavings := 0.0
	for _, g := range groupMap {
		totalSavings += g.totalSaving
	}

	var result []actionGroup
	topExtracted := false
	if len(applied) > 0 && totalSavings > 0 {
		topRec := applied[0]
		topKey := key(topRec)
		topGroup := groupMap[topKey]
		if len(topGroup.recs) > 1 && topRec.MonthlySave/totalSavings > 0.35 {
			solo := actionGroup{
				action:      topRec.Action,
				typeLabel:   friendlyTypePlural(topRec.ResourceType),
				resourceKey: strings.ToLower(topRec.ResourceType),
				recs:        []engine.Recommendation{topRec},
				totalSaving: topRec.MonthlySave,
			}
			result = append(result, solo)
			topGroup.recs = topGroup.recs[1:]
			topGroup.totalSaving -= topRec.MonthlySave
			topExtracted = true
		}
	}

	// Collect remaining groups sorted by total savings
	remaining := make([]actionGroup, 0, len(order))
	for _, k := range order {
		g := groupMap[k]
		if len(g.recs) > 0 {
			remaining = append(remaining, *g)
		}
	}
	sort.Slice(remaining, func(i, j int) bool {
		return remaining[i].totalSaving > remaining[j].totalSaving
	})

	if !topExtracted && len(remaining) > 0 {
		result = remaining
	} else {
		result = append(result, remaining...)
	}

	return result
}

func detectPattern(g *actionGroup) string {
	if len(g.recs) < 2 {
		return ""
	}

	action := strings.ToLower(g.action)
	t := strings.ToLower(g.recs[0].ResourceType)

	if action == "rightsize" {
		return "VMs consistently using <30% of allocated CPU — downsize saves without performance impact"
	}
	if action == "reserve" {
		return fmt.Sprintf("%d VMs running steady 24/7 — Reserved Instance commitment saves 36-56%%", len(g.recs))
	}

	if strings.Contains(t, "publicipaddresses") {
		natCount := 0
		for _, r := range g.recs {
			name := strings.ToLower(shortResourceName(r.ResourceID))
			if strings.Contains(name, "natg") || strings.Contains(name, "nat-") || strings.Contains(name, "nat_") {
				natCount++
			}
		}
		if natCount > len(g.recs)/2 {
			return "All from NAT gateways — likely leftover from deployments"
		}
		return "Not associated to any NIC or load balancer"
	}

	if strings.Contains(t, "disks") {
		rgCount := make(map[string]int)
		for _, r := range g.recs {
			parts := strings.Split(r.ResourceID, "/")
			for i, p := range parts {
				if strings.EqualFold(p, "resourceGroups") && i+1 < len(parts) {
					rgCount[parts[i+1]]++
					break
				}
			}
		}
		if len(rgCount) == 1 {
			for rg := range rgCount {
				return fmt.Sprintf("All in resource group %s — possible decommission candidate", rg)
			}
		}
	}

	return ""
}

func groupReason(g actionGroup) string {
	if len(g.recs) == 1 {
		return singleReason(g.recs[0])
	}
	action := strings.ToLower(g.action)
	if action == "rightsize" {
		return "CPU utilization consistently below 30% — can safely downsize."
	}
	if action == "reserve" {
		return "Running 24/7 for 90+ days — strong candidate for Reserved Instance."
	}
	t := strings.ToLower(g.recs[0].ResourceType)
	switch {
	case strings.Contains(t, "publicipaddresses"):
		return "These IPs are not associated to any NIC or load balancer."
	case strings.Contains(t, "disks"):
		return "Disks in \"Unattached\" state with no VM reference."
	case strings.Contains(t, "virtualmachines"):
		return "Zero CPU, network, and disk activity for 14 days."
	default:
		return "Zero activity across all signals."
	}
}

func singleReason(rec engine.Recommendation) string {
	if strings.Contains(rec.Reason, "not associated") {
		return "Not associated to any NIC or load balancer."
	}
	if strings.Contains(rec.Reason, "not attached") {
		return "Disk not attached to any VM — generating idle cost."
	}

	if rec.Analysis != nil {
		hasNonZero := false
		for _, sig := range rec.Analysis.Signals {
			if sig.Value > 0 {
				hasNonZero = true
				break
			}
		}
		if hasNonZero {
			return formatSignalSummary(rec.Analysis.Signals, rec.ResourceType) + "."
		}
	}

	return narrativeSignal(rec.ResourceType)
}

func extractResourceGroup(resourceID string) string {
	parts := strings.Split(resourceID, "/")
	for i, p := range parts {
		if strings.EqualFold(p, "resourceGroups") && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func actionColor(action string) string {
	switch action {
	case "STOP", "DELETE":
		return report.BoldRed(action)
	case "DEALLOCATE", "DOWNGRADE", "RIGHTSIZE":
		return report.BoldYellow(action)
	case "RESERVE":
		return report.BoldGreen(action)
	case "REVIEW":
		return report.Cyan(action)
	default:
		return report.BoldCyan(action)
	}
}

func friendlyType(resourceType string) string {
	t := strings.ToLower(resourceType)
	switch {
	case strings.Contains(t, "virtualmachines"):
		return "VM"
	case strings.Contains(t, "publicipaddresses"):
		return "Public IP"
	case strings.Contains(t, "disks"):
		return "Disk"
	case strings.Contains(t, "microsoft.web/sites"):
		return "App Service"
	case strings.Contains(t, "microsoft.sql"):
		return "SQL Database"
	case strings.Contains(t, "storageaccounts"):
		return "Storage Account"
	case strings.Contains(t, "loadbalancers"):
		return "Load Balancer"
	case strings.Contains(t, "natgateways"):
		return "NAT Gateway"
	case strings.Contains(t, "containergroups"):
		return "Container Group"
	default:
		return "Resource"
	}
}

func friendlyTypePlural(resourceType string) string {
	t := strings.ToLower(resourceType)
	switch {
	case strings.Contains(t, "virtualmachines"):
		return "VMs"
	case strings.Contains(t, "publicipaddresses"):
		return "public IPs"
	case strings.Contains(t, "disks"):
		return "unattached disks"
	case strings.Contains(t, "microsoft.web/sites"):
		return "App Services"
	case strings.Contains(t, "microsoft.sql"):
		return "SQL databases"
	case strings.Contains(t, "storageaccounts"):
		return "storage accounts"
	case strings.Contains(t, "loadbalancers"):
		return "load balancers"
	case strings.Contains(t, "natgateways"):
		return "NAT gateways"
	case strings.Contains(t, "containergroups"):
		return "container groups"
	default:
		return "resources"
	}
}

func formatSignalSummary(signals []engine.SignalResult, resourceType string) string {
	merged := mergeSignals(signals)

	allSilent := true
	for _, m := range merged {
		if m.pct > 0 {
			allSilent = false
			break
		}
	}

	if allSilent {
		return narrativeSignal(resourceType)
	}

	parts := make([]string, 0, len(merged))
	for _, m := range merged {
		parts = append(parts, m.display)
	}
	return strings.Join(parts, report.Dim(" · "))
}

func narrativeSignal(resourceType string) string {
	t := strings.ToLower(resourceType)
	switch {
	case strings.Contains(t, "virtualmachines"):
		return report.Dim("Ghost VM") + " — zero activity across all signals for 14 days"
	case strings.Contains(t, "publicipaddresses"):
		return report.Dim("Silent IP") + " — no traffic in/out for 14 days"
	case strings.Contains(t, "disks"):
		return report.Dim("Cold storage") + " — zero read/write activity for 14 days"
	case strings.Contains(t, "microsoft.web/sites"):
		return report.Dim("Dormant App") + " — no requests or compute activity detected"
	case strings.Contains(t, "microsoft.sql"):
		return report.Dim("Silent DB") + " — no queries or connections for 14 days"
	case strings.Contains(t, "loadbalancers"):
		return report.Dim("Empty balancer") + " — no backend traffic routed"
	case strings.Contains(t, "natgateways"):
		return report.Dim("Disconnected gateway") + " — no subnet traffic"
	case strings.Contains(t, "containergroups"):
		return report.Dim("Dead container") + " — stopped or failed, no activity"
	default:
		return report.Dim("Flatline") + " — no measurable activity for 14 days"
	}
}

type mergedSignal struct {
	label   string
	pct     float64
	display string
}

func mergeSignals(signals []engine.SignalResult) []mergedSignal {
	buckets := map[string]float64{}
	for _, s := range signals {
		switch s.Name {
		case "cpu":
			buckets["CPU"] = signalPct(s)
		case "network_in", "network_out":
			if signalPct(s) > buckets["Net"] {
				buckets["Net"] = signalPct(s)
			}
		case "disk_write", "disk_read":
			if signalPct(s) > buckets["Disk"] {
				buckets["Disk"] = signalPct(s)
			}
		default:
			label := titleASCII(strings.ReplaceAll(s.Name, "_", " "))
			buckets[label] = signalPct(s)
		}
	}

	order := []string{"CPU", "Net", "Disk"}
	for label := range buckets {
		found := false
		for _, o := range order {
			if o == label {
				found = true
				break
			}
		}
		if !found {
			order = append(order, label)
		}
	}

	result := make([]mergedSignal, 0, len(order))
	for _, label := range order {
		pct, ok := buckets[label]
		if !ok {
			continue
		}
		bar := signalBar(pct)
		var display string
		if pct == 0 {
			display = fmt.Sprintf("%s %s", report.Dim(label), bar)
		} else if label == "CPU" {
			display = fmt.Sprintf("%s %s %.1f%%", label, bar, pct)
		} else {
			display = fmt.Sprintf("%s %s", label, bar)
		}
		result = append(result, mergedSignal{label: label, pct: pct, display: display})
	}
	return result
}

func signalBar(pct float64) string {
	const width = 5
	filled := int(pct / 100.0 * float64(width))
	if pct > 0 && filled == 0 {
		filled = 1
	}
	if filled > width {
		filled = width
	}
	bar := strings.Repeat("▓", filled) + strings.Repeat("░", width-filled)
	return report.Dim("[") + bar + report.Dim("]")
}

// titleASCII capitalises the first letter of each whitespace-separated
// word. Signal names are always ASCII (disk_read, network_in, etc.),
// so we avoid pulling in golang.org/x/text/cases — the only reason
// strings.Title was deprecated was its Unicode handling, which does
// not apply here.
func titleASCII(s string) string {
	b := []byte(s)
	start := true
	for i, c := range b {
		if c == ' ' {
			start = true
			continue
		}
		if start && c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
		start = false
	}
	return string(b)
}

func signalPct(sig engine.SignalResult) float64 {
	if sig.Name == "cpu" {
		return sig.Value
	}
	if sig.Value == 0 {
		return 0
	}
	const refBytes = 10 * 1024 * 1024
	pct := sig.Value / refBytes * 100
	if pct > 100 {
		pct = 100
	}
	return pct
}

func trackQuickScan(out *pipeline.Output) {
	totalResources := len(out.Snapshot.Resources) + len(out.Protected)
	idleCount := 0
	for _, a := range out.Analyses {
		if a != nil && a.Score >= 0.85 {
			idleCount++
		}
	}
	var criticalRisks int
	for _, r := range out.Decisions {
		if r.Risk == engine.RiskHigh {
			criticalRisks++
		}
	}
	telemetry.Track("quick_scan_completed", map[string]interface{}{
		"resource_count":  totalResources,
		"recommendations": len(out.Simulation.Applied),
		"idle_count":      idleCount,
		"critical_risks":  criticalRisks,
	})
}

func shortResourceName(id string) string {
	name := id
	for i := len(id) - 1; i >= 0; i-- {
		if id[i] == '/' {
			name = id[i+1:]
			break
		}
	}
	return name
}

// ─── History & Trend ─────────────────────────────────────────────────────────

func buildScanRecord(out *pipeline.Output, subscriptionID string) history.ScanRecord {
	idleCount := 0
	for _, a := range out.Analyses {
		if a != nil && a.Score >= 0.85 {
			idleCount++
		}
	}

	actions := make(map[string]int)
	for _, rec := range out.Simulation.Applied {
		actions[rec.Action]++
	}

	return history.ScanRecord{
		Timestamp:      time.Now(),
		SubscriptionID: subscriptionID,
		TotalResources: len(out.Snapshot.Resources) + len(out.Protected),
		IdleDetected:   idleCount,
		MonthlySaving:  out.Simulation.TotalSaving,
		YearlySaving:   out.Forecast.TotalSaving,
		RecCount:       len(out.Simulation.Applied),
		RiskCount:      len(out.Simulation.Skipped),
		Actions:        actions,
	}
}

func saveHistory(out *pipeline.Output, subscriptionID string) {
	record := buildScanRecord(out, subscriptionID)
	if err := history.Save(record); err != nil {
		// We don't want history failures to block the scan output,
		// but silently swallowing them means users lose trend deltas
		// forever without knowing why. Surface it as a warning.
		fmt.Printf("  %s Could not persist scan history: %v\n",
			report.Yellow("!"), err)
		fmt.Printf("     %s Trend comparison on next run will be unavailable.\n",
			report.Dim("·"))
	}
	if err := history.Cleanup(7); err != nil {
		fmt.Printf("  %s Could not prune old history entries: %v\n",
			report.Yellow("!"), err)
	}
}

func renderTrendDelta(d history.Delta) {
	if d.Previous == nil {
		return
	}

	parts := []string{}

	if d.ResourceDelta > 0 {
		parts = append(parts, report.Yellow(fmt.Sprintf("+%d resources", d.ResourceDelta)))
	} else if d.ResourceDelta < 0 {
		parts = append(parts, report.Green(fmt.Sprintf("%d resources", d.ResourceDelta)))
	}

	if d.SavingDelta > 1.0 {
		parts = append(parts, report.Yellow(fmt.Sprintf("+$%.0f/mo waste", d.SavingDelta)))
	} else if d.SavingDelta < -1.0 {
		parts = append(parts, report.Green(fmt.Sprintf("-$%.0f/mo waste", -d.SavingDelta)))
	}

	if d.RecDelta > 0 {
		parts = append(parts, report.Yellow(fmt.Sprintf("+%d findings", d.RecDelta)))
	} else if d.RecDelta < 0 {
		parts = append(parts, report.Green(fmt.Sprintf("%d findings", d.RecDelta)))
	}

	if len(parts) == 0 {
		fmt.Printf("  %s  %s\n\n",
			report.Dim("TREND"),
			report.Dim(fmt.Sprintf("No change since last scan (%dd ago)", d.DaysSince)))
		return
	}

	label := "TREND"
	if d.DaysSince > 0 {
		label = fmt.Sprintf("TREND (%dd)", d.DaysSince)
	}
	fmt.Printf("  %s  %s\n\n",
		report.Dim(label),
		strings.Join(parts, report.Dim(" · ")))
}

func exportQuickScanMD(out *pipeline.Output, subscriptionID, path string) {
	totalResources := len(out.Snapshot.Resources) + len(out.Protected)
	monthlySavings := out.Simulation.TotalSaving
	yearlySavings := out.Forecast.TotalSaving
	safeCount := len(out.Simulation.Applied)
	heldCount := len(out.Simulation.Skipped)
	safeTotal := safeCount + heldCount

	autoExecCount := 0
	for _, rec := range out.Simulation.Applied {
		if rec.AutoExecute {
			autoExecCount++
		}
	}

	var b strings.Builder
	b.WriteString("# SafeCut Scan Report\n\n")
	b.WriteString(fmt.Sprintf("**Subscription:** `%s`\n", subscriptionID))
	b.WriteString(fmt.Sprintf("**Date:** %s\n", time.Now().Format("2006-01-02 15:04 MST")))
	b.WriteString(fmt.Sprintf("**Resources scanned:** %d\n\n", totalResources))

	b.WriteString("## Executive Summary\n\n")
	b.WriteString("| Metric | Value |\n")
	b.WriteString("|--------|-------|\n")
	b.WriteString(fmt.Sprintf("| Annual Savings | %s/yr |\n", report.Money(yearlySavings)))
	b.WriteString(fmt.Sprintf("| Monthly Savings | %s/mo |\n", report.Money(monthlySavings)))
	b.WriteString(fmt.Sprintf("| Safety Score | %d/%d safe |\n", safeCount, safeTotal))
	b.WriteString(fmt.Sprintf("| Auto-executable | %d actions |\n\n", autoExecCount))

	groups := buildActionGroups(out.Simulation.Applied)
	topN := maxDetailedTargets
	if topN > len(groups) {
		topN = len(groups)
	}

	if topN > 0 {
		b.WriteString("## Top Targets\n\n")
		for i := 0; i < topN; i++ {
			g := groups[i]
			if len(g.recs) == 1 {
				rec := g.recs[0]
				b.WriteString(fmt.Sprintf("%d. **%s** (%s) — %s — $%.2f/mo\n",
					i+1,
					shortResourceName(rec.ResourceID),
					friendlyType(rec.ResourceType),
					strings.ToUpper(rec.Action),
					rec.MonthlySave))
			} else {
				b.WriteString(fmt.Sprintf("%d. **%d %s** — %s — $%.2f/mo\n",
					i+1,
					len(g.recs),
					g.typeLabel,
					strings.ToUpper(g.action),
					g.totalSaving))
			}
		}
		b.WriteString("\n")
	}

	b.WriteString("---\n\n")
	b.WriteString("## Detailed Analysis\n\n")
	b.WriteString("Full per-resource breakdown with **SIGNALS**, **BLAST RADIUS**, and **evidence logs**\n")
	b.WriteString("is available with **SafeCut Cloud**.\n\n")
	b.WriteString(fmt.Sprintf("> [Join the waitlist](%s) to unlock exportable PDF/HTML reports with branding,\n", report.WaitlistURL))
	b.WriteString("> Slack integration, and continuous monitoring dashboards.\n")

	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		fmt.Printf("  %s Failed to write export: %s\n", report.BoldRed("✗"), err)
		return
	}

	fmt.Printf("\n  %s  Report summary saved to %s\n",
		report.Green("✓"),
		report.Bold(path))
	fmt.Printf("  %s  Full detailed report with SIGNALS + BLAST → %s  %s\n\n",
		report.Dim("🔒"),
		report.Bold("SafeCut Cloud"),
		report.Dim(report.WaitlistURL))
}

func init() {
	rootCmd.AddCommand(quickScanCmd)
	quickScanCmd.Flags().String("subscription", "", "Azure subscription ID (or set AZURE_SUBSCRIPTION_ID)")
	quickScanCmd.Flags().String("resource-group", "", "Limit scan to one resource group (faster, fewer API calls for metrics)")
	quickScanCmd.Flags().Bool("progress", false, "With -o json: print scan progress to stderr until JSON is ready")
	quickScanCmd.Flags().String("export", "", "Export scan summary to file (e.g. report.md)")
	quickScanCmd.Flags().String("cloud", "azure", "Target cloud (azure | aws | gcp). aws/gcp are waitlist-only in v1.0.")
}
