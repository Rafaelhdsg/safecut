package store

import "time"

// Record represents a single saved analysis run for historical tracking.
type Record struct {
	ID              string
	Timestamp       time.Time
	Provider        string
	TotalResources  int
	Recommendations int
	MonthlySaving   float64
	AnnualSaving    float64
}

// Store defines the interface for persisting analysis results.
// Initial implementation can use local SQLite; swap for Postgres later.
type Store interface {
	Save(record Record) error
	List() ([]Record, error)
	Get(id string) (*Record, error)
}
