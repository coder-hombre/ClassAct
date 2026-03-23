package storage

import (
	"classact/internal/model"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteRepository implements Repository using SQLite.
type SQLiteRepository struct {
	db *sql.DB
}

// Compile-time check that SQLiteRepository implements Repository.
var _ Repository = (*SQLiteRepository)(nil)

// NewSQLiteRepository opens or creates the SQLite database at dbPath and runs auto-migrations.
func NewSQLiteRepository(dbPath string) (*SQLiteRepository, error) {
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	repo := &SQLiteRepository{db: db}
	if err := repo.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return repo, nil
}

func (r *SQLiteRepository) migrate() error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS lawsuits (
			id          TEXT PRIMARY KEY,
			title       TEXT NOT NULL,
			description TEXT,
			source_url  TEXT NOT NULL UNIQUE,
			company     TEXT NOT NULL,
			filing_date TEXT,
			deadline    TEXT,
			status      TEXT NOT NULL DEFAULT 'open',
			applied     INTEGER NOT NULL DEFAULT 0,
			applied_at  TEXT,
			created_at  TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_lawsuits_source_url ON lawsuits(source_url)`,
		`CREATE INDEX IF NOT EXISTS idx_lawsuits_company ON lawsuits(company)`,
		`CREATE TABLE IF NOT EXISTS company_filter (
			name       TEXT PRIMARY KEY,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE IF NOT EXISTS scrape_runs (
			id              TEXT PRIMARY KEY,
			start_time      TEXT NOT NULL,
			end_time        TEXT,
			total_found     INTEGER DEFAULT 0,
			new_records     INTEGER DEFAULT 0,
			updated_records INTEGER DEFAULT 0,
			errors          TEXT,
			created_at      TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
	}

	for _, m := range migrations {
		if _, err := r.db.Exec(m); err != nil {
			return fmt.Errorf("exec migration: %w", err)
		}
	}
	return nil
}

// Close closes the underlying database connection.
func (r *SQLiteRepository) Close() error {
	return r.db.Close()
}

// timeToStr formats a time.Time as ISO 8601 string for SQLite storage.
func timeToStr(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05Z")
}

// optionalTimeToStr formats a *time.Time as a sql.NullString.
func optionalTimeToStr(t *time.Time) sql.NullString {
	if t == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: t.UTC().Format("2006-01-02T15:04:05Z"), Valid: true}
}

// parseTime parses an ISO 8601 string into time.Time.
func parseTime(s string) (time.Time, error) {
	return time.Parse("2006-01-02T15:04:05Z", s)
}

// parseOptionalTime parses a sql.NullString into *time.Time.
func parseOptionalTime(ns sql.NullString) *time.Time {
	if !ns.Valid || ns.String == "" {
		return nil
	}
	t, err := parseTime(ns.String)
	if err != nil {
		return nil
	}
	return &t
}

// UpsertLawsuits inserts or updates lawsuit records in a batch transaction.
// On conflict (source_url), it preserves the original created_at and applied/applied_at fields.
func (r *SQLiteRepository) UpsertLawsuits(ctx context.Context, records []model.LawsuitRecord) (inserted int, updated int, err error) {
	if len(records) == 0 {
		return 0, 0, nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Check existence by source_url
	checkStmt, err := tx.PrepareContext(ctx, `SELECT id, applied, applied_at, created_at FROM lawsuits WHERE source_url = ?`)
	if err != nil {
		return 0, 0, fmt.Errorf("prepare check: %w", err)
	}
	defer checkStmt.Close()

	insertStmt, err := tx.PrepareContext(ctx,
		`INSERT INTO lawsuits (id, title, description, source_url, company, filing_date, deadline, status, applied, applied_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, 0, fmt.Errorf("prepare insert: %w", err)
	}
	defer insertStmt.Close()

	updateStmt, err := tx.PrepareContext(ctx,
		`UPDATE lawsuits SET title=?, description=?, company=?, filing_date=?, deadline=?, status=?, updated_at=? WHERE source_url=?`)
	if err != nil {
		return 0, 0, fmt.Errorf("prepare update: %w", err)
	}
	defer updateStmt.Close()

	now := timeToStr(time.Now())

	for _, rec := range records {
		var existingID string
		var existingApplied int
		var existingAppliedAt sql.NullString
		var existingCreatedAt string

		err := checkStmt.QueryRowContext(ctx, rec.SourceURL).Scan(&existingID, &existingApplied, &existingAppliedAt, &existingCreatedAt)
		if err == sql.ErrNoRows {
			// New record — insert
			_, execErr := insertStmt.ExecContext(ctx,
				rec.ID, rec.Title, rec.Description, rec.SourceURL, rec.Company,
				optionalTimeToStr(rec.FilingDate), optionalTimeToStr(rec.Deadline),
				rec.Status, boolToInt(rec.Applied), optionalTimeToStr(rec.AppliedAt),
				now, now,
			)
			if execErr != nil {
				return 0, 0, fmt.Errorf("insert record: %w", execErr)
			}
			inserted++
		} else if err != nil {
			return 0, 0, fmt.Errorf("check existing: %w", err)
		} else {
			// Existing record — update, preserving applied/applied_at/created_at
			_, execErr := updateStmt.ExecContext(ctx,
				rec.Title, rec.Description, rec.Company,
				optionalTimeToStr(rec.FilingDate), optionalTimeToStr(rec.Deadline),
				rec.Status, now, rec.SourceURL,
			)
			if execErr != nil {
				return 0, 0, fmt.Errorf("update record: %w", execErr)
			}
			updated++
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("commit tx: %w", err)
	}
	return inserted, updated, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ListLawsuits returns lawsuit records, optionally filtered by company names using case-insensitive LIKE matching.
func (r *SQLiteRepository) ListLawsuits(ctx context.Context, filter LawsuitFilter) ([]model.LawsuitRecord, error) {
	query := `SELECT id, title, description, source_url, company, filing_date, deadline, status, applied, applied_at, created_at, updated_at FROM lawsuits`
	var args []interface{}

	if len(filter.Companies) > 0 {
		clauses := make([]string, len(filter.Companies))
		for i, c := range filter.Companies {
			clauses[i] = "LOWER(company) LIKE ?"
			args = append(args, "%"+strings.ToLower(c)+"%")
		}
		query += " WHERE (" + strings.Join(clauses, " OR ") + ")"
	}

	query += " ORDER BY created_at DESC"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query lawsuits: %w", err)
	}
	defer rows.Close()

	var results []model.LawsuitRecord
	for rows.Next() {
		var rec model.LawsuitRecord
		var filingDate, deadline, appliedAt, createdAt, updatedAt sql.NullString
		var applied int

		if err := rows.Scan(
			&rec.ID, &rec.Title, &rec.Description, &rec.SourceURL, &rec.Company,
			&filingDate, &deadline, &rec.Status, &applied, &appliedAt,
			&createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan lawsuit: %w", err)
		}

		rec.Applied = applied == 1
		rec.FilingDate = parseOptionalTime(filingDate)
		rec.Deadline = parseOptionalTime(deadline)
		rec.AppliedAt = parseOptionalTime(appliedAt)
		if createdAt.Valid {
			if t, err := parseTime(createdAt.String); err == nil {
				rec.CreatedAt = t
			}
		}
		if updatedAt.Valid {
			if t, err := parseTime(updatedAt.String); err == nil {
				rec.UpdatedAt = t
			}
		}

		results = append(results, rec)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}

	return results, nil
}

// MarkApplied marks a lawsuit as applied. Idempotent: only sets applied_at if not already set.
func (r *SQLiteRepository) MarkApplied(ctx context.Context, lawsuitID string) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE lawsuits SET applied = 1, applied_at = COALESCE(applied_at, ?) WHERE id = ?`,
		timeToStr(time.Now()), lawsuitID,
	)
	if err != nil {
		return fmt.Errorf("mark applied: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("lawsuit not found: %s", lawsuitID)
	}
	return nil
}

// GetAppliedStatus returns whether a lawsuit has been marked as applied.
func (r *SQLiteRepository) GetAppliedStatus(ctx context.Context, lawsuitID string) (bool, error) {
	var applied int
	err := r.db.QueryRowContext(ctx, `SELECT applied FROM lawsuits WHERE id = ?`, lawsuitID).Scan(&applied)
	if err == sql.ErrNoRows {
		return false, fmt.Errorf("lawsuit not found: %s", lawsuitID)
	}
	if err != nil {
		return false, fmt.Errorf("get applied status: %w", err)
	}
	return applied == 1, nil
}

// GetCompanyFilter returns all company names in the filter list.
func (r *SQLiteRepository) GetCompanyFilter(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT name FROM company_filter ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("query company filter: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan company: %w", err)
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// AddCompany adds a company name to the filter list, normalized to lowercase.
func (r *SQLiteRepository) AddCompany(ctx context.Context, name string) error {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return fmt.Errorf("company name cannot be empty")
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO company_filter (name) VALUES (?)`, normalized,
	)
	if err != nil {
		return fmt.Errorf("add company: %w", err)
	}
	return nil
}

// RemoveCompany removes a company name from the filter list, normalized to lowercase.
func (r *SQLiteRepository) RemoveCompany(ctx context.Context, name string) error {
	normalized := strings.ToLower(strings.TrimSpace(name))
	_, err := r.db.ExecContext(ctx, `DELETE FROM company_filter WHERE name = ?`, normalized)
	if err != nil {
		return fmt.Errorf("remove company: %w", err)
	}
	return nil
}

// siteErrorJSON is the JSON-serializable form of model.SiteError.
type siteErrorJSON struct {
	SiteName string `json:"site_name"`
	URL      string `json:"url"`
	Err      string `json:"error"`
}

// SaveRunResult persists a scraping run result, serializing errors as JSON.
func (r *SQLiteRepository) SaveRunResult(ctx context.Context, result model.RunResult) error {
	var errorsJSON []byte
	if len(result.Errors) > 0 {
		errs := make([]siteErrorJSON, len(result.Errors))
		for i, e := range result.Errors {
			errStr := ""
			if e.Err != nil {
				errStr = e.Err.Error()
			}
			errs[i] = siteErrorJSON{
				SiteName: e.SiteName,
				URL:      e.URL,
				Err:      errStr,
			}
		}
		var err error
		errorsJSON, err = json.Marshal(errs)
		if err != nil {
			return fmt.Errorf("marshal errors: %w", err)
		}
	}

	var endTime sql.NullString
	if !result.EndTime.IsZero() {
		endTime = sql.NullString{String: timeToStr(result.EndTime), Valid: true}
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO scrape_runs (id, start_time, end_time, total_found, new_records, updated_records, errors)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		result.ID, timeToStr(result.StartTime), endTime,
		result.TotalFound, result.NewRecords, result.UpdatedRecords,
		string(errorsJSON),
	)
	if err != nil {
		return fmt.Errorf("save run result: %w", err)
	}
	return nil
}

// GetLatestRunResult returns the most recent scraping run result, or nil if none exist.
func (r *SQLiteRepository) GetLatestRunResult(ctx context.Context) (*model.RunResult, error) {
	var result model.RunResult
	var startTime, endTime sql.NullString
	var errorsStr sql.NullString

	err := r.db.QueryRowContext(ctx,
		`SELECT id, start_time, end_time, total_found, new_records, updated_records, errors
		 FROM scrape_runs ORDER BY start_time DESC LIMIT 1`,
	).Scan(&result.ID, &startTime, &endTime, &result.TotalFound, &result.NewRecords, &result.UpdatedRecords, &errorsStr)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get latest run: %w", err)
	}

	if startTime.Valid {
		if t, err := parseTime(startTime.String); err == nil {
			result.StartTime = t
		}
	}
	if endTime.Valid {
		if t, err := parseTime(endTime.String); err == nil {
			result.EndTime = t
		}
	}

	if errorsStr.Valid && errorsStr.String != "" {
		var errs []siteErrorJSON
		if err := json.Unmarshal([]byte(errorsStr.String), &errs); err == nil {
			result.Errors = make([]model.SiteError, len(errs))
			for i, e := range errs {
				result.Errors[i] = model.SiteError{
					SiteName: e.SiteName,
					URL:      e.URL,
					Err:      fmt.Errorf("%s", e.Err),
				}
			}
		}
	}

	return &result, nil
}
