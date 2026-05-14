package azure

import (
	"context"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/monitor/azquery"
	"github.com/Rafaelhdsg/safecut/internal/engine"
	"github.com/Rafaelhdsg/safecut/internal/providers"
)

const metricsLookbackDays = 14

// queryMonitorMetrics fetches Azure Monitor metrics for the given resource.
// For VMs: CPU%, Network In/Out, Disk Write Bytes (14-day average).
// Disks/IPs don't have Monitor metrics — their idle state is property-based
// and handled in the discovery layer, so the caller must not assume zero
// metrics means "idle" for those types.
//
// Contract: returns (nil, err) on *any* Azure Monitor failure (auth,
// throttle, network, unmarshal). Swallowing the error and returning
// zero-valued metrics would let the idle-detection layer flag a
// production VM as fully idle simply because the caller's service
// principal lacks Monitor Reader, or because the VM has no diagnostic
// settings. That is the single worst kind of false positive we can
// produce, so we propagate the error and let the caller degrade
// gracefully (MetricsStatus = denied|unknown).
func queryMonitorMetrics(ctx context.Context, cred azcore.TokenCredential, resourceID string, resourceType string) (*providers.ResourceMetrics, error) {
	lower := strings.ToLower(resourceType)

	switch {
	case strings.Contains(lower, "virtualmachines"):
		return queryVMMetrics(ctx, cred, resourceID)
	default:
		return &providers.ResourceMetrics{}, nil
	}
}

func queryVMMetrics(ctx context.Context, cred azcore.TokenCredential, resourceID string) (*providers.ResourceMetrics, error) {
	client, err := azquery.NewMetricsClient(cred, nil)
	if err != nil {
		return nil, err
	}

	end := time.Now().UTC()
	start := end.AddDate(0, 0, -metricsLookbackDays)
	timespan := azquery.TimeInterval(start.Format(time.RFC3339) + "/" + end.Format(time.RFC3339))

	metricNames := "Percentage CPU,Network In Total,Network Out Total,Disk Read Bytes,Disk Write Bytes"
	avg := azquery.AggregationTypeAverage
	total := azquery.AggregationTypeTotal

	resp, err := client.QueryResource(ctx, resourceID, &azquery.MetricsClientQueryResourceOptions{
		Timespan:    &timespan,
		Interval:    to.Ptr("P1D"),
		MetricNames: to.Ptr(metricNames),
		Aggregation: []*azquery.AggregationType{&avg, &total},
	})
	if err != nil {
		// BUG #1 fix: propagate the error. Returning zero metrics here
		// used to silently produce "idle VM — auto-stop safe"
		// recommendations for any VM whose metrics we couldn't read.
		return nil, err
	}

	m := &providers.ResourceMetrics{}

	// Track data-point count per metric series. ObservedDays below
	// is the *minimum* across all four series — we only trust a
	// day's observation when every signal (CPU, net in/out, disk
	// write) has a sample for it. Using only CPU (the old behaviour)
	// silently treated a VM whose disk or network series were empty
	// as "fully observed", which caused the analyzer to compute a
	// full-weight idle score from an incomplete signal set.
	perMetric := map[string]int{
		"cpu":         0,
		"network_in":  0,
		"network_out": 0,
		"disk_write":  0,
		"disk_read":   0,
	}
	for _, metric := range resp.Value {
		if metric.Name == nil || metric.Name.Value == nil {
			continue
		}
		name := strings.ToLower(*metric.Name.Value)
		avgVal := extractAverage(metric)
		totalVal := extractTotal(metric)

		switch {
		case strings.Contains(name, "percentage cpu"):
			m.CPUAvgPercent = avgVal
			perMetric["cpu"] = countDataPoints(metric)
		case strings.Contains(name, "network in"):
			m.NetworkIn = totalVal
			perMetric["network_in"] = countDataPoints(metric)
		case strings.Contains(name, "network out"):
			m.NetworkOut = totalVal
			perMetric["network_out"] = countDataPoints(metric)
		case strings.Contains(name, "disk write"):
			m.DiskWriteBytes = totalVal
			perMetric["disk_write"] = countDataPoints(metric)
		case strings.Contains(name, "disk read"):
			m.DiskReadBytes = totalVal
			perMetric["disk_read"] = countDataPoints(metric)
		}
	}

	m.IdleDays = countIdleDays(resp.Value)
	m.ObservedDays = minObservedDays(perMetric)
	return m, nil
}

// minObservedDays returns the smallest sample count across the
// tracked series. If any series is missing entirely (e.g. Azure
// Monitor never sent a "Disk Write Bytes" meter back) the result is
// 0, which fails closed at the rules layer.
func minObservedDays(perMetric map[string]int) int {
	first := true
	best := 0
	for _, v := range perMetric {
		if first || v < best {
			best = v
			first = false
		}
	}
	if best < 0 {
		return 0
	}
	return best
}

func extractAverage(metric *azquery.Metric) float64 {
	count := 0
	sum := 0.0
	for _, ts := range metric.TimeSeries {
		for _, dp := range ts.Data {
			if dp.Average != nil {
				sum += *dp.Average
				count++
			}
		}
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

func extractTotal(metric *azquery.Metric) float64 {
	total := 0.0
	for _, ts := range metric.TimeSeries {
		for _, dp := range ts.Data {
			if dp.Total != nil {
				total += *dp.Total
			} else if dp.Average != nil {
				total += *dp.Average
			}
		}
	}
	return total
}

func countDataPoints(metric *azquery.Metric) int {
	count := 0
	for _, ts := range metric.TimeSeries {
		count += len(ts.Data)
	}
	return count
}

// countIdleDays counts how many days the CPU was below the shared
// idle threshold (engine.IdleCPUThresholdPercent). Using the same
// number the analyzer uses keeps "idle days" consistent with "idle
// signal" — otherwise the CLI could say "12/14 days idle" yet refuse
// to flag the VM as idle, which looks broken to the user.
func countIdleDays(metrics []*azquery.Metric) int {
	for _, metric := range metrics {
		if metric.Name == nil || metric.Name.Value == nil {
			continue
		}
		if !strings.Contains(strings.ToLower(*metric.Name.Value), "percentage cpu") {
			continue
		}
		idle := 0
		for _, ts := range metric.TimeSeries {
			for _, dp := range ts.Data {
				if dp.Average != nil && *dp.Average < engine.IdleCPUThresholdPercent {
					idle++
				}
			}
		}
		return idle
	}
	return 0
}
