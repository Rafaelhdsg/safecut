package discovery

import (
	"context"
	"fmt"

	"github.com/Rafaelhdsg/inframind-cli/internal/providers"
)

// Collector orchestrates resource discovery across one or more cloud providers.
// It collects resources, raw metrics, and tag hierarchy data for policy inheritance.
type Collector struct {
	provider providers.Provider
}

func NewCollector(p providers.Provider) *Collector {
	return &Collector{provider: p}
}

// Snapshot represents the raw state captured during a discovery pass.
type Snapshot struct {
	Resources         []providers.Resource
	Metrics           map[string]*ResourceMetrics
	SubscriptionTags  map[string]string
	SubscriptionName  string
	ResourceGroupTags map[string]map[string]string // RG name → tags
}

// Collect performs a full discovery pass: lists resources, gathers raw metrics,
// and collects parent-level tags if the provider supports hierarchy.
func (c *Collector) Collect(ctx context.Context, resourceTypes []string) (*Snapshot, error) {
	snap := &Snapshot{
		Metrics:           make(map[string]*ResourceMetrics),
		ResourceGroupTags: make(map[string]map[string]string),
	}

	for _, rt := range resourceTypes {
		resources, err := c.provider.ListResources(ctx, rt)
		if err != nil {
			return nil, fmt.Errorf("collecting %s: %w", rt, err)
		}
		snap.Resources = append(snap.Resources, resources...)
	}

	for i := range snap.Resources {
		m, err := CollectMetrics(ctx, &snap.Resources[i])
		if err != nil {
			continue
		}
		snap.Metrics[snap.Resources[i].ID] = m
	}

	c.collectHierarchyTags(ctx, snap)

	return snap, nil
}

// collectHierarchyTags fetches subscription and RG-level tags if the
// provider implements HierarchyProvider. Errors are non-fatal — the
// pipeline falls back to resource-level tags only.
func (c *Collector) collectHierarchyTags(ctx context.Context, snap *Snapshot) {
	hp, ok := c.provider.(providers.HierarchyProvider)
	if !ok {
		return
	}

	if subTags, err := hp.GetSubscriptionTags(ctx); err == nil {
		snap.SubscriptionTags = subTags
	}

	seenRGs := make(map[string]bool)
	for _, r := range snap.Resources {
		if r.ResourceGroup == "" || seenRGs[r.ResourceGroup] {
			continue
		}
		seenRGs[r.ResourceGroup] = true
		if rgTags, err := hp.GetResourceGroupTags(ctx, r.ResourceGroup); err == nil {
			snap.ResourceGroupTags[r.ResourceGroup] = rgTags
		}
	}
}
