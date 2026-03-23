package scraper

import (
	"bytes"
	"classact/internal/compliance"
	"classact/internal/config"
	"classact/internal/model"
	"classact/internal/storage"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"pgregory.net/rapid"
)

// Feature: class-action-scraper, Property 14: Structured JSON logging
// **Validates: Requirements 9.1, 9.2, 9.4**
func TestProperty14_StructuredJSONLogging(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		repo, err := storage.NewSQLiteRepository(":memory:")
		if err != nil {
			rt.Fatalf("NewSQLiteRepository: %v", err)
		}
		defer repo.Close()

		// Generate 1-5 sites, some succeeding and some failing
		numSites := rapid.IntRange(1, 5).Draw(rt, "numSites")
		sites := make([]config.TargetSite, numSites)
		for i := 0; i < numSites; i++ {
			sites[i] = genSite(rt, i)
		}

		// Randomly decide which sites fail
		failSet := make(map[int]bool)
		for i := 0; i < numSites; i++ {
			if rapid.Bool().Draw(rt, fmt.Sprintf("fail_%d", i)) {
				failSet[i] = true
			}
		}

		var mu sync.Mutex

		mockCrawl := func(ctx context.Context, site config.TargetSite, policy *compliance.Policy) ([]model.LawsuitRecord, error) {
			for i, s := range sites {
				if s.Name == site.Name && failSet[i] {
					return nil, fmt.Errorf("simulated error for %s", site.Name)
				}
			}
			return []model.LawsuitRecord{
				{
					ID:        fmt.Sprintf("rec-%s", site.Name),
					Title:     "Lawsuit from " + site.Name,
					SourceURL: site.URL + "/lawsuit",
					Company:   "TestCo",
					Status:    "open",
				},
			}, nil
		}
		_ = mu // suppress unused warning if needed

		// Create a logger that writes JSON to a buffer
		var buf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&buf, nil))

		engine := NewEngine(repo, logger, numSites)
		engine.GetSites = func() []config.TargetSite { return sites }
		engine.NewPolicy = func(ctx context.Context, site config.TargetSite) (*compliance.Policy, error) {
			return testPolicy()
		}
		engine.CollyCrawl = mockCrawl
		engine.HeadlessCrawl = mockCrawl

		_, runErr := engine.Run(context.Background())
		if runErr != nil {
			rt.Fatalf("Run returned fatal error: %v", runErr)
		}

		// Parse each line of the captured log output as JSON
		logOutput := buf.String()
		lines := strings.Split(strings.TrimSpace(logOutput), "\n")
		if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
			rt.Fatalf("expected log output, got none")
		}

		var foundRunSummary bool
		var errorLogCount int

		for _, line := range lines {
			if line == "" {
				continue
			}

			var entry map[string]interface{}
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				rt.Fatalf("log line is not valid JSON: %q, err: %v", line, err)
			}

			// Every JSON log entry must have "msg" and "level" fields (slog standard)
			if _, ok := entry["msg"]; !ok {
				rt.Fatalf("log entry missing 'msg' field: %v", entry)
			}
			if _, ok := entry["level"]; !ok {
				rt.Fatalf("log entry missing 'level' field: %v", entry)
			}

			msg, _ := entry["msg"].(string)

			// Check run-summary log (the "scraping run completed" message)
			if msg == "scraping run completed" {
				foundRunSummary = true
				for _, field := range []string{"start_time", "end_time", "total_found", "duration"} {
					if _, ok := entry[field]; !ok {
						rt.Fatalf("run summary log missing required field %q: %v", field, entry)
					}
				}
			}

			// Check error log entries for crawl failures
			if msg == "crawl failed" {
				errorLogCount++
				for _, field := range []string{"site", "url", "error"} {
					if _, ok := entry[field]; !ok {
						rt.Fatalf("error log entry missing required field %q: %v", field, entry)
					}
				}
			}

			// Also check compliance policy error logs
			if msg == "compliance policy creation failed" {
				for _, field := range []string{"site", "url", "error"} {
					if _, ok := entry[field]; !ok {
						rt.Fatalf("compliance error log entry missing required field %q: %v", field, entry)
					}
				}
			}

			// Check panic recovery error logs
			if msg == "panic recovered in site goroutine" {
				for _, field := range []string{"site", "url"} {
					if _, ok := entry[field]; !ok {
						rt.Fatalf("panic error log entry missing required field %q: %v", field, entry)
					}
				}
			}
		}

		// The run summary must always be present
		if !foundRunSummary {
			rt.Fatalf("no 'scraping run completed' summary log found in output")
		}

		// If there were failing sites, we should see error logs
		if len(failSet) > 0 && errorLogCount == 0 {
			rt.Fatalf("expected error log entries for %d failing sites, found none", len(failSet))
		}
	})
}
