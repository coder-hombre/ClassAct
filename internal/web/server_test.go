package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"classact/internal/model"
	"classact/internal/storage"
)

// mockRepository implements storage.Repository for testing.
type mockRepository struct {
	lawsuits  []model.LawsuitRecord
	companies []string
	latestRun *model.RunResult
	appliedID string
	addedCo   string
	removedCo string
}

func (m *mockRepository) UpsertLawsuits(_ context.Context, records []model.LawsuitRecord) (int, int, error) {
	m.lawsuits = append(m.lawsuits, records...)
	return len(records), 0, nil
}

func (m *mockRepository) ListLawsuits(_ context.Context, _ storage.LawsuitFilter) ([]model.LawsuitRecord, error) {
	return m.lawsuits, nil
}

func (m *mockRepository) MarkApplied(_ context.Context, id string) error {
	for i := range m.lawsuits {
		if m.lawsuits[i].ID == id {
			m.lawsuits[i].Applied = true
			now := time.Now()
			m.lawsuits[i].AppliedAt = &now
			m.appliedID = id
			return nil
		}
	}
	return fmt.Errorf("lawsuit not found: %s", id)
}

func (m *mockRepository) GetAppliedStatus(_ context.Context, id string) (bool, error) {
	for _, l := range m.lawsuits {
		if l.ID == id {
			return l.Applied, nil
		}
	}
	return false, fmt.Errorf("lawsuit not found: %s", id)
}

func (m *mockRepository) GetCompanyFilter(_ context.Context) ([]string, error) {
	return m.companies, nil
}

func (m *mockRepository) AddCompany(_ context.Context, name string) error {
	m.addedCo = strings.ToLower(strings.TrimSpace(name))
	m.companies = append(m.companies, m.addedCo)
	return nil
}

func (m *mockRepository) RemoveCompany(_ context.Context, name string) error {
	m.removedCo = strings.ToLower(strings.TrimSpace(name))
	filtered := m.companies[:0]
	for _, c := range m.companies {
		if c != m.removedCo {
			filtered = append(filtered, c)
		}
	}
	m.companies = filtered
	return nil
}

func (m *mockRepository) SaveRunResult(_ context.Context, result model.RunResult) error {
	m.latestRun = &result
	return nil
}

func (m *mockRepository) GetLatestRunResult(_ context.Context) (*model.RunResult, error) {
	return m.latestRun, nil
}

func (m *mockRepository) Close() error { return nil }

func newTestServer(repo *mockRepository) *Server {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	return NewServer(repo, nil, logger)
}

func TestGetDashboard(t *testing.T) {
	repo := &mockRepository{}
	srv := newTestServer(repo)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "ClassAct Dashboard") {
		t.Error("dashboard should contain title")
	}
	if !strings.Contains(body, "No lawsuits found") {
		t.Error("empty state message should appear when no lawsuits")
	}
}

func TestGetDashboardWithLawsuits(t *testing.T) {
	now := time.Now()
	repo := &mockRepository{
		lawsuits: []model.LawsuitRecord{
			{ID: "1", Title: "Test Lawsuit", Company: "Acme Corp", Status: "open", SourceURL: "https://example.com/1", CreatedAt: now, UpdatedAt: now},
		},
		latestRun: &model.RunResult{
			ID: "run-1", StartTime: now, EndTime: now, TotalFound: 1, NewRecords: 1,
		},
	}
	srv := newTestServer(repo)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Test Lawsuit") {
		t.Error("dashboard should contain lawsuit title")
	}
	if !strings.Contains(body, "Acme Corp") {
		t.Error("dashboard should contain company name")
	}
}

func TestPostScrapeConflict(t *testing.T) {
	repo := &mockRepository{}
	srv := newTestServer(repo)
	// Simulate an already-running scrape.
	srv.scraping.Store(true)

	req := httptest.NewRequest(http.MethodPost, "/api/scrape", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
}

func TestGetScrapeStatus(t *testing.T) {
	now := time.Now()
	repo := &mockRepository{
		latestRun: &model.RunResult{
			ID: "run-1", StartTime: now, EndTime: now, TotalFound: 5,
		},
	}
	srv := newTestServer(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/scrape/status", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp scrapeStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Scraping {
		t.Error("expected scraping=false")
	}
	if resp.LatestRun == nil {
		t.Fatal("expected latest_run to be present")
	}
	if resp.LatestRun.TotalFound != 5 {
		t.Errorf("expected total_found=5, got %d", resp.LatestRun.TotalFound)
	}
}

func TestPostMarkApplied(t *testing.T) {
	now := time.Now()
	repo := &mockRepository{
		lawsuits: []model.LawsuitRecord{
			{ID: "abc-123", Title: "Test", Company: "Co", Status: "open", SourceURL: "https://x.com/1", CreatedAt: now, UpdatedAt: now},
		},
	}
	srv := newTestServer(repo)

	req := httptest.NewRequest(http.MethodPost, "/api/applied/abc-123", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if repo.appliedID != "abc-123" {
		t.Errorf("expected applied ID abc-123, got %s", repo.appliedID)
	}
}

func TestPostMarkAppliedNotFound(t *testing.T) {
	repo := &mockRepository{}
	srv := newTestServer(repo)

	req := httptest.NewRequest(http.MethodPost, "/api/applied/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestGetCompanies(t *testing.T) {
	repo := &mockRepository{companies: []string{"acme", "globex"}}
	srv := newTestServer(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/companies", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var companies []string
	if err := json.NewDecoder(w.Body).Decode(&companies); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(companies) != 2 {
		t.Errorf("expected 2 companies, got %d", len(companies))
	}
}

func TestGetCompaniesEmpty(t *testing.T) {
	repo := &mockRepository{}
	srv := newTestServer(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/companies", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var companies []string
	if err := json.NewDecoder(w.Body).Decode(&companies); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if companies == nil || len(companies) != 0 {
		t.Errorf("expected empty array, got %v", companies)
	}
}

func TestPostAddCompany(t *testing.T) {
	repo := &mockRepository{}
	srv := newTestServer(repo)

	body := strings.NewReader(`{"name":"Acme Corp"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/companies", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
	if repo.addedCo != "acme corp" {
		t.Errorf("expected added company 'acme corp', got '%s'", repo.addedCo)
	}
}

func TestPostAddCompanyEmpty(t *testing.T) {
	repo := &mockRepository{}
	srv := newTestServer(repo)

	body := strings.NewReader(`{"name":"  "}`)
	req := httptest.NewRequest(http.MethodPost, "/api/companies", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestDeleteCompany(t *testing.T) {
	repo := &mockRepository{companies: []string{"acme", "globex"}}
	srv := newTestServer(repo)

	req := httptest.NewRequest(http.MethodDelete, "/api/companies/acme", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if repo.removedCo != "acme" {
		t.Errorf("expected removed company 'acme', got '%s'", repo.removedCo)
	}
}

func TestGetLogs(t *testing.T) {
	now := time.Now()
	repo := &mockRepository{
		latestRun: &model.RunResult{
			ID: "run-1", StartTime: now, EndTime: now, TotalFound: 3,
			Errors: []model.SiteError{{SiteName: "test", URL: "https://test.com", Err: fmt.Errorf("timeout")}},
		},
	}
	srv := newTestServer(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result runResult
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.TotalFound != 3 {
		t.Errorf("expected total_found=3, got %d", result.TotalFound)
	}
	if len(result.Errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(result.Errors))
	}
}

func TestGetLogsEmpty(t *testing.T) {
	repo := &mockRepository{}
	srv := newTestServer(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result map[string]any
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result["message"] != "no scraping runs yet" {
		t.Errorf("expected empty message, got %v", result)
	}
}
