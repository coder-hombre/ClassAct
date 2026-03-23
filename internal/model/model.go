package model

import "time"

// LawsuitRecord represents a single class action lawsuit.
type LawsuitRecord struct {
	ID          string
	Title       string
	Description string
	SourceURL   string
	Company     string
	FilingDate  *time.Time // nil if not available
	Deadline    *time.Time // nil if not available
	Status      string     // "open", "closed", "settled"
	Applied     bool
	AppliedAt   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// RunResult captures metadata about a completed scraping run.
type RunResult struct {
	ID             string
	StartTime      time.Time
	EndTime        time.Time
	TotalFound     int
	NewRecords     int
	UpdatedRecords int
	Errors         []SiteError
}

// SiteError pairs a site name with the error encountered.
type SiteError struct {
	SiteName string
	URL      string
	Err      error
}
