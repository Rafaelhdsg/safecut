package telemetry

import (
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/Rafaelhdsg/safecut/internal/version"
	"github.com/posthog/posthog-go"
)

const posthogEndpoint = "https://us.i.posthog.com"

func posthogAPIKey() string {
	if k := os.Getenv("SAFECUT_POSTHOG_KEY"); k != "" {
		return k
	}
	return embeddedPostHogKey
}

var (
	client  posthog.Client
	config  *Config
	enabled bool
)

// IsFirstRun checks whether the config file exists.
// Returns true if this is the very first execution.
func IsFirstRun() bool {
	cfg, _ := LoadConfig()
	return cfg == nil
}

// EnsureConfig creates the default config file if it doesn't exist yet.
// Returns the config (existing or newly created) and whether it was just created.
func EnsureConfig() (*Config, bool, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, false, err
	}
	if cfg != nil {
		return cfg, false, nil
	}
	cfg = DefaultConfig()
	if err := SaveConfig(cfg); err != nil {
		return nil, false, err
	}
	return cfg, true, nil
}

// Init initializes telemetry. Must be called once from root command.
// Safe to call even if telemetry ends up disabled — it becomes a no-op.
// Assumes EnsureConfig has already been called by the first-run interceptor.
func Init() {
	if envDisabled() {
		enabled = false
		return
	}

	cfg, err := LoadConfig()
	if err != nil {
		enabled = false
		return
	}
	if cfg == nil {
		enabled = false
		return
	}

	config = cfg

	if !cfg.TelemetryEnabled {
		enabled = false
		return
	}

	apiKey := posthogAPIKey()
	if apiKey == "" {
		enabled = false
		return
	}

	c, err := posthog.NewWithConfig(apiKey, posthog.Config{
		Endpoint: posthogEndpoint,
	})
	if err != nil {
		enabled = false
		return
	}

	client = c
	enabled = true
}

// Track sends an analytics event. Non-blocking, fire-and-forget.
// No-op if telemetry is disabled or initialization failed.
func Track(event string, properties map[string]interface{}) {
	if !enabled || client == nil || config == nil {
		return
	}

	props := posthog.NewProperties()
	props.Set("cli_version", version.Version)
	props.Set("os", runtime.GOOS)
	props.Set("arch", runtime.GOARCH)
	for k, v := range properties {
		props.Set(k, v)
	}

	_ = client.Enqueue(posthog.Capture{
		DistinctId: config.InstallationID,
		Event:      event,
		Properties: props,
	})
}

const flushTimeout = 2 * time.Second

// Close flushes pending events with a hard timeout.
// If the network is slow or unreachable, the CLI exits after 2s — never blocks the user.
func Close() {
	if client == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		_ = client.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(flushTimeout):
	}
}

// Disable persistently disables telemetry.
func Disable() error {
	cfg, err := LoadConfig()
	if err != nil || cfg == nil {
		cfg = DefaultConfig()
	}
	cfg.TelemetryEnabled = false
	return SaveConfig(cfg)
}

// Enable persistently enables telemetry.
func Enable() error {
	cfg, err := LoadConfig()
	if err != nil || cfg == nil {
		cfg = DefaultConfig()
	}
	cfg.TelemetryEnabled = true
	return SaveConfig(cfg)
}

// CTAShown records that a conversion CTA was rendered to the user.
// tier is typically "solo", "team", "enterprise", "partner", or "pricing".
// context is the emitting surface (e.g. "quick_scan_roi", "apply_footer").
// monthlySavings is bucketed (not raw) to keep telemetry low-cardinality and
// privacy-friendly; 0 is fine when savings are unknown.
func CTAShown(tier, context string, monthlySavings float64) {
	Track("cta_shown", map[string]interface{}{
		"tier":                   tier,
		"context":                context,
		"monthly_savings_bucket": savingsBucket(monthlySavings),
	})
}

// CTAClicked records that the user invoked a specific CTA (e.g. ran
// `safecut upgrade --start-trial solo`). action is one of
// "start_trial", "book_demo", "partner_apply", "open_pricing".
func CTAClicked(tier, action, context string) {
	Track("cta_clicked", map[string]interface{}{
		"tier":    tier,
		"action":  action,
		"context": context,
	})
}

// SavingsBucket is the exported wrapper around savingsBucket so other
// packages can bucket savings values consistently before sending them
// through Track. Use this rather than sending raw dollar amounts.
func SavingsBucket(monthlySavings float64) string { return savingsBucket(monthlySavings) }

// savingsBucket reduces continuous savings into stable buckets so the
// distribution is useful in PostHog without leaking exact dollar amounts.
func savingsBucket(monthlySavings float64) string {
	switch {
	case monthlySavings <= 0:
		return "zero"
	case monthlySavings < 50:
		return "0-50"
	case monthlySavings < 200:
		return "50-200"
	case monthlySavings < 1000:
		return "200-1k"
	case monthlySavings < 5000:
		return "1k-5k"
	default:
		return "5k+"
	}
}

func envDisabled() bool {
	for _, key := range []string{"SAFECUT_NO_TELEMETRY", "DO_NOT_TRACK"} {
		if v := os.Getenv(key); v != "" && v != "0" && strings.ToLower(v) != "false" {
			return true
		}
	}
	for _, key := range []string{"CI", "GITHUB_ACTIONS", "GITLAB_CI", "CIRCLECI", "JENKINS_URL", "CODEBUILD_BUILD_ID"} {
		if os.Getenv(key) != "" {
			return true
		}
	}
	return false
}
