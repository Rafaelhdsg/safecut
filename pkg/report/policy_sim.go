package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/Rafaelhdsg/safecut/internal/engine"
	"github.com/Rafaelhdsg/safecut/internal/pipeline"
)

// RenderPolicySim writes the full policy simulation report to w.
func RenderPolicySim(w io.Writer, result *pipeline.PolicySimResult, format Format) error {
	switch format {
	case FormatJSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	case FormatTable, FormatASCII:
		return renderPolicySimTable(w, result)
	default:
		return fmt.Errorf("unknown format: %s", format)
	}
}

func renderPolicySimTable(w io.Writer, r *pipeline.PolicySimResult) error {
	fmt.Fprintln(w)

	// ── AGGRESSIVE SUMMARY (first thing the user reads) ──
	fmt.Fprintf(w, "  %s\n", Header("POLICY SIMULATION REPORT"))
	fmt.Fprintf(w, "  %s\n", Dim("========================"))
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s\n", buildAggressiveSummary(r))
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  Scope:   %s %s (%d resources)\n", r.Scope, Bold("\""+r.ScopeName+"\""), r.TotalResources)
	fmt.Fprintf(w, "  Action:  Set %s\n", Bold(formatSetTags(r.SetTags)))
	fmt.Fprintf(w, "  Impact:  %s\n", colorImpact(r.Impact))
	fmt.Fprintln(w)

	// ── POLICY CHANGES (Before vs After) ──
	if len(r.Comparisons) > 0 {
		fmt.Fprintf(w, "  %s\n", Section("POLICY CHANGES (Before vs After)"))
		fmt.Fprintf(w, "  %s\n", Dim("================================"))
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n",
			Dim("RESOURCE"), Dim("FIELD"), Dim("BEFORE"), Dim("AFTER"), Dim("SOURCE"))
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n",
			Dim("--------"), Dim("-----"), Dim("------"), Dim("-----"), Dim("------"))
		for _, comp := range r.Comparisons {
			label := comp.ResourceName
			if label == "" {
				label = shortID(comp.ResourceID)
			}
			src := Dim("direct")
			if comp.Inherited {
				src = Yellow("inherited")
			}
			for i, ch := range comp.Changes {
				name := ""
				if i == 0 {
					name = Bold(label)
				}
				fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n",
					name, ch.Field, Dim(policyVal(ch.Before)), Bold(policyVal(ch.After)), src)
			}
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		fmt.Fprintln(w)
	}

	// ── DECISION DIFF ──
	if len(r.DecisionDiffs) > 0 {
		fmt.Fprintf(w, "  %s\n", Section("DECISION DIFF"))
		fmt.Fprintf(w, "  %s\n", Dim("============="))
		for _, d := range r.DecisionDiffs {
			name := d.ResourceName
			if name == "" {
				name = shortID(d.ResourceID)
			}
			fmt.Fprintf(w, "  %s %s:\n", Dim(d.ResourceType), Bold(name))
			fmt.Fprintf(w, "    Before:  %s (risk=%s, confidence=%.2f, auto=%v, %s/mo)\n",
				Dim(d.BeforeAction), RiskColor(d.BeforeRisk.String()), d.BeforeConf, d.BeforeAuto, Dim(fmt.Sprintf("$%.2f", d.BeforeSaving)))
			fmt.Fprintf(w, "    After:   %s (risk=%s, confidence=%.2f, auto=%v, %s/mo)\n",
				Bold(d.AfterAction), RiskColor(d.AfterRisk.String()), d.AfterConf, d.AfterAuto, Savings(d.AfterSaving))

			if len(d.Explanation) > 0 {
				fmt.Fprintf(w, "    %s\n", Yellow("WHY:"))
				for _, reason := range d.Explanation {
					fmt.Fprintf(w, "      %s %s\n", Yellow("-"), reason)
				}
			}
			fmt.Fprintln(w)
		}
	}

	// ── SIMULATION RESULTS (before/after table) ──
	fmt.Fprintf(w, "  %s\n", Section("SIMULATION RESULTS"))
	fmt.Fprintf(w, "  %s\n", Dim("------------------"))
	tw2 := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw2, "  \t%s\t%s\t%s\n", Dim("BEFORE"), Dim("AFTER"), Dim("DELTA"))
	fmt.Fprintf(tw2, "  \t%s\t%s\t%s\n", Dim("------"), Dim("-----"), Dim("-----"))
	fmt.Fprintf(tw2, "  Recommendations\t%d\t%d\t%s\n",
		r.BeforeRecsCount, r.AfterRecsCount, colorIntDelta(r.AfterRecsCount-r.BeforeRecsCount))
	fmt.Fprintf(tw2, "  Sim. Applied\t%d\t%d\t%s\n",
		len(r.BeforeSim.Applied), len(r.AfterSim.Applied),
		colorIntDelta(len(r.AfterSim.Applied)-len(r.BeforeSim.Applied)))
	fmt.Fprintf(tw2, "  Sim. Skipped\t%d\t%d\t%s\n",
		len(r.BeforeSim.Skipped), len(r.AfterSim.Skipped),
		colorIntDelta(len(r.AfterSim.Skipped)-len(r.BeforeSim.Skipped)))
	fmt.Fprintf(tw2, "  Monthly Savings\t$%.2f\t$%.2f\t%s\n",
		r.BeforeSavings, r.AfterSavings, SavingsDelta(r.SavingsDelta))
	forecastDelta := r.AfterForecast.TotalSaving - r.BeforeForecast.TotalSaving
	fmt.Fprintf(tw2, "  12-mo Forecast\t$%.2f\t$%.2f\t%s\n",
		r.BeforeForecast.TotalSaving, r.AfterForecast.TotalSaving, SavingsDelta(forecastDelta))
	if err := tw2.Flush(); err != nil {
		return err
	}
	fmt.Fprintln(w)

	// ── IMPACT SCORE ──
	fmt.Fprintf(w, "  %s\n", Section("IMPACT SCORE"))
	fmt.Fprintf(w, "  %s\n", Dim("============"))
	fmt.Fprintf(w, "  Score:  %s\n", colorImpact(r.Impact))
	fmt.Fprintln(w, "  Based on:")
	fmt.Fprintf(w, "    - Resources affected:   %s of %d (%.0f%%)\n",
		Bold(fmt.Sprintf("%d", r.AffectedCount)), r.TotalResources, safePct(r.AffectedCount, r.TotalResources))
	fmt.Fprintf(w, "    - Decision changes:     %s\n", Bold(fmt.Sprintf("%d", len(r.DecisionDiffs))))
	fmt.Fprintf(w, "    - Financial delta:      %s/mo (%.0f%%)\n",
		SavingsDelta(r.SavingsDelta), abs(savingsDeltaPct(r.BeforeSavings, r.SavingsDelta)))
	if r.ExternalAffected > 0 {
		fmt.Fprintf(w, "    - %s\n",
			BoldRed(fmt.Sprintf("EXTERNAL DEPS: %d resource(s) >>> AUTO-ESCALATED TO CRITICAL", r.ExternalAffected)))
	}
	fmt.Fprintln(w)

	// ── RECOMMENDATION ──
	fmt.Fprintf(w, "  %s\n", Section("RECOMMENDATION"))
	fmt.Fprintf(w, "  %s\n", Dim("=============="))
	fmt.Fprintf(w, "  %s\n", colorSafety(r.Impact, r.Safety))
	fmt.Fprintln(w)

	// ── CONVERSION CTA ──
	fmt.Fprintf(w, "%s\n", CloudCTA(r.AfterSavings))
	fmt.Fprintln(w)

	return nil
}

func buildAggressiveSummary(r *pipeline.PolicySimResult) string {
	newlyProtected := 0
	newlyActionable := 0
	for _, d := range r.DecisionDiffs {
		if d.BeforeAuto && !d.AfterAuto {
			newlyProtected++
		}
		if !d.BeforeAuto && d.AfterAuto {
			newlyActionable++
		}
	}

	delta := r.SavingsDelta
	switch {
	case r.ExternalAffected > 0:
		return BoldRed(fmt.Sprintf(
			"This change touches %d resource(s) with external dependencies. "+
				"Impact auto-escalated to CRITICAL. Savings delta: %s/mo.",
			r.ExternalAffected, SavingsDelta(delta)))
	case delta < 0 && newlyProtected > 0:
		return BoldYellow(fmt.Sprintf(
			"This change will reduce savings by $%.2f/mo but increase safety for %d critical resource(s).",
			-delta, newlyProtected))
	case delta > 0 && newlyActionable > 0:
		return BoldGreen(fmt.Sprintf(
			"This change unlocks $%.2f/mo in new savings by relaxing protections on %d resource(s).",
			delta, newlyActionable))
	case r.AffectedCount == 0:
		return Dim("No resources would be affected by this policy change.")
	case delta == 0:
		return Bold(fmt.Sprintf(
			"This change affects %d resource(s) with no financial impact. Policy posture will shift.",
			r.AffectedCount))
	default:
		return Bold(fmt.Sprintf(
			"This change affects %d resource(s). Savings delta: %s/mo.",
			r.AffectedCount, SavingsDelta(delta)))
	}
}

func colorImpact(level engine.ImpactLevel) string {
	switch level {
	case engine.ImpactCritical:
		return BadgeCritical("Major blast radius")
	case engine.ImpactHigh:
		return BadgeHigh("Significant change")
	case engine.ImpactMedium:
		return BadgeMedium("Moderate change")
	default:
		return BadgeLow("Minimal impact")
	}
}

func colorIntDelta(n int) string {
	s := fmt.Sprintf("%+d", n)
	switch {
	case n > 0:
		return Green(s)
	case n < 0:
		return Red(s)
	default:
		return Dim(s)
	}
}

func colorSafety(impact engine.ImpactLevel, text string) string {
	switch impact {
	case engine.ImpactCritical:
		return BoldRed(text)
	case engine.ImpactHigh:
		return Red(text)
	case engine.ImpactMedium:
		return Yellow(text)
	default:
		return Green(text)
	}
}

func formatSetTags(tags map[string]string) string {
	parts := make([]string, 0, len(tags))
	for k, v := range tags {
		parts = append(parts, fmt.Sprintf("%s=%s", k, v))
	}
	return strings.Join(parts, ", ")
}

func policyVal(v string) string {
	if v == "" {
		return "(default)"
	}
	return v
}

func shortID(id string) string {
	parts := strings.Split(id, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return id
}

func deltaSign(d float64) string {
	if d >= 0 {
		return "+"
	}
	return "-"
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func intSign(n int) string {
	if n >= 0 {
		return "+"
	}
	return "-"
}

func intAbs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func safePct(part, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}

func savingsDeltaPct(before, delta float64) float64 {
	if before == 0 {
		return 0
	}
	return delta / before * 100
}

// ExportPolicySim writes the policy simulation report to the given writer
// in Markdown or HTML format, suitable for sharing via Slack or email.
func ExportPolicySim(w io.Writer, result *pipeline.PolicySimResult, path string) error {
	if strings.HasSuffix(path, ".html") {
		return exportPolicySimHTML(w, result)
	}
	return exportPolicySimMarkdown(w, result)
}

func exportPolicySimMarkdown(w io.Writer, r *pipeline.PolicySimResult) error {
	fmt.Fprintf(w, "# Policy Simulation Report\n\n")
	fmt.Fprintf(w, "**Scope:** %s `%s` (%d resources)  \n", r.Scope, r.ScopeName, r.TotalResources)
	fmt.Fprintf(w, "**Action:** Set %s  \n", formatSetTags(r.SetTags))
	fmt.Fprintf(w, "**Impact:** `%s`  \n\n", r.Impact)

	if len(r.Comparisons) > 0 {
		fmt.Fprintf(w, "## Policy Changes (Before vs After)\n\n")
		fmt.Fprintln(w, "| Resource | Field | Before | After | Source |")
		fmt.Fprintln(w, "|----------|-------|--------|-------|--------|")
		for _, c := range r.Comparisons {
			label := c.ResourceName
			if label == "" {
				label = shortID(c.ResourceID)
			}
			src := "direct"
			if c.Inherited {
				src = "inherited"
			}
			for i, ch := range c.Changes {
				name := ""
				if i == 0 {
					name = label
				}
				fmt.Fprintf(w, "| %s | %s | %s | %s | %s |\n",
					name, ch.Field, policyVal(ch.Before), policyVal(ch.After), src)
			}
		}
		fmt.Fprintln(w)
	}

	if len(r.DecisionDiffs) > 0 {
		fmt.Fprintf(w, "## Decision Diff\n\n")
		for _, d := range r.DecisionDiffs {
			name := d.ResourceName
			if name == "" {
				name = shortID(d.ResourceID)
			}
			fmt.Fprintf(w, "### %s `%s`\n\n", d.ResourceType, name)
			fmt.Fprintf(w, "- **Before:** %s (risk=%s, confidence=%.2f, auto=%v, $%.2f/mo)\n",
				d.BeforeAction, d.BeforeRisk, d.BeforeConf, d.BeforeAuto, d.BeforeSaving)
			fmt.Fprintf(w, "- **After:** %s (risk=%s, confidence=%.2f, auto=%v, $%.2f/mo)\n",
				d.AfterAction, d.AfterRisk, d.AfterConf, d.AfterAuto, d.AfterSaving)
			if len(d.Explanation) > 0 {
				fmt.Fprintln(w, "\n**Why:**")
				for _, reason := range d.Explanation {
					fmt.Fprintf(w, "- %s\n", reason)
				}
			}
			fmt.Fprintln(w)
		}
	}

	fmt.Fprintf(w, "## Simulation Results\n\n")
	fmt.Fprintln(w, "| Metric | Before | After | Delta |")
	fmt.Fprintln(w, "|--------|--------|-------|-------|")
	fmt.Fprintf(w, "| Recommendations | %d | %d | %s%d |\n",
		r.BeforeRecsCount, r.AfterRecsCount, intSign(r.AfterRecsCount-r.BeforeRecsCount), intAbs(r.AfterRecsCount-r.BeforeRecsCount))
	fmt.Fprintf(w, "| Sim. Applied | %d | %d | %s%d |\n",
		len(r.BeforeSim.Applied), len(r.AfterSim.Applied),
		intSign(len(r.AfterSim.Applied)-len(r.BeforeSim.Applied)),
		intAbs(len(r.AfterSim.Applied)-len(r.BeforeSim.Applied)))
	fmt.Fprintf(w, "| Monthly Savings | $%.2f | $%.2f | %s$%.2f |\n",
		r.BeforeSavings, r.AfterSavings, deltaSign(r.SavingsDelta), abs(r.SavingsDelta))
	fmt.Fprintf(w, "| 12-mo Forecast | $%.2f | $%.2f | %s$%.2f |\n",
		r.BeforeForecast.TotalSaving, r.AfterForecast.TotalSaving,
		deltaSign(r.AfterForecast.TotalSaving-r.BeforeForecast.TotalSaving),
		abs(r.AfterForecast.TotalSaving-r.BeforeForecast.TotalSaving))
	fmt.Fprintln(w)

	fmt.Fprintf(w, "## Impact Score\n\n")
	fmt.Fprintf(w, "**%s**\n\n", r.Impact)
	fmt.Fprintf(w, "- Resources affected: %d of %d (%.0f%%)\n",
		r.AffectedCount, r.TotalResources, safePct(r.AffectedCount, r.TotalResources))
	fmt.Fprintf(w, "- Decision changes: %d\n", len(r.DecisionDiffs))
	fmt.Fprintf(w, "- Financial delta: %s$%.2f/mo\n", deltaSign(r.SavingsDelta), abs(r.SavingsDelta))
	if r.ExternalAffected > 0 {
		fmt.Fprintf(w, "- **EXTERNAL DEPS: %d resource(s) — AUTO-ESCALATED TO CRITICAL**\n", r.ExternalAffected)
	}
	fmt.Fprintln(w)

	fmt.Fprintf(w, "## Recommendation\n\n")
	fmt.Fprintf(w, "> %s\n\n", r.Safety)

	fmt.Fprintf(w, "---\n\n*%s*\n\n", r.Summary)
	fmt.Fprintf(w, "---\n\n")
	fmt.Fprintf(w, "> **Found savings?** Let SafeCut Cloud track and automate them for you.\n>\n")
	fmt.Fprintf(w, "> [Join the Waitlist](%s)\n", WaitlistURL)
	return nil
}

func exportPolicySimHTML(w io.Writer, r *pipeline.PolicySimResult) error {
	impactClass := "low"
	switch r.Impact {
	case engine.ImpactCritical:
		impactClass = "critical"
	case engine.ImpactHigh:
		impactClass = "high"
	case engine.ImpactMedium:
		impactClass = "medium"
	}

	fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>SafeCut Policy Simulation Report</title>
<style>
  body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; max-width: 900px; margin: 2rem auto; padding: 0 1rem; color: #1a1a2e; background: #fafafa; }
  h1 { border-bottom: 3px solid #16213e; padding-bottom: 0.5rem; }
  h2 { color: #16213e; margin-top: 2rem; }
  table { border-collapse: collapse; width: 100%%; margin: 1rem 0; }
  th, td { border: 1px solid #ddd; padding: 8px 12px; text-align: left; }
  th { background: #16213e; color: white; }
  tr:nth-child(even) { background: #f2f2f2; }
  .badge { display: inline-block; padding: 4px 12px; border-radius: 4px; font-weight: bold; color: white; }
  .badge.low { background: #2ecc71; }
  .badge.medium { background: #f39c12; }
  .badge.high { background: #e74c3c; }
  .badge.critical { background: #8e44ad; animation: pulse 1.5s infinite; }
  @keyframes pulse { 0%%,100%% { opacity: 1; } 50%% { opacity: 0.7; } }
  .recommendation { background: #eef; border-left: 4px solid #16213e; padding: 1rem; margin: 1rem 0; }
  .decision-card { background: white; border: 1px solid #ddd; border-radius: 8px; padding: 1rem; margin: 0.5rem 0; }
  .before { color: #888; }
  .after { color: #16213e; font-weight: bold; }
  .why { font-size: 0.9em; color: #555; margin-top: 0.5rem; }
  .external-warning { background: #ffeaa7; border-left: 4px solid #e74c3c; padding: 0.75rem; margin: 0.5rem 0; font-weight: bold; }
  .summary { font-style: italic; color: #555; margin-top: 2rem; padding-top: 1rem; border-top: 1px solid #ddd; }
</style>
</head>
<body>
<h1>Policy Simulation Report</h1>
<p><strong>Scope:</strong> %s <code>%s</code> (%d resources)</p>
<p><strong>Action:</strong> Set %s</p>
<p><strong>Impact:</strong> <span class="badge %s">%s</span></p>
`, r.Scope, r.ScopeName, r.TotalResources, formatSetTags(r.SetTags), impactClass, r.Impact)

	if r.ExternalAffected > 0 {
		fmt.Fprintf(w, `<div class="external-warning">%d resource(s) with external dependencies affected — Impact auto-escalated to CRITICAL</div>
`, r.ExternalAffected)
	}

	if len(r.Comparisons) > 0 {
		fmt.Fprintln(w, `<h2>Policy Changes</h2>
<table><tr><th>Resource</th><th>Field</th><th>Before</th><th>After</th><th>Source</th></tr>`)
		for _, c := range r.Comparisons {
			label := c.ResourceName
			if label == "" {
				label = shortID(c.ResourceID)
			}
			src := "direct"
			if c.Inherited {
				src = "inherited"
			}
			for i, ch := range c.Changes {
				name := ""
				if i == 0 {
					name = label
				}
				fmt.Fprintf(w, "<tr><td>%s</td><td>%s</td><td>%s</td><td><strong>%s</strong></td><td>%s</td></tr>\n",
					name, ch.Field, policyVal(ch.Before), policyVal(ch.After), src)
			}
		}
		fmt.Fprintln(w, "</table>")
	}

	if len(r.DecisionDiffs) > 0 {
		fmt.Fprintln(w, `<h2>Decision Diff</h2>`)
		for _, d := range r.DecisionDiffs {
			name := d.ResourceName
			if name == "" {
				name = shortID(d.ResourceID)
			}
			fmt.Fprintf(w, `<div class="decision-card">
<strong>%s</strong> <code>%s</code>
<div class="before">Before: %s (risk=%s, confidence=%.2f, auto=%v, $%.2f/mo)</div>
<div class="after">After: %s (risk=%s, confidence=%.2f, auto=%v, $%.2f/mo)</div>
`, d.ResourceType, name,
				d.BeforeAction, d.BeforeRisk, d.BeforeConf, d.BeforeAuto, d.BeforeSaving,
				d.AfterAction, d.AfterRisk, d.AfterConf, d.AfterAuto, d.AfterSaving)
			if len(d.Explanation) > 0 {
				fmt.Fprintln(w, `<div class="why"><strong>Why:</strong><ul>`)
				for _, reason := range d.Explanation {
					fmt.Fprintf(w, "<li>%s</li>\n", reason)
				}
				fmt.Fprintln(w, "</ul></div>")
			}
			fmt.Fprintln(w, "</div>")
		}
	}

	fmt.Fprintln(w, `<h2>Simulation Results</h2>
<table><tr><th>Metric</th><th>Before</th><th>After</th><th>Delta</th></tr>`)
	fmt.Fprintf(w, "<tr><td>Recommendations</td><td>%d</td><td>%d</td><td>%s%d</td></tr>\n",
		r.BeforeRecsCount, r.AfterRecsCount, intSign(r.AfterRecsCount-r.BeforeRecsCount), intAbs(r.AfterRecsCount-r.BeforeRecsCount))
	fmt.Fprintf(w, "<tr><td>Sim. Applied</td><td>%d</td><td>%d</td><td>%s%d</td></tr>\n",
		len(r.BeforeSim.Applied), len(r.AfterSim.Applied),
		intSign(len(r.AfterSim.Applied)-len(r.BeforeSim.Applied)),
		intAbs(len(r.AfterSim.Applied)-len(r.BeforeSim.Applied)))
	fmt.Fprintf(w, "<tr><td>Monthly Savings</td><td>$%.2f</td><td>$%.2f</td><td>%s$%.2f</td></tr>\n",
		r.BeforeSavings, r.AfterSavings, deltaSign(r.SavingsDelta), abs(r.SavingsDelta))
	fmt.Fprintf(w, "<tr><td>12-mo Forecast</td><td>$%.2f</td><td>$%.2f</td><td>%s$%.2f</td></tr>\n",
		r.BeforeForecast.TotalSaving, r.AfterForecast.TotalSaving,
		deltaSign(r.AfterForecast.TotalSaving-r.BeforeForecast.TotalSaving),
		abs(r.AfterForecast.TotalSaving-r.BeforeForecast.TotalSaving))
	fmt.Fprintln(w, "</table>")

	fmt.Fprintf(w, `<h2>Impact Score: <span class="badge %s">%s</span></h2>
<ul>
<li>Resources affected: %d of %d (%.0f%%)</li>
<li>Decision changes: %d</li>
<li>Financial delta: %s$%.2f/mo</li>
`, impactClass, r.Impact,
		r.AffectedCount, r.TotalResources, safePct(r.AffectedCount, r.TotalResources),
		len(r.DecisionDiffs),
		deltaSign(r.SavingsDelta), abs(r.SavingsDelta))
	if r.ExternalAffected > 0 {
		fmt.Fprintf(w, "<li><strong>EXTERNAL DEPS: %d resource(s) — AUTO-ESCALATED TO CRITICAL</strong></li>\n", r.ExternalAffected)
	}
	fmt.Fprintln(w, "</ul>")

	fmt.Fprintf(w, `<div class="recommendation"><strong>Recommendation:</strong> %s</div>
<p class="summary">%s</p>

<footer style="margin-top:3rem;padding:1.5rem;background:#16213e;border-radius:8px;text-align:center;">
  <p style="color:#a0a0c0;font-size:0.95em;margin:0 0 0.75rem 0;">
    Found savings? Let SafeCut Cloud track and automate them for you.
  </p>
  <a href="%s"
     style="display:inline-block;padding:10px 28px;background:#3498db;color:white;text-decoration:none;border-radius:6px;font-weight:bold;font-size:1em;">
    Join the Waitlist
  </a>
  <p style="color:#555;font-size:0.75em;margin:0.75rem 0 0 0;">
    Generated by SafeCut CLI — Policy Simulation Engine
  </p>
</footer>
</body>
</html>
`, r.Safety, r.Summary, WaitlistURL)

	return nil
}
