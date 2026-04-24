package cmd

import (
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/Rafaelhdsg/inframind-cli/internal/version"
	"github.com/spf13/cobra"
)

// versionCmd prints the InfraMind CLI version and build metadata.
// It is intentionally explicit (and independent from cobra's --version
// flag) so release scripts, docker images and support playbooks have
// a stable `inframind version` to call without parsing a flag banner.
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print InfraMind CLI version, commit, and build date",
	Long: `Print the compiled-in version, commit short hash, build date and runtime.

These values are injected at build time via -ldflags by GoReleaser. When
InfraMind is built locally (e.g. ` + "`go build ./cmd/inframind`" + `)
they fall back to "dev" / "none" / "unknown".

Flags:
  --short    Print just the version number (suitable for scripts)
  -o json    Print version metadata as a JSON object`,
	RunE: runVersion,
}

func init() {
	versionCmd.Flags().Bool("short", false, "Print only the version number")
	rootCmd.AddCommand(versionCmd)
}

func runVersion(cmd *cobra.Command, _ []string) error {
	short, _ := cmd.Flags().GetBool("short")
	output, _ := cmd.Flags().GetString("output")
	w := cmd.OutOrStdout()

	if short {
		fmt.Fprintln(w, version.Version)
		return nil
	}

	if output == "json" {
		payload := map[string]string{
			"version":    version.Version,
			"commit":     version.Commit,
			"build_date": version.Date,
			"go_version": runtime.Version(),
			"os":         runtime.GOOS,
			"arch":       runtime.GOARCH,
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}

	fmt.Fprintf(w, "inframind %s\n", version.Version)
	fmt.Fprintf(w, "  commit:     %s\n", version.Commit)
	fmt.Fprintf(w, "  build date: %s\n", version.Date)
	fmt.Fprintf(w, "  go:         %s (%s/%s)\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	return nil
}
