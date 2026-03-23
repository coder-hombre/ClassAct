package scraper

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"classact/internal/compliance"
	"classact/internal/config"
	"classact/internal/crawler"
	"classact/internal/headless"
	"classact/internal/model"
	"classact/internal/storage"
)

// CrawlFunc is the signature for crawl functions (both Colly and headless).
type CrawlFunc func(ctx context.Context, site config.TargetSite, policy *compliance.Policy) ([]model.LawsuitRecord, error)

// SitesFunc is the signature for the function that returns configured sites.
type SitesFunc func() []config.TargetSite

// NewPolicyFunc is the signature for creating compliance policies.
type NewPolicyFunc func(ctx context.Context, site config.TargetSite) (*compliance.Policy, error)

// siteResult holds the outcome of crawling a single site.
type siteResult struct {
	site    config.TargetSite
	records []model.LawsuitRecord
	err     error
}

// Engine orchestrates a scraping run across all configured sites.
type Engine struct {
	store      storage.Repository
	logger     *slog.Logger
	maxWorkers int

	// Pluggable functions for testability.
	CollyCrawl   CrawlFunc
	HeadlessCrawl CrawlFunc
	GetSites     SitesFunc
	NewPolicy    NewPolicyFunc
}

// NewEngine creates a new Engine with real implementations as defaults.
func NewEngine(store storage.Repository, logger *slog.Logger, maxWorkers int) *Engine {
	if maxWorkers <= 0 {
		maxWorkers = 3
	}
	return &Engine{
		store:         store,
		logger:        logger,
		maxWorkers:    maxWorkers,
		CollyCrawl:    crawler.Crawl,
		HeadlessCrawl: headless.Crawl,
		GetSites:      config.Sites,
		NewPolicy:     compliance.NewPolicy,
	}
}

// Run executes a full scraping cycle across all configured sites.
// It returns a RunResult summarizing the run, or an error for fatal failures.
func (e *Engine) Run(ctx context.Context) (*model.RunResult, error) {
	startTime := time.Now()

	sites := e.GetSites()
	if len(sites) == 0 {
		return nil, fmt.Errorf("scraper: no sites configured")
	}

	e.logger.InfoContext(ctx, "scraping run started",
		slog.Time("start_time", startTime),
		slog.Int("site_count", len(sites)),
	)

	// Buffered channel acts as a semaphore for the worker pool.
	sem := make(chan struct{}, e.maxWorkers)

	// Results channel collects outcomes from each site goroutine.
	results := make(chan siteResult, len(sites))

	var wg sync.WaitGroup

	for _, site := range sites {
		wg.Add(1)
		go func(s config.TargetSite) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					e.logger.ErrorContext(ctx, "panic recovered in site goroutine",
						slog.String("site", s.Name),
						slog.String("url", s.URL),
						slog.Any("panic", r),
					)
					results <- siteResult{
						site: s,
						err:  fmt.Errorf("panic: %v", r),
					}
				}
			}()

			// Acquire worker slot.
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results <- siteResult{site: s, err: ctx.Err()}
				return
			}

			// Create compliance policy for this site.
			policy, err := e.NewPolicy(ctx, s)
			if err != nil {
				e.logger.ErrorContext(ctx, "compliance policy creation failed",
					slog.String("site", s.Name),
					slog.String("url", s.URL),
					slog.String("error", err.Error()),
				)
				results <- siteResult{site: s, err: fmt.Errorf("policy: %w", err)}
				return
			}

			// Dispatch to the appropriate crawler.
			var records []model.LawsuitRecord
			if s.UseHeadless {
				records, err = e.HeadlessCrawl(ctx, s, policy)
			} else {
				records, err = e.CollyCrawl(ctx, s, policy)
			}

			if err != nil {
				e.logger.ErrorContext(ctx, "crawl failed",
					slog.String("site", s.Name),
					slog.String("url", s.URL),
					slog.String("error", err.Error()),
				)
			} else {
				e.logger.InfoContext(ctx, "crawl completed",
					slog.String("site", s.Name),
					slog.Int("records_found", len(records)),
				)
			}

			results <- siteResult{site: s, records: records, err: err}
		}(site)
	}

	// Close results channel once all goroutines finish.
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results from all sites.
	var (
		allRecords []model.LawsuitRecord
		siteErrors []model.SiteError
	)

	for res := range results {
		if res.err != nil {
			siteErrors = append(siteErrors, model.SiteError{
				SiteName: res.site.Name,
				URL:      res.site.URL,
				Err:      res.err,
			})
		}
		if len(res.records) > 0 {
			allRecords = append(allRecords, res.records...)
		}
	}

	// Batch upsert all collected records to storage.
	var inserted, updated int
	if len(allRecords) > 0 {
		var err error
		inserted, updated, err = e.store.UpsertLawsuits(ctx, allRecords)
		if err != nil {
			e.logger.ErrorContext(ctx, "batch upsert failed",
				slog.String("error", err.Error()),
			)
			siteErrors = append(siteErrors, model.SiteError{
				SiteName: "storage",
				URL:      "",
				Err:      fmt.Errorf("upsert: %w", err),
			})
		}
	}

	endTime := time.Now()
	duration := endTime.Sub(startTime)

	runResult := &model.RunResult{
		ID:             uuid.New().String(),
		StartTime:      startTime,
		EndTime:        endTime,
		TotalFound:     len(allRecords),
		NewRecords:     inserted,
		UpdatedRecords: updated,
		Errors:         siteErrors,
	}

	// Persist the run result.
	if err := e.store.SaveRunResult(ctx, *runResult); err != nil {
		e.logger.ErrorContext(ctx, "failed to save run result",
			slog.String("error", err.Error()),
		)
	}

	e.logger.InfoContext(ctx, "scraping run completed",
		slog.Time("start_time", startTime),
		slog.Time("end_time", endTime),
		slog.Int("total_found", len(allRecords)),
		slog.Int("new_records", inserted),
		slog.Int("updated_records", updated),
		slog.Int("errors", len(siteErrors)),
		slog.Duration("duration", duration),
	)

	return runResult, nil
}
