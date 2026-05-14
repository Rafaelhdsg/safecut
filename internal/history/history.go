package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"
)

// ScanRecord is a lightweight summary of a scan, persisted between runs.
type ScanRecord struct {
	Timestamp      time.Time      `json:"timestamp"`
	SubscriptionID string         `json:"subscription_id"`
	TotalResources int            `json:"total_resources"`
	IdleDetected   int            `json:"idle_detected"`
	MonthlySaving  float64        `json:"monthly_saving"`
	YearlySaving   float64        `json:"yearly_saving"`
	RecCount       int            `json:"recommendation_count"`
	RiskCount      int            `json:"risk_count"`
	Actions        map[string]int `json:"actions"`
}

// Delta describes the change between two scans.
type Delta struct {
	Previous       *ScanRecord
	DaysSince      int
	ResourceDelta  int
	IdleDelta      int
	SavingDelta    float64
	RecDelta       int
	NewActions     map[string]int
	RemovedActions map[string]int
}

func historyDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "safecut", "history")
	default:
		cfg := os.Getenv("XDG_CONFIG_HOME")
		if cfg == "" {
			cfg = filepath.Join(home, ".config")
		}
		return filepath.Join(cfg, "safecut", "history")
	}
}

// Save persists a scan record to the history directory.
func Save(record ScanRecord) error {
	dir := historyDir()
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	filename := record.Timestamp.Format("2006-01-02T150405") + ".json"
	path := filepath.Join(dir, filename)

	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

const localHistoryDays = 7

// LoadPrevious returns the most recent scan record for the given subscription
// within the local history window (7 days), or nil if none exists.
func LoadPrevious(subscriptionID string) *ScanRecord {
	dir := historyDir()
	if dir == "" {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() > entries[j].Name()
	})

	cutoff := time.Now().AddDate(0, 0, -localHistoryDays)

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}

		var record ScanRecord
		if err := json.Unmarshal(data, &record); err != nil {
			continue
		}

		if record.SubscriptionID == subscriptionID {
			if record.Timestamp.Before(cutoff) {
				return nil
			}
			return &record
		}
	}
	return nil
}

// ComputeDelta compares a current scan record with a previous one.
func ComputeDelta(current, previous ScanRecord) Delta {
	d := Delta{
		Previous:       &previous,
		DaysSince:      int(current.Timestamp.Sub(previous.Timestamp).Hours() / 24),
		ResourceDelta:  current.TotalResources - previous.TotalResources,
		IdleDelta:      current.IdleDetected - previous.IdleDetected,
		SavingDelta:    current.MonthlySaving - previous.MonthlySaving,
		RecDelta:       current.RecCount - previous.RecCount,
		NewActions:     make(map[string]int),
		RemovedActions: make(map[string]int),
	}
	if d.DaysSince < 0 {
		d.DaysSince = 0
	}

	for action, count := range current.Actions {
		if prev, ok := previous.Actions[action]; ok {
			if count > prev {
				d.NewActions[action] = count - prev
			} else if count < prev {
				d.RemovedActions[action] = prev - count
			}
		} else {
			d.NewActions[action] = count
		}
	}
	for action, count := range previous.Actions {
		if _, ok := current.Actions[action]; !ok {
			d.RemovedActions[action] = count
		}
	}

	return d
}

// Cleanup removes history files older than maxDays. Returns the first
// error encountered (directory read or file removal). Missing directory
// is not an error.
func Cleanup(maxDays int) error {
	dir := historyDir()
	if dir == "" {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read history dir %q: %w", dir, err)
	}

	cutoff := time.Now().AddDate(0, 0, -maxDays)
	var firstErr error
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("stat %q: %w", entry.Name(), err)
			}
			continue
		}
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("remove %q: %w", entry.Name(), err)
			}
		}
	}
	return firstErr
}
