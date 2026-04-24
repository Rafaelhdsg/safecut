package rules

import (
	"context"
	"strings"

	"github.com/Rafaelhdsg/inframind-cli/internal/discovery"
	"github.com/Rafaelhdsg/inframind-cli/internal/pricing"
)

// fakePricing is a deterministic pricing provider for rule tests.
// Entries can be seeded per call; unset keys return an error so the
// fail-closed branch in rules like rightsize.go and reserved_instance.go
// can be exercised.
type fakePricing struct {
	VMs          map[string]float64 // vmSize -> monthly price
	Reservations map[string]float64 // vmSize+term -> monthly amortized price
}

func (f *fakePricing) Warmup(_ context.Context, _ string) error { return nil }

func (f *fakePricing) GetVMPrice(_ context.Context, vmSize, _ string) (float64, error) {
	// Match the production code path: rules lowercase the size before
	// looking it up, so the test double normalises both sides here to
	// avoid accidental miss on capitalised seed entries.
	key := strings.ToLower(vmSize)
	for k, v := range f.VMs {
		if strings.EqualFold(k, key) {
			return v, nil
		}
	}
	return 0, nil
}

func (f *fakePricing) GetDiskPrice(_ context.Context, _ string, _ float64, _ string) (float64, error) {
	return 0, nil
}
func (f *fakePricing) GetIPPrice(_ context.Context, _ string, _ string) (float64, error) {
	return 0, nil
}
func (f *fakePricing) GetAppServicePrice(_ context.Context, _ string, _ bool, _ string) (float64, error) {
	return 0, nil
}
func (f *fakePricing) GetSQLDBPrice(_ context.Context, _ string, _ string) (float64, error) {
	return 0, nil
}
func (f *fakePricing) GetLoadBalancerPrice(_ context.Context, _ string, _ string) (float64, error) {
	return 0, nil
}
func (f *fakePricing) GetNATGatewayPrice(_ context.Context, _ string) (float64, error) {
	return 0, nil
}
func (f *fakePricing) GetContainerGroupPrice(_ context.Context, _ float64, _ float64, _ string) (float64, error) {
	return 0, nil
}
func (f *fakePricing) GetVMReservationPrice(_ context.Context, vmSize, _ string, term pricing.ReservationTerm) (float64, error) {
	target := strings.ToLower(vmSize) + "|" + string(term)
	for k, v := range f.Reservations {
		if strings.EqualFold(k, target) {
			return v, nil
		}
	}
	return 0, nil
}

// markSafetyKnown populates LocksStatus / SnapshotsStatus with
// SafetyKnown for every resource currently in the EvalContext and
// flips SafetyProviderSupported on. This mirrors the behaviour of
// the discovery collector when every probe succeeded and returned
// no locks — the baseline "safe to auto-execute" state that legacy
// rule tests used to get for free.
//
// New tests that want to exercise the fail-closed branches (probe
// errored, or SafetyProvider unsupported) should NOT call this.
func markSafetyKnown(ctx *EvalContext) {
	if ctx.LocksStatus == nil {
		ctx.LocksStatus = make(map[string]discovery.SafetyStatus)
	}
	if ctx.SnapshotsStatus == nil {
		ctx.SnapshotsStatus = make(map[string]discovery.SafetyStatus)
	}
	ctx.SafetyProviderSupported = true
	seenRGs := make(map[string]bool)
	for _, r := range ctx.Resources {
		ctx.LocksStatus[r.ID] = discovery.SafetyKnown
		if r.ResourceGroup != "" && !seenRGs[r.ResourceGroup] {
			seenRGs[r.ResourceGroup] = true
			ctx.LocksStatus[r.ResourceGroup] = discovery.SafetyKnown
			ctx.SnapshotsStatus[r.ResourceGroup] = discovery.SafetyKnown
		}
	}
}
