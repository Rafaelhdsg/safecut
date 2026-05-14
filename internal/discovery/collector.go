package discovery

import (
	"context"
	"fmt"
	"strings"

	"github.com/Rafaelhdsg/safecut/internal/providers"
)

// ProgressFunc is called by the collector to report progress.
// stage: current high-level phase, detail: optional sub-message,
// current/total: numeric progress (0,0 if indeterminate).
type ProgressFunc func(stage, detail string, current, total int)

// Collector orchestrates resource discovery across one or more cloud providers.
// It collects resources, raw metrics, and tag hierarchy data for policy inheritance.
type Collector struct {
	provider   providers.Provider
	OnProgress ProgressFunc
	// ResourceGroupFilter, if non-empty, keeps only resources in this resource group
	// (case-insensitive match on ResourceGroup). Applied after listing, before metrics.
	ResourceGroupFilter string
	// SkipMetrics disables the metrics collection phase. Used by `policy lint`
	// and other cheap, metadata-only passes.
	SkipMetrics bool
}

func NewCollector(p providers.Provider) *Collector {
	return &Collector{provider: p}
}

func (c *Collector) progress(stage, detail string, current, total int) {
	if c.OnProgress != nil {
		c.OnProgress(stage, detail, current, total)
	}
}

// SafetyStatus tracks whether the locks / snapshots check for a given
// scope (resource ID or resource group name) actually succeeded.
// Rules consult LocksStatus before auto-executing: an "unknown" lock
// probe must never silently become "no lock", because that would lead
// to "auto-apply safe" recommendations on resources the operator
// explicitly locked.
type SafetyStatus string

const (
	// SafetyUnchecked means the collector never ran the probe
	// (unsupported provider, Lint mode, etc).
	SafetyUnchecked SafetyStatus = ""
	// SafetyKnown means the probe ran and the list we stored is
	// authoritative (possibly empty).
	SafetyKnown SafetyStatus = "known"
	// SafetyDenied means the probe ran and failed (permission,
	// throttle, network). Treat as "we can't rule out a lock"
	// and refuse auto-execution.
	SafetyDenied SafetyStatus = "denied"
)

// Snapshot represents the raw state captured during a discovery pass.
type Snapshot struct {
	Resources         []providers.Resource
	Metrics           map[string]*ResourceMetrics
	SubscriptionTags  map[string]string
	SubscriptionName  string
	ResourceGroupTags map[string]map[string]string        // RG name → tags
	Locks             map[string][]providers.LockInfo     // resource/RG ID → locks
	Snapshots         map[string][]providers.SnapshotInfo // RG name → disk snapshots

	// LocksStatus records whether the per-resource / per-RG lock probe
	// succeeded. Keys are resource IDs and RG names, mirroring Locks.
	// Rules must refuse auto-execute when LocksStatus == SafetyDenied
	// (or missing entirely, when the provider claims to support locks).
	LocksStatus map[string]SafetyStatus

	// SnapshotsStatus records whether the per-RG snapshot probe
	// succeeded. Keys are RG names.
	SnapshotsStatus map[string]SafetyStatus

	// SafetyProviderSupported is true when the provider implements
	// SafetyProvider and locks/snapshots probing was actually
	// attempted. Rules use this to distinguish "we don't know
	// because the provider can't tell us" from "we didn't check".
	SafetyProviderSupported bool

	// TagsWarnings records human-readable messages when subscription
	// or resource-group tag lookups failed. Policy resolution still
	// works (it falls back to resource-level tags), but the user
	// deserves a visible warning because governance expectations may
	// silently shift.
	TagsWarnings []string
}

func filterResourcesByResourceGroup(resources []providers.Resource, rg string) []providers.Resource {
	if rg == "" {
		return resources
	}
	var out []providers.Resource
	for _, r := range resources {
		if strings.EqualFold(r.ResourceGroup, rg) {
			out = append(out, r)
		}
	}
	return out
}

func shortType(resourceType string) string {
	parts := strings.Split(resourceType, "/")
	if len(parts) > 1 {
		return parts[len(parts)-1]
	}
	return resourceType
}

// Collect performs a full discovery pass: lists resources, gathers raw metrics,
// and collects parent-level tags if the provider supports hierarchy.
func (c *Collector) Collect(ctx context.Context, resourceTypes []string) (*Snapshot, error) {
	snap := &Snapshot{
		Metrics:           make(map[string]*ResourceMetrics),
		ResourceGroupTags: make(map[string]map[string]string),
		Locks:             make(map[string][]providers.LockInfo),
		Snapshots:         make(map[string][]providers.SnapshotInfo),
		LocksStatus:       make(map[string]SafetyStatus),
		SnapshotsStatus:   make(map[string]SafetyStatus),
	}

	c.progress("Discovering resources...", "", 0, 0)

	for i, rt := range resourceTypes {
		c.progress("Discovering resources...",
			fmt.Sprintf("Querying %s (%d/%d)", shortType(rt), i+1, len(resourceTypes)),
			i+1, len(resourceTypes))

		resources, err := c.provider.ListResources(ctx, rt)
		if err != nil {
			return nil, fmt.Errorf("collecting %s: %w", rt, err)
		}
		snap.Resources = append(snap.Resources, resources...)
	}

	if c.ResourceGroupFilter != "" {
		snap.Resources = filterResourcesByResourceGroup(snap.Resources, c.ResourceGroupFilter)
	}

	if !c.SkipMetrics {
		total := len(snap.Resources)
		c.progress("Collecting metrics...",
			fmt.Sprintf("Analyzing %d resources", total), 0, total)

		for i := range snap.Resources {
			name := snap.Resources[i].Name
			c.progress("Collecting metrics...",
				fmt.Sprintf("[%d/%d] %s", i+1, total, name),
				i+1, total)

			m := collectMetrics(ctx, c.provider, &snap.Resources[i])
			snap.Metrics[snap.Resources[i].ID] = m
		}
	}

	c.progress("Resolving policy hierarchy...", "", 0, 0)
	c.collectHierarchyTags(ctx, snap)

	if !c.SkipMetrics {
		c.progress("Checking safety constraints...", "", 0, 0)
		c.collectSafetyData(ctx, snap)
	}

	return snap, nil
}

// collectSafetyData fetches locks and snapshots if the provider supports it.
// Every probe result is recorded in LocksStatus / SnapshotsStatus so the
// decision engine can distinguish "no lock" (SafetyKnown, empty list)
// from "we failed to read locks" (SafetyDenied). The old behaviour
// silently merged both cases and produced "auto-apply safe"
// recommendations on locked resources whenever the service principal
// lacked Microsoft.Authorization/locks/read.
func (c *Collector) collectSafetyData(ctx context.Context, snap *Snapshot) {
	sp, ok := c.provider.(providers.SafetyProvider)
	if !ok {
		return
	}
	snap.SafetyProviderSupported = true

	seenRGs := make(map[string]bool)
	for _, r := range snap.Resources {
		// Per-resource locks
		c.progress("Checking safety constraints...",
			fmt.Sprintf("Locks on %s", r.Name), 0, 0)
		resLocks, err := sp.ListResourceLocks(ctx, r.ID)
		if err != nil {
			snap.LocksStatus[r.ID] = SafetyDenied
		} else {
			snap.LocksStatus[r.ID] = SafetyKnown
			if len(resLocks) > 0 {
				snap.Locks[r.ID] = resLocks
			}
		}

		// Per-resource group locks and snapshots (deduplicated by RG)
		if r.ResourceGroup != "" && !seenRGs[r.ResourceGroup] {
			seenRGs[r.ResourceGroup] = true

			c.progress("Checking safety constraints...",
				fmt.Sprintf("Locks on RG %s", r.ResourceGroup), 0, 0)
			rgLocks, err := sp.ListResourceGroupLocks(ctx, r.ResourceGroup)
			if err != nil {
				snap.LocksStatus[r.ResourceGroup] = SafetyDenied
			} else {
				snap.LocksStatus[r.ResourceGroup] = SafetyKnown
				if len(rgLocks) > 0 {
					snap.Locks[r.ResourceGroup] = rgLocks
				}
			}

			hasDisk := false
			for _, res := range snap.Resources {
				if res.ResourceGroup == r.ResourceGroup && strings.Contains(strings.ToLower(res.Type), "disks") {
					hasDisk = true
					break
				}
			}
			if hasDisk {
				c.progress("Checking safety constraints...",
					fmt.Sprintf("Snapshots in RG %s", r.ResourceGroup), 0, 0)
				snaps, err := sp.ListDiskSnapshots(ctx, r.ResourceGroup)
				if err != nil {
					snap.SnapshotsStatus[r.ResourceGroup] = SafetyDenied
				} else {
					snap.SnapshotsStatus[r.ResourceGroup] = SafetyKnown
					snap.Snapshots[r.ResourceGroup] = snaps
				}
			}
		}
	}
}

// collectHierarchyTags fetches subscription and RG-level tags if the
// provider implements HierarchyProvider. Errors are non-fatal — the
// pipeline falls back to resource-level tags only — but each failure
// is recorded in Snapshot.TagsWarnings so the output layer can warn
// the operator that governance policy resolution may be incomplete.
func (c *Collector) collectHierarchyTags(ctx context.Context, snap *Snapshot) {
	hp, ok := c.provider.(providers.HierarchyProvider)
	if !ok {
		return
	}

	if subTags, err := hp.GetSubscriptionTags(ctx); err == nil {
		snap.SubscriptionTags = subTags
	} else {
		snap.TagsWarnings = append(snap.TagsWarnings,
			fmt.Sprintf("subscription tag lookup failed: %v", err))
	}

	if name, err := hp.GetSubscriptionName(ctx); err == nil && name != "" {
		snap.SubscriptionName = name
	}

	seenRGs := make(map[string]bool)
	for _, r := range snap.Resources {
		if r.ResourceGroup == "" || seenRGs[r.ResourceGroup] {
			continue
		}
		seenRGs[r.ResourceGroup] = true
		if rgTags, err := hp.GetResourceGroupTags(ctx, r.ResourceGroup); err == nil {
			snap.ResourceGroupTags[r.ResourceGroup] = rgTags
		} else {
			snap.TagsWarnings = append(snap.TagsWarnings,
				fmt.Sprintf("resource group %q tag lookup failed: %v", r.ResourceGroup, err))
		}
	}
}
