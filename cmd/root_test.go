package cmd

import (
	"testing"
)

// TestRootHelpListsAllCommands guards against the regression where a new
// subcommand gets wired via init() but forgets to be AddCommand()'d, or
// is silently hidden, or is renamed without updating docs. Every
// user-facing command that ships in v1.0 must be reachable from root.
//
// We inspect the Commands() slice directly instead of running
// `rootCmd.Execute(--help)` because cobra's help flow calls os.Exit on
// some platforms and pollutes global state (flag parsing) in ways that
// break subsequent tests in the same package.
func TestRootHelpListsAllCommands(t *testing.T) {
	want := []string{
		"quick-scan",
		"policy",
		"apply",
		"upgrade",
		"doctor",
		"history",
		"config",
		"partner",
	}

	present := map[string]bool{}
	for _, sub := range rootCmd.Commands() {
		present[sub.Name()] = true
	}
	for _, cmd := range want {
		if !present[cmd] {
			t.Errorf("root is missing command %q; have %v", cmd, present)
		}
	}
}

// TestPolicySubcommandsRegistered ensures the policy parent command wires
// both `simulate` and `lint`. The lint command in particular is what
// users are pointed at from the README for CI gating. We walk the
// Commands() slice directly rather than invoking `--help` because cobra
// routes parent-command help through a template that doesn't render when
// the parent is detached from its root.
func TestPolicySubcommandsRegistered(t *testing.T) {
	present := map[string]bool{}
	for _, sub := range policyCmd.Commands() {
		present[sub.Name()] = true
	}
	for _, want := range []string{"simulate", "lint"} {
		if !present[want] {
			t.Errorf("policy is missing subcommand %q; have %v", want, present)
		}
	}
}
