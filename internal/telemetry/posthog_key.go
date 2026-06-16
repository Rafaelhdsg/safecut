package telemetry

// embeddedPostHogKey is injected at link time for release binaries (GoReleaser).
// Dev builds and `go install` leave it empty unless SAFECUT_POSTHOG_KEY is set.
var embeddedPostHogKey string
