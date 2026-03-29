package report

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/Rafaelhdsg/inframind-cli/internal/engine"
)

// Format specifies the output format for reports.
type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
	FormatASCII Format = "ascii"
)

// FullReport contains everything needed to render a complete output.
type FullReport struct {
	Recommendations []engine.Recommendation    `json:"recommendations"`
	Protected       []engine.ProtectedResource `json:"protected,omitempty"`
	Observed        []engine.ObservedResource  `json:"observed,omitempty"`
	Policies        map[string]*engine.ResolvedPolicy `json:"policies,omitempty"`
	Drifts          []engine.PolicyDrift        `json:"drifts,omitempty"`
}

// Render writes the full report to w in the specified format.
func Render(w io.Writer, report FullReport, format Format) error {
	switch format {
	case FormatJSON:
		return renderJSON(w, report)
	case FormatTable, FormatASCII:
		return renderTable(w, report)
	default:
		return fmt.Errorf("unknown format: %s", format)
	}
}

func renderJSON(w io.Writer, report FullReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func renderTable(w io.Writer, report FullReport) error {
	if err := renderRecommendations(w, report.Recommendations); err != nil {
		return err
	}
	if err := renderSignalBreakdown(w, report.Recommendations); err != nil {
		return err
	}
	if err := renderPolicySources(w, report.Recommendations, report.Policies); err != nil {
		return err
	}
	if err := renderDrifts(w, report.Drifts); err != nil {
		return err
	}
	if err := renderObserved(w, report.Observed); err != nil {
		return err
	}
	return renderProtected(w, report.Protected)
}

func renderRecommendations(w io.Writer, recs []engine.Recommendation) error {
	if len(recs) == 0 {
		fmt.Fprintln(w, "No recommendations found.")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "RESOURCE\tACTION\tRISK\tSAVING/MO\tIDLE\tCONF\tAUTO\tREASON")
	fmt.Fprintln(tw, "--------\t------\t----\t---------\t----\t----\t----\t------")
	for _, r := range recs {
		idle := "-"
		conf := "-"
		if r.Analysis != nil {
			idle = fmt.Sprintf("%.2f", r.Analysis.Score)
			conf = fmt.Sprintf("%.2f", r.Analysis.Confidence)
		}
		auto := "yes"
		if !r.AutoExecute {
			auto = "NO"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t$%.2f\t%s\t%s\t%s\t%s\n",
			r.ResourceID, r.Action, r.Risk, r.MonthlySave, idle, conf, auto, r.Reason)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	hasNotes := false
	for _, r := range recs {
		if r.PolicyNote != "" {
			if !hasNotes {
				fmt.Fprintln(w)
				fmt.Fprintln(w, "POLICY NOTES")
				fmt.Fprintln(w, "============")
				hasNotes = true
			}
			fmt.Fprintf(w, "  %s: %s\n", r.ResourceID, r.PolicyNote)
		}
	}
	return nil
}

func renderSignalBreakdown(w io.Writer, recs []engine.Recommendation) error {
	hasAnalysis := false
	for _, r := range recs {
		if r.Analysis != nil && len(r.Analysis.Signals) > 0 {
			hasAnalysis = true
			break
		}
	}
	if !hasAnalysis {
		return nil
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "SIGNAL BREAKDOWN")
	fmt.Fprintln(w, "================")
	for _, r := range recs {
		if r.Analysis == nil || len(r.Analysis.Signals) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n%s (idle=%.2f, confidence=%.2f):\n", r.ResourceID, r.Analysis.Score, r.Analysis.Confidence)
		for _, s := range r.Analysis.Signals {
			fmt.Fprintf(w, "  %-12s %s\n", s.Name+":", s.Status)
		}
	}
	return nil
}

// renderPolicySources shows where each resource's policy came from.
func renderPolicySources(w io.Writer, recs []engine.Recommendation, policies map[string]*engine.ResolvedPolicy) error {
	if len(policies) == 0 {
		return nil
	}

	hasInherited := false
	for _, r := range recs {
		pol := policies[r.ResourceID]
		if pol == nil {
			continue
		}
		if pol.ModeOrigin.Source != engine.SourceDefault && pol.ModeOrigin.Source != engine.SourceResource {
			hasInherited = true
			break
		}
		if pol.CriticalityOrigin.Source != engine.SourceDefault && pol.CriticalityOrigin.Source != engine.SourceResource {
			hasInherited = true
			break
		}
		if pol.ExternalOrigin.Source != engine.SourceDefault && pol.ExternalOrigin.Source != engine.SourceResource {
			hasInherited = true
			break
		}
	}
	if !hasInherited {
		return nil
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "POLICY SOURCES")
	fmt.Fprintln(w, "==============")
	for _, r := range recs {
		pol := policies[r.ResourceID]
		if pol == nil {
			continue
		}
		fmt.Fprintf(w, "\n%s", r.ResourceID)
		if pol.Override {
			fmt.Fprint(w, " [override — inheritance blocked]")
		}
		fmt.Fprintln(w, ":")
		fmt.Fprintf(w, "  %-14s %s  ← %s\n", "mode:", pol.Mode, pol.ModeOrigin)
		fmt.Fprintf(w, "  %-14s %s  ← %s\n", "criticality:", pol.Criticality, pol.CriticalityOrigin)

		extVal := "false"
		if pol.ExternalDeps {
			extVal = "true"
		}
		fmt.Fprintf(w, "  %-14s %s  ← %s\n", "external:", extVal, pol.ExternalOrigin)
	}
	return nil
}

func renderDrifts(w io.Writer, drifts []engine.PolicyDrift) error {
	if len(drifts) == 0 {
		return nil
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "POLICY DRIFT WARNINGS")
	fmt.Fprintln(w, "=====================")
	for _, d := range drifts {
		fmt.Fprintf(w, "  %s: resource=%s, but %s \"%s\"=%s\n",
			d.Field, d.ResourceValue, d.ParentSource, d.ParentName, d.ParentValue)
	}
	return nil
}

func renderObserved(w io.Writer, observed []engine.ObservedResource) error {
	if len(observed) == 0 {
		return nil
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "OBSERVED RESOURCES (analyzed but no action taken)")
	fmt.Fprintln(w, "=================================================")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "RESOURCE\tNAME\tTYPE\tIDLE\tCONF")
	fmt.Fprintln(tw, "--------\t----\t----\t----\t----")
	for _, o := range observed {
		idle := "-"
		conf := "-"
		if o.Analysis != nil {
			idle = fmt.Sprintf("%.2f", o.Analysis.Score)
			conf = fmt.Sprintf("%.2f", o.Analysis.Confidence)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			o.ResourceID, o.ResourceName, o.ResourceType, idle, conf)
	}
	return tw.Flush()
}

func renderProtected(w io.Writer, protected []engine.ProtectedResource) error {
	if len(protected) == 0 {
		return nil
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "IGNORED RESOURCES (safe-locked, excluded from all analysis)")
	fmt.Fprintln(w, "============================================================")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "RESOURCE\tNAME\tTYPE\tMODE SOURCE")
	fmt.Fprintln(tw, "--------\t----\t----\t-----------")
	for _, p := range protected {
		source := "resource"
		if p.Policy != nil {
			source = p.Policy.ModeOrigin.String()
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			p.ResourceID, p.ResourceName, p.ResourceType, source)
	}
	return tw.Flush()
}
