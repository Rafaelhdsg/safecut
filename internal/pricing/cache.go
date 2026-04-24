package pricing

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const cacheTTL = 7 * 24 * time.Hour

// cacheVersion is bumped whenever the cache schema or the set of fetched
// services changes. Old caches with a different version are automatically
// invalidated on load.
const cacheVersion = 2

// cacheEntry wraps the pricing data stored on disk.
type cacheEntry struct {
	Version   int                    `json:"version"`
	FetchedAt time.Time              `json:"fetched_at"`
	Region    string                 `json:"region"`
	Records   map[string]PriceRecord `json:"records"`
}

// CacheDir returns the on-disk cache directory for pricing records.
// Exported so CLI health checks (inframind doctor) can inspect cache state.
func CacheDir() string { return cacheDir() }

// CacheTTL is the validity window used by pricing cache reads.
const CacheTTL = cacheTTL

func cacheDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "inframind", "pricing")
	default:
		cfg := os.Getenv("XDG_CONFIG_HOME")
		if cfg == "" {
			cfg = filepath.Join(home, ".config")
		}
		return filepath.Join(cfg, "inframind", "pricing")
	}
}

func cachePath(cloud, region string) string {
	dir := cacheDir()
	if dir == "" {
		return ""
	}
	safe := strings.ReplaceAll(strings.ToLower(region), "/", "-")
	return filepath.Join(dir, cloud+"-"+safe+".json")
}

// cacheIsValid returns true if the cache file exists and is younger than cacheTTL.
func cacheIsValid(cloud, region string) bool {
	path := cachePath(cloud, region)
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) < cacheTTL
}

// cacheLoad reads cached pricing records from disk. Returns nil if the
// cache version does not match the current version (forces re-fetch).
func cacheLoad(cloud, region string) (map[string]PriceRecord, error) {
	path := cachePath(cloud, region)
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, err
	}
	if entry.Version != cacheVersion {
		_ = os.Remove(path)
		return nil, nil
	}
	return entry.Records, nil
}

// cacheSave writes pricing records to disk.
func cacheSave(cloud, region string, records map[string]PriceRecord) error {
	path := cachePath(cloud, region)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	entry := cacheEntry{
		Version:   cacheVersion,
		FetchedAt: time.Now(),
		Region:    region,
		Records:   records,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
