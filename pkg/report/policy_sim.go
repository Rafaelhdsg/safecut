package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/Rafaelhdsg/inframind-cli/internal/engine"
	"github.com/Rafaelhdsg/inframind-cli/internal/pipeline"
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
	// ── HEADER ──
	fmt.Fprintln(w, "POLICY SIMULATION REPORT")
	fmt.Fprintln(w, "========================")
	fmt.Fprintf(w, "Scope:   %s \"%s\" (%d resources)\n", r.Scope, r.ScopeName, r.TotalResources)
	fmt.Fprintf(w, "Action:  Set %s\n", formatSetTags(r.SetTags))
	fmt.Fprintf(w, "Impact:  %s\n", formatImpactBadge(r.Impact))
	fmt.Fprintln(w)

	// ── MELHORIA 1: DELTA EXPLÍCITO (ANTES vs DEPOIS) ──
	if len(r.Comparisons) > 0 {
		fmt.Fprintln(w, "POLICY CHANGES (Before vs After)")
		fmt.Fprintln(w, "================================")
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "RESOURCE\tFIELD\tBEFORE\tAFTER\tSOURCE")
		fmt.Fprintln(tw, "--------\t-----\t------\t-----\t------")
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
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
					name, ch.Field, policyVal(ch.Before), policyVal(ch.After), src)
			}
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		fmt.Fprintln(w)
	}

	// ── MELHORIA 2: DECISION DIFF (ANTES vs DEPOIS POR RECURSO) ──
	if len(r.DecisionDiffs) > 0 {
		fmt.Fprintln(w, "DECISION DIFF")
		fmt.Fprintln(w, "=============")
		for _, d := range r.DecisionDiffs {
			name := d.ResourceName
			if name == "" {
				name = shortID(d.ResourceID)
			}
			fmt.Fprintf(w, "%s %s:\n", d.ResourceType, name)
			fmt.Fprintf(w, "  Before:  %s (risk=%s, confidence=%.2f, auto=%v, $%.2f/mo)\n",
				d.BeforeAction, d.BeforeRisk, d.BeforeConf, d.BeforeAuto, d.BeforeSaving)
			fmt.Fprintf(w, "  After:   %s (risk=%s, confidence=%.2f, auto=%v, $%.2f/mo)\n",
				d.AfterAction, d.AfterRisk, d.AfterConf, d.AfterAuto, d.AfterSaving)

			// ── MELHORIA 3: POLICY EXPLANATION (WHY) ──
			if len(d.Explanation) > 0 {
				fmt.Fprintln(w, "  WHY:")
				for _, reason := range d.Explanation {
					fmt.Fprintf(w, "    - %s\n", reason)
				}
			}
			fmt.Fprintln(w)
		}
	}

	// ── IMPACT SUMMARY ──
	fmt.Fprintln(w, "IMPACT SUMMARY")
	fmt.Fprintln(w, "--------------")
	fmt.Fprintf(w, "  Resources affected:     %d of %d (%.0f%%)\n",
		r.AffectedCount, r.TotalResources, safePct(r.AffectedCount, r.TotalResources))
	fmt.Fprintf(w, "  Via inheritance:        %d\n", r.InheritedCount)
	fmt.Fprintf(w, "  Decision changes:       %d\n", len(r.DecisionDiffs))
	if r.DriftsResolved > 0 {
		fmt.Fprintf(w, "  Drifts resolved:        %d\n", r.DriftsResolved)
	}
	if r.DriftsCreated > 0 {
		fmt.Fprintf(w, "  Drifts created:         %d\n", r.DriftsCreated)
	}
	fmt.Fprintln(w)

	// ── SIMULATION & FORECAST (full pipeline) ──
	fmt.Fprintln(w, "SIMULATION RESULTS")
	fmt.Fprintln(w, "------------------")
	tw2 := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw2, "\tBEFORE\tAFTER\tDELTA")
	fmt.Fprintln(tw2, "\t------\t-----\t-----")
	fmt.Fprintf(tw2, "Recommendations\t%d\t%d\t%s%d\n",
		r.BeforeRecsCount, r.AfterRecsCount, intSign(r.AfterRecsCount-r.BeforeRecsCount), intAbs(r.AfterRecsCount-r.BeforeRecsCount))
	fmt.Fprintf(tw2, "Sim. Applied\t%d\t%d\t%s%d\n",
		len(r.BeforeSim.Applied), len(r.AfterSim.Applied),
		intSign(len(r.AfterSim.Applied)-len(r.BeforeSim.Applied)),
		intAbs(len(r.AfterSim.Applied)-len(r.BeforeSim.Applied)))
	fmt.Fprintf(tw2, "Sim. Skipped\t%d\t%d\t%s%d\n",
		len(r.BeforeSim.Skipped), len(r.AfterSim.Skipped),
		intSign(len(r.AfterSim.Skipped)-len(r.BeforeSim.Skipped)),
		intAbs(len(r.AfterSim.Skipped)-len(r.BeforeSim.Skipped)))
	fmt.Fprintf(tw2, "Monthly Savings\t$%.2f\t$%.2f\t%s$%.2f\n",
		r.BeforeSavings, r.AfterSavings, deltaSign(r.SavingsDelta), abs(r.SavingsDelta))
	fmt.Fprintf(tw2, "12-mo Forecast\t$%.2f\t$%.2f\t%s$%.2f\n",
		r.BeforeForecast.TotalSaving, r.AfterForecast.TotalSaving,
		deltaSign(r.AfterForecast.TotalSaving-r.BeforeForecast.TotalSaving),
		abs(r.AfterForecast.TotalSaving-r.BeforeForecast.TotalSaving))
	if err := tw2.Flush(); err != nil {
		return err
	}
	fmt.Fprintln(w)

	// ── MELHORIA 4: IMPACT SCORE ──
	fmt.Fprintln(w, "IMPACT SCORE")
	fmt.Fprintln(w, "============")
	fmt.Fprintf(w, "  Score:  %s\n", formatImpactBadge(r.Impact))
	fmt.Fprintln(w, "  Based on:")
	fmt.Fprintf(w, "    - Resources affected:   %d of %d (%.0f%%)\n",
		r.AffectedCount, r.TotalResources, safePct(r.AffectedCount, r.TotalResources))
	fmt.Fprintf(w, "    - Decision changes:     %d\n", len(r.DecisionDiffs))
	fmt.Fprintf(w, "    - Financial delta:      %s$%.2f/mo (%.0f%%)\n",
		deltaSign(r.SavingsDelta), abs(r.SavingsDelta), abs(savingsDeltaPct(r.BeforeSavings, r.SavingsDelta)))
	if r.ExternalAffected > 0 {
		fmt.Fprintf(w, "    - EXTERNAL DEPS:        %d resource(s) with external dependencies  >>> AUTO-ESCALATED TO CRITICAL\n", r.ExternalAffected)
	}
	fmt.Fprintln(w)

	// ── MELHORIA 5: SAFE RECOMMENDATION ──
	fmt.Fprintln(w, "RECOMMENDATION")
	fmt.Fprintln(w, "==============")
	fmt.Fprintf(w, "  %s\n", r.Safety)
	fmt.Fprintln(w)

	// ── SUMMARY ──
	fmt.Fprintf(w, "\"%s\"\n", r.Summary)

	return nil
}

func formatSetTags(tags map[string]string) string {
	parts := make([]string, 0, len(tags))
	for k, v := range tags {
		parts = append(parts, fmt.Sprintf("%s=%s", k, v))
	}
	return strings.Join(parts, ", ")
}

func formatImpactBadge(level engine.ImpactLevel) string {
	switch level {
	case engine.ImpactCritical:
		return "[CRITICAL] !!!"
	case engine.ImpactHigh:
		return "[HIGH] !!"
	case engine.ImpactMedium:
		return "[MEDIUM]"
	default:
		return "[LOW]"
	}
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
		fmt.Fprintln(w, "## Policy Changes (Before vs After)\n")
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
		fmt.Fprintln(w, "## Decision Diff\n")
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

	fmt.Fprintln(w, "## Simulation Results\n")
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

	fmt.Fprintln(w, "## Impact Score\n")
	fmt.Fprintf(w, "**%s**\n\n", r.Impact)
	fmt.Fprintf(w, "- Resources affected: %d of %d (%.0f%%)\n",
		r.AffectedCount, r.TotalResources, safePct(r.AffectedCount, r.TotalResources))
	fmt.Fprintf(w, "- Decision changes: %d\n", len(r.DecisionDiffs))
	fmt.Fprintf(w, "- Financial delta: %s$%.2f/mo\n", deltaSign(r.SavingsDelta), abs(r.SavingsDelta))
	if r.ExternalAffected > 0 {
		fmt.Fprintf(w, "- **EXTERNAL DEPS: %d resource(s) — AUTO-ESCALATED TO CRITICAL**\n", r.ExternalAffected)
	}
	fmt.Fprintln(w)

	fmt.Fprintln(w, "## Recommendation\n")
	fmt.Fprintf(w, "> %s\n\n", r.Safety)

	fmt.Fprintf(w, "---\n\n*%s*\n", r.Summary)
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
<title>InfraMind Policy Simulation Report</title>
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
<hr>
<p style="font-size:0.8em;color:#999;">Generated by InfraMind CLI — Policy Simulation Engine</p>
</body>
</html>
`, r.Safety, r.Summary)

	return nil
}
