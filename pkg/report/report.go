package report

// Format specifies the output format for reports.
type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
	FormatASCII Format = "ascii"
)
