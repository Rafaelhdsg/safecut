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

// Render writes recommendations to w in the specified format.
func Render(w io.Writer, recs []engine.Recommendation, format Format) error {
	switch format {
	case FormatJSON:
		return renderJSON(w, recs)
	case FormatTable, FormatASCII:
		return renderTable(w, recs)
	default:
		return fmt.Errorf("unknown format: %s", format)
	}
}

func renderJSON(w io.Writer, recs []engine.Recommendation) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(recs)
}

func renderTable(w io.Writer, recs []engine.Recommendation) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "RESOURCE\tACTION\tRISK\tSAVING/MO\tREASON")
	fmt.Fprintln(tw, "--------\t------\t----\t---------\t------")
	for _, r := range recs {
		fmt.Fprintf(tw, "%s\t%s\t%s\t$%.2f\t%s\n",
			r.ResourceID, r.Action, r.Risk, r.MonthlySave, r.Reason)
	}
	return tw.Flush()
}
