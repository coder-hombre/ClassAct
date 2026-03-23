package storage

import (
	"classact/internal/model"
	"context"
)

// LawsuitFilter specifies optional filtering criteria for listing lawsuits.
type LawsuitFilter struct {
	Companies []string // empty = no filter (return all)
}

// Repository defines the data access interface for the storage layer.
type Repository interface {
	// UpsertLawsuits inserts or updates lawsuit records in a batch transaction.
	// Returns the count of newly inserted and updated records.
	UpsertLawsuits(ctx context.Context, records []model.LawsuitRecord) (inserted int, updated int, err error)

	// ListLawsuits returns lawsuit records, optionally filtered by company names (case-insensitive).
	ListLawsuits(ctx context.Context, filter LawsuitFilter) ([]model.LawsuitRecord, error)

	// MarkApplied marks a lawsuit as applied. Idempotent: only sets applied_at if not already set.
	MarkApplied(ctx context.Context, lawsuitID string) error

	// GetAppliedStatus returns whether a lawsuit has been marked as applied.
	GetAppliedStatus(ctx context.Context, lawsuitID string) (bool, error)

	// GetCompanyFilter returns all company names in the filter list.
	GetCompanyFilter(ctx context.Context) ([]string, error)

	// AddCompany adds a company name to the filter list (normalized to lowercase).
	AddCompany(ctx context.Context, name string) error

	// RemoveCompany removes a company name from the filter list (normalized to lowercase).
	RemoveCompany(ctx context.Context, name string) error

	// SaveRunResult persists a scraping run result.
	SaveRunResult(ctx context.Context, result model.RunResult) error

	// GetLatestRunResult returns the most recent scraping run result, or nil if none exist.
	GetLatestRunResult(ctx context.Context) (*model.RunResult, error)

	// Close closes the underlying database connection.
	Close() error
}
