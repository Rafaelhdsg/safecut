package cmd

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Rafaelhdsg/inframind-cli/internal/pricing"
	"github.com/Rafaelhdsg/inframind-cli/internal/telemetry"
	"github.com/Rafaelhdsg/inframind-cli/internal/version"
	"github.com/Rafaelhdsg/inframind-cli/pkg/report"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose auth, pricing cache, and environment readiness",
	Long: `Doctor runs a set of read-only health checks before a scan:

  1. CLI version and runtime
  2. Subscription ID resolution (flag / env vars)
  3. Azure credentials (DefaultAzureCredential chain)
  4. Pricing cache location and freshness
  5. Telemetry config status

Any failure is printed with a concrete fix. Nothing is written to Azure.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDoctor(cmd)
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
	doctorCmd.Flags().String("subscription", "", "Azure subscription ID (overrides AZURE_SUBSCRIPTION_ID / ARM_SUBSCRIPTION_ID)")
}

type checkResult struct {
	Name   string
	OK     bool
	Warn   bool
	Detail string
	Hint   string
}

func runDoctor(cmd *cobra.Command) error {
	w := cmd.OutOrStdout()
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s\n", report.Header("InfraMind Doctor"))
	fmt.Fprintf(w, "  %s\n", report.Dim("================"))
	fmt.Fprintln(w)

	checks := []checkResult{
		checkRuntime(),
		checkSubscription(cmd),
		checkAzureCredential(),
		checkPricingCache(),
		checkTelemetry(),
	}

	fail := 0
	warn := 0
	for _, c := range checks {
		printCheck(w, c)
		if !c.OK && !c.Warn {
			fail++
		}
		if c.Warn {
			warn++
		}
	}

	fmt.Fprintln(w)
	switch {
	case fail > 0:
		fmt.Fprintf(w, "  %s %d check(s) failed, %d warning(s). Fix the items above before running quick-scan.\n",
			report.BoldRed("✗"), fail, warn)
		return fmt.Errorf("doctor: %d failed check(s)", fail)
	case warn > 0:
		fmt.Fprintf(w, "  %s All mandatory checks passed (%d warning(s)). You can run quick-scan.\n",
			report.BoldYellow("!"), warn)
	default:
		fmt.Fprintf(w, "  %s All checks passed. Ready for quick-scan.\n", report.BoldGreen("✓"))
	}
	fmt.Fprintln(w)
	return nil
}

func printCheck(w interface {
	Write(p []byte) (n int, err error)
}, c checkResult) {
	mark := report.BoldGreen("✓")
	switch {
	case !c.OK && !c.Warn:
		mark = report.BoldRed("✗")
	case c.Warn:
		mark = report.BoldYellow("!")
	}
	fmt.Fprintf(w, "  %s %s\n", mark, report.Bold(c.Name))
	if c.Detail != "" {
		fmt.Fprintf(w, "      %s\n", report.Dim(c.Detail))
	}
	if c.Hint != "" {
		fmt.Fprintf(w, "      %s %s\n", report.Yellow("hint:"), c.Hint)
	}
}

func checkRuntime() checkResult {
	return checkResult{
		Name:   "Runtime",
		OK:     true,
		Detail: fmt.Sprintf("inframind %s (%s/%s, go %s)", version.Full(), runtime.GOOS, runtime.GOARCH, runtime.Version()),
	}
}

func checkSubscription(cmd *cobra.Command) checkResult {
	flagVal, _ := cmd.Flags().GetString("subscription")
	sub, err := resolveSubscriptionID(flagVal)
	if err != nil {
		return checkResult{
			Name:   "Subscription ID",
			OK:     false,
			Detail: "no subscription resolved from --subscription or env vars",
			Hint:   "export AZURE_SUBSCRIPTION_ID=<id> or pass --subscription <id>",
		}
	}
	short := sub
	if len(short) > 12 {
		short = sub[:8] + "..."
	}
	return checkResult{
		Name:   "Subscription ID",
		OK:     true,
		Detail: fmt.Sprintf("resolved to %s", short),
	}
}

func checkAzureCredential() checkResult {
	_, err := azidentity.NewDefaultAzureCredential(&azidentity.DefaultAzureCredentialOptions{
		ClientOptions: policy.ClientOptions{},
	})
	if err != nil {
		return checkResult{
			Name:   "Azure credential",
			OK:     false,
			Detail: err.Error(),
			Hint:   "run 'az login' or set AZURE_CLIENT_ID / AZURE_TENANT_ID / AZURE_CLIENT_SECRET",
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return checkResult{Name: "Azure credential", OK: false, Detail: err.Error()}
	}
	_, tokenErr := cred.GetToken(ctx, policy.TokenRequestOptions{
		Scopes: []string{"https://management.azure.com/.default"},
	})
	if tokenErr != nil {
		return checkResult{
			Name:   "Azure credential",
			Warn:   true,
			Detail: "credential chain loaded, but no token issued: " + tokenErr.Error(),
			Hint:   "run 'az login --tenant <tenant>' to prime the chain",
		}
	}
	return checkResult{
		Name:   "Azure credential",
		OK:     true,
		Detail: "DefaultAzureCredential issued an ARM token",
	}
}

func checkPricingCache() checkResult {
	dir := pricing.CacheDir()
	if dir == "" {
		return checkResult{
			Name:   "Pricing cache",
			Warn:   true,
			Detail: "could not resolve user home; cache disabled",
			Hint:   "set HOME or XDG_CONFIG_HOME to enable on-disk cache",
		}
	}
	info, err := os.Stat(dir)
	if err != nil {
		return checkResult{
			Name:   "Pricing cache",
			Warn:   true,
			Detail: fmt.Sprintf("not populated yet (%s)", dir),
			Hint:   "run quick-scan once to warm the cache",
		}
	}
	if !info.IsDir() {
		return checkResult{Name: "Pricing cache", OK: false, Detail: dir + " is not a directory"}
	}

	var newest time.Time
	var count int
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".json") {
			count++
			if fi, err := d.Info(); err == nil && fi.ModTime().After(newest) {
				newest = fi.ModTime()
			}
		}
		return nil
	})
	if count == 0 {
		return checkResult{
			Name:   "Pricing cache",
			Warn:   true,
			Detail: fmt.Sprintf("no cached files in %s", dir),
			Hint:   "run quick-scan once to populate pricing cache",
		}
	}
	stale := time.Since(newest) > pricing.CacheTTL
	detail := fmt.Sprintf("%d file(s) in %s (newest %s)", count, dir, newest.Format(time.RFC3339))
	if stale {
		return checkResult{
			Name:   "Pricing cache",
			Warn:   true,
			Detail: detail + " — older than TTL, will refresh on next scan",
		}
	}
	return checkResult{Name: "Pricing cache", OK: true, Detail: detail}
}

func checkTelemetry() checkResult {
	cfg, _ := telemetry.LoadConfig()
	if cfg == nil {
		return checkResult{
			Name:   "Telemetry",
			OK:     true,
			Detail: "not initialized (will be created on first real command)",
		}
	}
	status := "enabled"
	if !cfg.TelemetryEnabled {
		status = "disabled"
	}
	return checkResult{
		Name:   "Telemetry",
		OK:     true,
		Detail: fmt.Sprintf("%s (install %s)", status, cfg.InstallationID),
	}
}
