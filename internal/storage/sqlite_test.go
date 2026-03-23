package storage

import (
	"classact/internal/model"
	"context"
	"fmt"
	"testing"
	"time"
)

func newTestRepo(t *testing.T) *SQLiteRepository {
	t.Helper()
	repo, err := NewSQLiteRepository(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteRepository: %v", err)
	}
	t.Cleanup(func() { repo.Close() })
	return repo
}

func makeRecord(id, title, sourceURL, company string) model.LawsuitRecord {
	return model.LawsuitRecord{
		ID:        id,
		Title:     title,
		SourceURL: sourceURL,
		Company:   company,
		Status:    "open",
	}
}

func TestAutoMigration(t *testing.T) {
	repo := newTestRepo(t)
	// Tables should exist — query each one
	ctx := context.Background()
	if _, err := repo.db.ExecContext(ctx, "SELECT 1 FROM lawsuits LIMIT 1"); err != nil {
		t.Errorf("lawsuits table missing: %v", err)
	}
	if _, err := repo.db.ExecContext(ctx, "SELECT 1 FROM company_filter LIMIT 1"); err != nil {
		t.Errorf("company_filter table missing: %v", err)
	}
	if _, err := repo.db.ExecContext(ctx, "SELECT 1 FROM scrape_runs LIMIT 1"); err != nil {
		t.Errorf("scrape_runs table missing: %v", err)
	}
}

func TestUpsertLawsuits_InsertNew(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	records := []model.LawsuitRecord{
		makeRecord("1", "Lawsuit A", "https://example.com/a", "Acme Corp"),
		makeRecord("2", "Lawsuit B", "https://example.com/b", "Beta Inc"),
	}

	inserted, updated, err := repo.UpsertLawsuits(ctx, records)
	if err != nil {
		t.Fatalf("UpsertLawsuits: %v", err)
	}
	if inserted != 2 {
		t.Errorf("inserted = %d, want 2", inserted)
	}
	if updated != 0 {
		t.Errorf("updated = %d, want 0", updated)
	}

	all, err := repo.ListLawsuits(ctx, LawsuitFilter{})
	if err != nil {
		t.Fatalf("ListLawsuits: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("len = %d, want 2", len(all))
	}
}

func TestUpsertLawsuits_UpdateExisting(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	rec := makeRecord("1", "Original Title", "https://example.com/a", "Acme Corp")
	repo.UpsertLawsuits(ctx, []model.LawsuitRecord{rec})

	// Upsert again with same source_url but different title
	rec2 := makeRecord("1-new", "Updated Title", "https://example.com/a", "Acme Corp Updated")
	inserted, updated, err := repo.UpsertLawsuits(ctx, []model.LawsuitRecord{rec2})
	if err != nil {
		t.Fatalf("UpsertLawsuits: %v", err)
	}
	if inserted != 0 {
		t.Errorf("inserted = %d, want 0", inserted)
	}
	if updated != 1 {
		t.Errorf("updated = %d, want 1", updated)
	}

	all, err := repo.ListLawsuits(ctx, LawsuitFilter{})
	if err != nil {
		t.Fatalf("ListLawsuits: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("len = %d, want 1", len(all))
	}
	if all[0].Title != "Updated Title" {
		t.Errorf("title = %q, want %q", all[0].Title, "Updated Title")
	}
}

func TestUpsertLawsuits_PreservesAppliedOnUpdate(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	rec := makeRecord("1", "Lawsuit A", "https://example.com/a", "Acme")
	repo.UpsertLawsuits(ctx, []model.LawsuitRecord{rec})
	repo.MarkApplied(ctx, "1")

	// Upsert again — applied status should be preserved
	rec2 := makeRecord("1-new", "Lawsuit A v2", "https://example.com/a", "Acme")
	repo.UpsertLawsuits(ctx, []model.LawsuitRecord{rec2})

	applied, err := repo.GetAppliedStatus(ctx, "1")
	if err != nil {
		t.Fatalf("GetAppliedStatus: %v", err)
	}
	if !applied {
		t.Error("applied status was not preserved after upsert")
	}
}

func TestUpsertLawsuits_Empty(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	inserted, updated, err := repo.UpsertLawsuits(ctx, nil)
	if err != nil {
		t.Fatalf("UpsertLawsuits: %v", err)
	}
	if inserted != 0 || updated != 0 {
		t.Errorf("expected 0/0, got %d/%d", inserted, updated)
	}
}

func TestListLawsuits_CompanyFilter(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	records := []model.LawsuitRecord{
		makeRecord("1", "A", "https://example.com/a", "Apple Inc"),
		makeRecord("2", "B", "https://example.com/b", "Google LLC"),
		makeRecord("3", "C", "https://example.com/c", "APPLE INC"),
	}
	repo.UpsertLawsuits(ctx, records)

	// Filter by "apple" — should match case-insensitively
	filtered, err := repo.ListLawsuits(ctx, LawsuitFilter{Companies: []string{"apple"}})
	if err != nil {
		t.Fatalf("ListLawsuits: %v", err)
	}
	if len(filtered) != 2 {
		t.Errorf("len = %d, want 2", len(filtered))
	}

	// Empty filter — return all
	all, err := repo.ListLawsuits(ctx, LawsuitFilter{})
	if err != nil {
		t.Fatalf("ListLawsuits: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("len = %d, want 3", len(all))
	}
}

func TestMarkApplied_Idempotent(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	rec := makeRecord("1", "Lawsuit", "https://example.com/a", "Acme")
	repo.UpsertLawsuits(ctx, []model.LawsuitRecord{rec})

	// First mark
	if err := repo.MarkApplied(ctx, "1"); err != nil {
		t.Fatalf("MarkApplied (1st): %v", err)
	}

	// Get the applied_at timestamp
	all, _ := repo.ListLawsuits(ctx, LawsuitFilter{})
	firstAppliedAt := all[0].AppliedAt

	// Wait a bit and mark again
	time.Sleep(10 * time.Millisecond)
	if err := repo.MarkApplied(ctx, "1"); err != nil {
		t.Fatalf("MarkApplied (2nd): %v", err)
	}

	// applied_at should not change
	all, _ = repo.ListLawsuits(ctx, LawsuitFilter{})
	if !all[0].AppliedAt.Equal(*firstAppliedAt) {
		t.Errorf("applied_at changed: %v -> %v", firstAppliedAt, all[0].AppliedAt)
	}
	if !all[0].Applied {
		t.Error("expected Applied=true")
	}
}

func TestMarkApplied_NotFound(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	err := repo.MarkApplied(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent lawsuit")
	}
}

func TestGetAppliedStatus(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	rec := makeRecord("1", "Lawsuit", "https://example.com/a", "Acme")
	repo.UpsertLawsuits(ctx, []model.LawsuitRecord{rec})

	applied, err := repo.GetAppliedStatus(ctx, "1")
	if err != nil {
		t.Fatalf("GetAppliedStatus: %v", err)
	}
	if applied {
		t.Error("expected not applied initially")
	}

	repo.MarkApplied(ctx, "1")
	applied, err = repo.GetAppliedStatus(ctx, "1")
	if err != nil {
		t.Fatalf("GetAppliedStatus: %v", err)
	}
	if !applied {
		t.Error("expected applied after MarkApplied")
	}
}

func TestCompanyFilter_AddRemoveGet(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Initially empty
	names, err := repo.GetCompanyFilter(ctx)
	if err != nil {
		t.Fatalf("GetCompanyFilter: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("expected empty, got %v", names)
	}

	// Add companies
	repo.AddCompany(ctx, "Apple")
	repo.AddCompany(ctx, "GOOGLE")
	repo.AddCompany(ctx, "  Meta  ")

	names, _ = repo.GetCompanyFilter(ctx)
	if len(names) != 3 {
		t.Fatalf("expected 3 companies, got %d: %v", len(names), names)
	}

	// Verify normalization to lowercase
	expected := map[string]bool{"apple": true, "google": true, "meta": true}
	for _, n := range names {
		if !expected[n] {
			t.Errorf("unexpected company: %q", n)
		}
	}

	// Add duplicate — should be ignored
	repo.AddCompany(ctx, "apple")
	names, _ = repo.GetCompanyFilter(ctx)
	if len(names) != 3 {
		t.Errorf("duplicate not ignored, got %d", len(names))
	}

	// Remove
	repo.RemoveCompany(ctx, "Google")
	names, _ = repo.GetCompanyFilter(ctx)
	if len(names) != 2 {
		t.Errorf("expected 2 after remove, got %d", len(names))
	}
}

func TestCompanyFilter_EmptyName(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	err := repo.AddCompany(ctx, "  ")
	if err == nil {
		t.Error("expected error for empty company name")
	}
}

func TestSaveAndGetRunResult(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	start := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 15, 10, 5, 0, 0, time.UTC)

	result := model.RunResult{
		ID:             "run-1",
		StartTime:      start,
		EndTime:        end,
		TotalFound:     42,
		NewRecords:     30,
		UpdatedRecords: 12,
		Errors: []model.SiteError{
			{SiteName: "site-a", URL: "https://a.com", Err: fmt.Errorf("timeout")},
		},
	}

	if err := repo.SaveRunResult(ctx, result); err != nil {
		t.Fatalf("SaveRunResult: %v", err)
	}

	got, err := repo.GetLatestRunResult(ctx)
	if err != nil {
		t.Fatalf("GetLatestRunResult: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil result")
	}

	if got.ID != "run-1" {
		t.Errorf("ID = %q, want %q", got.ID, "run-1")
	}
	if got.TotalFound != 42 {
		t.Errorf("TotalFound = %d, want 42", got.TotalFound)
	}
	if got.NewRecords != 30 {
		t.Errorf("NewRecords = %d, want 30", got.NewRecords)
	}
	if got.UpdatedRecords != 12 {
		t.Errorf("UpdatedRecords = %d, want 12", got.UpdatedRecords)
	}
	if !got.StartTime.Equal(start) {
		t.Errorf("StartTime = %v, want %v", got.StartTime, start)
	}
	if !got.EndTime.Equal(end) {
		t.Errorf("EndTime = %v, want %v", got.EndTime, end)
	}
	if len(got.Errors) != 1 {
		t.Fatalf("Errors len = %d, want 1", len(got.Errors))
	}
	if got.Errors[0].SiteName != "site-a" {
		t.Errorf("SiteName = %q, want %q", got.Errors[0].SiteName, "site-a")
	}
	if got.Errors[0].Err.Error() != "timeout" {
		t.Errorf("Err = %q, want %q", got.Errors[0].Err.Error(), "timeout")
	}
}

func TestGetLatestRunResult_Empty(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	got, err := repo.GetLatestRunResult(ctx)
	if err != nil {
		t.Fatalf("GetLatestRunResult: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for empty DB, got %+v", got)
	}
}

func TestGetLatestRunResult_ReturnsLatest(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	r1 := model.RunResult{
		ID:        "run-1",
		StartTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		EndTime:   time.Date(2025, 1, 1, 0, 5, 0, 0, time.UTC),
	}
	r2 := model.RunResult{
		ID:        "run-2",
		StartTime: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		EndTime:   time.Date(2025, 1, 2, 0, 5, 0, 0, time.UTC),
	}

	repo.SaveRunResult(ctx, r1)
	repo.SaveRunResult(ctx, r2)

	got, _ := repo.GetLatestRunResult(ctx)
	if got.ID != "run-2" {
		t.Errorf("ID = %q, want %q (latest)", got.ID, "run-2")
	}
}

func TestSaveRunResult_NoErrors(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	result := model.RunResult{
		ID:        "run-no-err",
		StartTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		EndTime:   time.Date(2025, 1, 1, 0, 1, 0, 0, time.UTC),
	}

	if err := repo.SaveRunResult(ctx, result); err != nil {
		t.Fatalf("SaveRunResult: %v", err)
	}

	got, _ := repo.GetLatestRunResult(ctx)
	if len(got.Errors) != 0 {
		t.Errorf("expected no errors, got %d", len(got.Errors))
	}
}

func TestUniqueConstraintOnSourceURL(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	rec1 := makeRecord("1", "A", "https://example.com/same", "Acme")
	rec2 := makeRecord("2", "B", "https://example.com/same", "Beta")

	// Insert both in same batch — second should be treated as update since same source_url
	inserted, updated, err := repo.UpsertLawsuits(ctx, []model.LawsuitRecord{rec1, rec2})
	if err != nil {
		t.Fatalf("UpsertLawsuits: %v", err)
	}
	if inserted != 1 || updated != 1 {
		t.Errorf("expected 1 inserted + 1 updated, got %d/%d", inserted, updated)
	}

	all, _ := repo.ListLawsuits(ctx, LawsuitFilter{})
	if len(all) != 1 {
		t.Errorf("expected 1 record, got %d", len(all))
	}
}

func TestListLawsuits_EmptyDB(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	all, err := repo.ListLawsuits(ctx, LawsuitFilter{})
	if err != nil {
		t.Fatalf("ListLawsuits: %v", err)
	}
	if all != nil {
		t.Errorf("expected nil for empty DB, got %v", all)
	}
}
