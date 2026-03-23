package scraper

import (
	"classact/internal/compliance"
	"classact/internal/config"
	"classact/internal/model"
	"classact/internal/storage"
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// discardLogger returns a logger that discards all output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(
		devNull{}, &slog.HandlerOptions{Level: slog.LevelError + 1},
	))
}

type devNull struct{}

func (devNull) Write(p []byte) (int, error) { return len(p), nil }

// testPolicy creates a permissive compliance policy for testing.
func testPolicy() (*compliance.Policy, error) {
	return compliance.NewPolicyFromRobots("", 1*time.Millisecond)
}

// genSite generates a random TargetSite for property tests.
func genSite(rt *rapid.T, idx int) config.TargetSite {
	return config.TargetSite{
		Name:        fmt.Sprintf("site-%d-%s", idx, rapid.StringMatching(`[a-z]{3,8}`).Draw(rt, fmt.Sprintf("siteName_%d", idx))),
		URL:         fmt.Sprintf("https://test-%d.example.com/%s", idx, rapid.StringMatching(`[a-z]{3,8}`).Draw(rt, fmt.Sprintf("siteURL_%d", idx))),
		UseHeadless: rapid.Bool().Draw(rt, fmt.Sprintf("useHeadless_%d", idx)),
		RateLimit:   1 * time.Millisecond,
	}
}


// Feature: class-action-scraper, Property 2: All configured sites are attempted
// **Validates: Requirements 2.1**
func TestProperty2_AllConfiguredSitesAttempted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		repo, err := storage.NewSQLiteRepository(":memory:")
		if err != nil {
			rt.Fatalf("NewSQLiteRepository: %v", err)
		}
		defer repo.Close()

		// Generate 1-5 mock sites
		numSites := rapid.IntRange(1, 5).Draw(rt, "numSites")
		sites := make([]config.TargetSite, numSites)
		for i := 0; i < numSites; i++ {
			sites[i] = genSite(rt, i)
		}

		// Track which sites were crawled
		var mu sync.Mutex
		crawled := make(map[string]bool)

		mockCrawl := func(ctx context.Context, site config.TargetSite, policy *compliance.Policy) ([]model.LawsuitRecord, error) {
			mu.Lock()
			crawled[site.Name] = true
			mu.Unlock()
			return nil, nil
		}

		engine := NewEngine(repo, discardLogger(), numSites)
		engine.GetSites = func() []config.TargetSite { return sites }
		engine.NewPolicy = func(ctx context.Context, site config.TargetSite) (*compliance.Policy, error) {
			return testPolicy()
		}
		engine.CollyCrawl = mockCrawl
		engine.HeadlessCrawl = mockCrawl

		result, err := engine.Run(context.Background())
		if err != nil {
			rt.Fatalf("Run: %v", err)
		}

		// Build set of all sites that appear in results or errors
		attempted := make(map[string]bool)
		mu.Lock()
		for name := range crawled {
			attempted[name] = true
		}
		mu.Unlock()
		for _, se := range result.Errors {
			attempted[se.SiteName] = true
		}

		// Verify all N sites were attempted
		for _, site := range sites {
			if !attempted[site.Name] {
				rt.Fatalf("site %q was not attempted (crawled=%v, errors=%v)", site.Name, crawled, result.Errors)
			}
		}
	})
}

// Feature: class-action-scraper, Property 5: Fault tolerance across sites
// **Validates: Requirements 2.4, 7.5, 10.5**
func TestProperty5_FaultToleranceAcrossSites(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		repo, err := storage.NewSQLiteRepository(":memory:")
		if err != nil {
			rt.Fatalf("NewSQLiteRepository: %v", err)
		}
		defer repo.Close()

		// Generate 2-5 sites
		numSites := rapid.IntRange(2, 5).Draw(rt, "numSites")
		sites := make([]config.TargetSite, numSites)
		for i := 0; i < numSites; i++ {
			sites[i] = genSite(rt, i)
		}

		// Randomly choose which sites fail (at least 1 fails, at least 1 succeeds)
		failSet := make(map[int]bool)
		for i := 0; i < numSites; i++ {
			if rapid.Bool().Draw(rt, fmt.Sprintf("fail_%d", i)) {
				failSet[i] = true
			}
		}
		// Ensure at least one fails and at least one succeeds
		if len(failSet) == 0 {
			failSet[0] = true
		}
		if len(failSet) == numSites {
			delete(failSet, numSites-1)
		}

		var mu sync.Mutex
		successSites := make(map[string]bool)

		mockCrawl := func(ctx context.Context, site config.TargetSite, policy *compliance.Policy) ([]model.LawsuitRecord, error) {
			// Find the index of this site
			for i, s := range sites {
				if s.Name == site.Name {
					if failSet[i] {
						return nil, fmt.Errorf("simulated failure for %s", site.Name)
					}
					mu.Lock()
					successSites[site.Name] = true
					mu.Unlock()
					return []model.LawsuitRecord{
						{
							ID:        fmt.Sprintf("rec-%s", site.Name),
							Title:     "Test Lawsuit from " + site.Name,
							SourceURL: site.URL + "/lawsuit",
							Company:   "TestCo",
							Status:    "open",
						},
					}, nil
				}
			}
			return nil, fmt.Errorf("unknown site: %s", site.Name)
		}

		engine := NewEngine(repo, discardLogger(), numSites)
		engine.GetSites = func() []config.TargetSite { return sites }
		engine.NewPolicy = func(ctx context.Context, site config.TargetSite) (*compliance.Policy, error) {
			return testPolicy()
		}
		engine.CollyCrawl = mockCrawl
		engine.HeadlessCrawl = mockCrawl

		result, err := engine.Run(context.Background())
		if err != nil {
			rt.Fatalf("Run returned fatal error: %v", err)
		}

		// Verify successful sites produced results
		expectedSuccessCount := numSites - len(failSet)
		mu.Lock()
		actualSuccess := len(successSites)
		mu.Unlock()
		if actualSuccess != expectedSuccessCount {
			rt.Fatalf("expected %d successful sites, got %d", expectedSuccessCount, actualSuccess)
		}

		// Verify failed sites appear in RunResult.Errors
		errorSiteNames := make(map[string]bool)
		for _, se := range result.Errors {
			errorSiteNames[se.SiteName] = true
		}
		for i := range failSet {
			if !errorSiteNames[sites[i].Name] {
				rt.Fatalf("failed site %q not found in RunResult.Errors", sites[i].Name)
			}
		}
	})
}

// Feature: class-action-scraper, Property 15: Crawl strategy dispatch
// **Validates: Requirements 10.2, 10.3**
func TestProperty15_CrawlStrategyDispatch(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		repo, err := storage.NewSQLiteRepository(":memory:")
		if err != nil {
			rt.Fatalf("NewSQLiteRepository: %v", err)
		}
		defer repo.Close()

		// Generate 1-5 sites with random UseHeadless flags
		numSites := rapid.IntRange(1, 5).Draw(rt, "numSites")
		sites := make([]config.TargetSite, numSites)
		for i := 0; i < numSites; i++ {
			sites[i] = genSite(rt, i)
		}

		// Track which crawl function was called for each site
		var mu sync.Mutex
		collyCalledFor := make(map[string]bool)
		headlessCalledFor := make(map[string]bool)

		collyCrawl := func(ctx context.Context, site config.TargetSite, policy *compliance.Policy) ([]model.LawsuitRecord, error) {
			mu.Lock()
			collyCalledFor[site.Name] = true
			mu.Unlock()
			return nil, nil
		}

		headlessCrawl := func(ctx context.Context, site config.TargetSite, policy *compliance.Policy) ([]model.LawsuitRecord, error) {
			mu.Lock()
			headlessCalledFor[site.Name] = true
			mu.Unlock()
			return nil, nil
		}

		engine := NewEngine(repo, discardLogger(), numSites)
		engine.GetSites = func() []config.TargetSite { return sites }
		engine.NewPolicy = func(ctx context.Context, site config.TargetSite) (*compliance.Policy, error) {
			return testPolicy()
		}
		engine.CollyCrawl = collyCrawl
		engine.HeadlessCrawl = headlessCrawl

		_, err = engine.Run(context.Background())
		if err != nil {
			rt.Fatalf("Run: %v", err)
		}

		mu.Lock()
		defer mu.Unlock()

		for _, site := range sites {
			if site.UseHeadless {
				if !headlessCalledFor[site.Name] {
					rt.Fatalf("site %q has UseHeadless=true but headless crawl was not called", site.Name)
				}
				if collyCalledFor[site.Name] {
					rt.Fatalf("site %q has UseHeadless=true but colly crawl was also called", site.Name)
				}
			} else {
				if !collyCalledFor[site.Name] {
					rt.Fatalf("site %q has UseHeadless=false but colly crawl was not called", site.Name)
				}
				if headlessCalledFor[site.Name] {
					rt.Fatalf("site %q has UseHeadless=false but headless crawl was also called", site.Name)
				}
			}
		}
	})
}

// Feature: class-action-scraper, Property 13: Context cancellation propagation
// **Validates: Requirements 7.4**
func TestProperty13_ContextCancellationPropagation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		repo, err := storage.NewSQLiteRepository(":memory:")
		if err != nil {
			rt.Fatalf("NewSQLiteRepository: %v", err)
		}
		defer repo.Close()

		// Generate 1-5 sites
		numSites := rapid.IntRange(1, 5).Draw(rt, "numSites")
		sites := make([]config.TargetSite, numSites)
		for i := 0; i < numSites; i++ {
			sites[i] = genSite(rt, i)
		}

		// Mock crawl that blocks until context is cancelled
		mockCrawl := func(ctx context.Context, site config.TargetSite, policy *compliance.Policy) ([]model.LawsuitRecord, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}

		engine := NewEngine(repo, discardLogger(), numSites)
		engine.GetSites = func() []config.TargetSite { return sites }
		engine.NewPolicy = func(ctx context.Context, site config.TargetSite) (*compliance.Policy, error) {
			return testPolicy()
		}
		engine.CollyCrawl = mockCrawl
		engine.HeadlessCrawl = mockCrawl

		// Use an already-cancelled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		deadline := time.Now().Add(5 * time.Second)
		result, runErr := engine.Run(ctx)

		// Verify timely return (well within 5 seconds)
		if time.Now().After(deadline) {
			rt.Fatalf("Run did not return within bounded time after context cancellation")
		}

		// The engine should still return a result (not a fatal error) since
		// individual site context errors are collected in RunResult.Errors.
		// However, it's also acceptable for Run to return an error if context
		// cancellation is treated as fatal.
		if runErr != nil {
			// Fatal error path — acceptable if context error
			return
		}

		// Non-fatal path: all sites should have context errors in RunResult.Errors
		if result == nil {
			rt.Fatalf("Run returned nil result and nil error")
		}

		// Every site should have reported a context cancellation error
		errorSites := make(map[string]bool)
		for _, se := range result.Errors {
			errorSites[se.SiteName] = true
		}
		for _, site := range sites {
			if !errorSites[site.Name] {
				rt.Fatalf("site %q missing from errors after context cancellation", site.Name)
			}
		}
	})
}

// Feature: class-action-scraper, Property 9: Exit code correctness
// **Validates: Requirements 4.4, 4.5**
func TestProperty9_ExitCodeCorrectness(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		repo, err := storage.NewSQLiteRepository(":memory:")
		if err != nil {
			rt.Fatalf("NewSQLiteRepository: %v", err)
		}
		defer repo.Close()

		// Decide whether this is a success run or a fatal failure run
		isFatal := rapid.Bool().Draw(rt, "isFatal")

		if isFatal {
			// Fatal case: no sites configured → Run returns non-nil error
			engine := NewEngine(repo, discardLogger(), 3)
			engine.GetSites = func() []config.TargetSite { return nil }

			_, err := engine.Run(context.Background())
			if err == nil {
				rt.Fatalf("expected non-nil error for fatal failure (no sites), got nil")
			}
		} else {
			// Success case: generate 1-5 sites, some may have per-site errors
			numSites := rapid.IntRange(1, 5).Draw(rt, "numSites")
			sites := make([]config.TargetSite, numSites)
			for i := 0; i < numSites; i++ {
				sites[i] = genSite(rt, i)
			}

			// Randomly fail some sites (non-fatal per-site errors)
			failSet := make(map[int]bool)
			for i := 0; i < numSites; i++ {
				if rapid.Bool().Draw(rt, fmt.Sprintf("siteFail_%d", i)) {
					failSet[i] = true
				}
			}

			mockCrawl := func(ctx context.Context, site config.TargetSite, policy *compliance.Policy) ([]model.LawsuitRecord, error) {
				for i, s := range sites {
					if s.Name == site.Name && failSet[i] {
						return nil, fmt.Errorf("simulated error for %s", site.Name)
					}
				}
				return nil, nil
			}

			engine := NewEngine(repo, discardLogger(), numSites)
			engine.GetSites = func() []config.TargetSite { return sites }
			engine.NewPolicy = func(ctx context.Context, site config.TargetSite) (*compliance.Policy, error) {
				return testPolicy()
			}
			engine.CollyCrawl = mockCrawl
			engine.HeadlessCrawl = mockCrawl

			result, err := engine.Run(context.Background())
			// Per-site failures are NOT fatal — Run returns nil error
			if err != nil {
				rt.Fatalf("expected nil error for non-fatal run, got: %v", err)
			}
			if result == nil {
				rt.Fatalf("expected non-nil RunResult for successful run")
			}

			// Verify per-site errors are captured in RunResult.Errors
			errorCount := len(result.Errors)
			if errorCount != len(failSet) {
				rt.Fatalf("expected %d site errors, got %d", len(failSet), errorCount)
			}
		}
	})
}
