package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

// executeRoot runs a cobra command through rootCmd so persistent flags
// (like -o/--output) behave identically to real user invocations.
// Flag state is reset between tests because cobra keeps it on the
// shared *Command object — a classic foot-gun in CLI test suites.
func executeRoot(t *testing.T, args ...string) string {
	t.Helper()
	buf := &bytes.Buffer{}
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs(args)
	t.Setenv("INFRAMIND_NO_TELEMETRY", "1")
	t.Cleanup(resetAllFlags)
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("executeRoot %v: %v\noutput: %s", args, err, buf.String())
	}
	return buf.String()
}

// resetAllFlags walks every command in the rootCmd tree and resets
// each flag back to its default value string. This mirrors what a
// fresh process invocation looks like.
func resetAllFlags() {
	reset := func(fs *pflag.FlagSet) {
		fs.VisitAll(func(f *pflag.Flag) {
			_ = f.Value.Set(f.DefValue)
			f.Changed = false
		})
	}
	reset(rootCmd.Flags())
	for _, sub := range rootCmd.Commands() {
		reset(sub.Flags())
	}
}

func TestVersionCommand_humanReadable(t *testing.T) {
	out := executeRoot(t, "version")
	for _, want := range []string{"inframind", "commit:", "build date:", "go:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("version output missing %q\nfull output: %s", want, out)
		}
	}
}

func TestVersionCommand_short(t *testing.T) {
	got := strings.TrimSpace(executeRoot(t, "version", "--short"))
	if got == "" {
		t.Fatal("version --short produced empty output")
	}
	if strings.Contains(got, " ") || strings.Contains(got, "\n") {
		t.Fatalf("version --short must be a single token, got %q", got)
	}
}

func TestVersionCommand_json(t *testing.T) {
	out := executeRoot(t, "version", "-o", "json")
	var payload map[string]string
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("version -o json not valid JSON: %v\nbody: %s", err, out)
	}
	for _, k := range []string{"version", "commit", "build_date", "go_version", "os", "arch"} {
		if _, ok := payload[k]; !ok {
			t.Errorf("version JSON missing key %q", k)
		}
	}
}
