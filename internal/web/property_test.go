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
	"sync"
	"testing"
	"time"

	"classact/internal/compliance"
	"classact/internal/config"
	"classact/internal/model"
	"classact/internal/scraper"

	"pgregory.net/rapid"
)

// genLawsuitRecord generates a random LawsuitRecord for property testing.
func genLawsuitRecord(t *rapid.T) model.LawsuitRecord {
	id := rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "id")
	title := rapid.StringMatching(`[A-Za-z ]{5,50}`).Draw(t, "title")
	company := rapid.StringMatching(`[A-Za-z ]{3,30}`).Draw(t, "company")
	sourceURL := fmt.Sprintf("https://example.com/%s", rapid.StringMatching(`[a-z0-9]{5,20}`).Draw(t, "path"))
	status := rapid.SampledFrom([]string{"open", "closed", "settled"}).Draw(t, "status")
	now := time.Now().Truncate(time.Second)

	return model.LawsuitRecord{
		ID:        id,
		Title:     title,
		Company:   company,
		SourceURL: sourceURL,
		Status:    status,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// genRunResult generates a random RunResult for property testing.
func genRunResult(t *rapid.T) *model.RunResult {
	start := time.Now().Add(-time.Duration(rapid.IntRange(1, 3600).Draw(t, "offset")) * time.Second).Truncate(time.Second)
	end := start.Add(time.Duration(rapid.IntRange(1, 300).Draw(t, "duration")) * time.Second)

	return &model.RunResult{
		ID:             fmt.Sprintf("run-%s", rapid.StringMatching(`[a-f0-9]{8}`).Draw(t, "runID")),
		StartTime:      start,
		EndTime:        end,
		TotalFound:     rapid.IntRange(0, 100).Draw(t, "totalFound"),
		NewRecords:     rapid.IntRange(0, 50).Draw(t, "newRecords"),
		UpdatedRecords: rapid.IntRange(0, 50).Draw(t, "updatedRecords"),
	}
}

// newMockEngine creates a scraper.Engine with mock crawl functions that complete
// instantly. This is used in Property 17 to prevent panics when handleTriggerScrape
// launches a background goroutine.
func newMockEngine(repo *mockRepository, logger *slog.Logger) *scraper.Engine {
	return newMockEngineWithDelay(repo, logger, 0)
}

// newMockEngineWithDelay creates a mock engine whose crawl functions sleep for
// the given duration before returning. This is useful for Property 17 where we
// need the scraping flag to stay set while concurrent requests are in flight.
func newMockEngineWithDelay(repo *mockRepository, logger *slog.Logger, crawlDelay time.Duration) *scraper.Engine {
	eng := scraper.NewEngine(repo, logger, 1)
	eng.GetSites = func() []config.TargetSite {
		return []config.TargetSite{
			{Name: "mock-site", URL: "https://mock.example.com", RateLimit: time.Millisecond},
		}
	}
	eng.NewPolicy = func(_ context.Context, _ config.TargetSite) (*compliance.Policy, error) {
		return compliance.NewPolicyFromRobots("User-agent: *\nAllow: /\n", time.Millisecond)
	}
	eng.CollyCrawl = func(_ context.Context, _ config.TargetSite, _ *compliance.Policy) ([]model.LawsuitRecord, error) {
		if crawlDelay > 0 {
			time.Sleep(crawlDelay)
		}
		return nil, nil
	}
	eng.HeadlessCrawl = func(_ context.Context, _ config.TargetSite, _ *compliance.Policy) ([]model.LawsuitRecord, error) {
		if crawlDelay > 0 {
			time.Sleep(crawlDelay)
		}
		return nil, nil
	}
	return eng
}

// Feature: class-action-scraper, Property 16: Web frontend lists all records and run status
// **Validates: Requirements 11.2, 11.6**
func TestProperty16_WebFrontendListsAllRecordsAndRunStatus(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate 1-10 random lawsuit records.
		n := rapid.IntRange(1, 10).Draw(t, "numRecords")
		records := make([]model.LawsuitRecord, n)
		for i := 0; i < n; i++ {
			records[i] = genLawsuitRecord(t)
		}

		// Generate a random latest run result.
		latestRun := genRunResult(t)

		// Create mock repository with generated data.
		repo := &mockRepository{
			lawsuits:  records,
			latestRun: latestRun,
		}
		logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
		srv := NewServer(repo, nil, logger)
		handler := srv.Handler()

		// --- Test GET /api/scrape/status returns the latest run result ---
		statusReq := httptest.NewRequest(http.MethodGet, "/api/scrape/status", nil)
		statusW := httptest.NewRecorder()
		handler.ServeHTTP(statusW, statusReq)

		if statusW.Code != http.StatusOK {
			t.Fatalf("GET /api/scrape/status: expected 200, got %d", statusW.Code)
		}

		var statusResp scrapeStatusResponse
		if err := json.NewDecoder(statusW.Body).Decode(&statusResp); err != nil {
			t.Fatalf("failed to decode scrape status response: %v", err)
		}

		if statusResp.LatestRun == nil {
			t.Fatal("expected latest_run to be present in scrape status response")
		}
		if statusResp.LatestRun.ID != latestRun.ID {
			t.Errorf("expected run ID %q, got %q", latestRun.ID, statusResp.LatestRun.ID)
		}
		if statusResp.LatestRun.TotalFound != latestRun.TotalFound {
			t.Errorf("expected total_found=%d, got %d", latestRun.TotalFound, statusResp.LatestRun.TotalFound)
		}
		if statusResp.LatestRun.NewRecords != latestRun.NewRecords {
			t.Errorf("expected new_records=%d, got %d", latestRun.NewRecords, statusResp.LatestRun.NewRecords)
		}
		if statusResp.LatestRun.UpdatedRecords != latestRun.UpdatedRecords {
			t.Errorf("expected updated_records=%d, got %d", latestRun.UpdatedRecords, statusResp.LatestRun.UpdatedRecords)
		}

		// --- Test GET / (dashboard) contains all lawsuit titles ---
		dashReq := httptest.NewRequest(http.MethodGet, "/", nil)
		dashW := httptest.NewRecorder()
		handler.ServeHTTP(dashW, dashReq)

		if dashW.Code != http.StatusOK {
			t.Fatalf("GET /: expected 200, got %d", dashW.Code)
		}

		body := dashW.Body.String()
		for _, rec := range records {
			if !strings.Contains(body, rec.Title) {
				t.Errorf("dashboard HTML should contain lawsuit title %q", rec.Title)
			}
			if !strings.Contains(body, rec.Company) {
				t.Errorf("dashboard HTML should contain company name %q", rec.Company)
			}
		}

		// Verify run status info appears in the dashboard.
		if !strings.Contains(body, fmt.Sprintf("%d", latestRun.TotalFound)) {
			t.Errorf("dashboard should display total found count %d", latestRun.TotalFound)
		}
	})
}

// Feature: class-action-scraper, Property 17: Scrape trigger mutual exclusion
// **Validates: Requirements 11.10, 11.11**
func TestProperty17_ScrapeTriggerMutualExclusion(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random number of concurrent requests (2-10).
		numRequests := rapid.IntRange(2, 10).Draw(t, "numRequests")

		repo := &mockRepository{}
		logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

		// Create a real Engine with mock crawl functions that hold the scraping
		// flag long enough for all concurrent requests to observe it.
		engine := newMockEngineWithDelay(repo, logger, 500*time.Millisecond)

		srv := NewServer(repo, engine, logger)
		handler := srv.Handler()

		// Launch all requests simultaneously using goroutines.
		var wg sync.WaitGroup
		statusCodes := make([]int, numRequests)

		// Use a channel as a barrier so all goroutines start at the same time.
		barrier := make(chan struct{})

		for i := 0; i < numRequests; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				<-barrier // Wait for barrier to open.

				req := httptest.NewRequest(http.MethodPost, "/api/scrape", nil)
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)
				statusCodes[idx] = w.Code
			}(i)
		}

		// Open the barrier — all goroutines fire simultaneously.
		close(barrier)
		wg.Wait()

		// Wait briefly for the scrape goroutine to complete and reset the flag.
		time.Sleep(100 * time.Millisecond)

		// Count 202 Accepted and 409 Conflict responses.
		accepted := 0
		conflict := 0
		for _, code := range statusCodes {
			switch code {
			case http.StatusAccepted:
				accepted++
			case http.StatusConflict:
				conflict++
			default:
				t.Errorf("unexpected status code: %d", code)
			}
		}

		if accepted != 1 {
			t.Errorf("expected exactly 1 accepted (202), got %d", accepted)
		}
		if conflict != numRequests-1 {
			t.Errorf("expected %d conflicts (409), got %d", numRequests-1, conflict)
		}
	})
}
