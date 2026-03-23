package crawler

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"sync"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly/v2"

	"classact/internal/compliance"
	"classact/internal/config"
	"classact/internal/model"
	"classact/internal/parser"
)

// Crawl fetches and parses lawsuit listings from a target site using Colly.
// Returns parsed LawsuitRecords. Respects robots.txt and rate limits via the
// provided compliance.Policy.
func Crawl(ctx context.Context, site config.TargetSite, policy *compliance.Policy) ([]model.LawsuitRecord, error) {
	var (
		mu      sync.Mutex
		records []model.LawsuitRecord
		crawlErr error
	)

	c := colly.NewCollector(
		colly.UserAgent(policy.UserAgent()),
	)

	// Before each request: check context, enforce robots.txt, apply rate limit.
	c.OnRequest(func(r *colly.Request) {
		// Check context cancellation.
		if ctx.Err() != nil {
			r.Abort()
			return
		}

		// Check robots.txt compliance.
		reqURL := r.URL
		if !policy.IsAllowed(reqURL.Path) {
			r.Abort()
			return
		}

		// Wait for rate limiter (respects context cancellation).
		if err := policy.Wait(ctx); err != nil {
			mu.Lock()
			crawlErr = fmt.Errorf("crawler: rate limit wait: %w", err)
			mu.Unlock()
			r.Abort()
			return
		}
	})

	// On HTML response: parse body and extract records.
	c.OnResponse(func(r *colly.Response) {
		if ctx.Err() != nil {
			return
		}

		doc, err := goquery.NewDocumentFromReader(bytes.NewReader(r.Body))
		if err != nil {
			mu.Lock()
			crawlErr = fmt.Errorf("crawler: parsing HTML from %s: %w", r.Request.URL, err)
			mu.Unlock()
			return
		}

		extracted, err := parser.ExtractRecords(doc, site)
		if err != nil {
			mu.Lock()
			crawlErr = fmt.Errorf("crawler: extracting records from %s: %w", r.Request.URL, err)
			mu.Unlock()
			return
		}

		// Resolve relative detail links to absolute URLs.
		for i := range extracted {
			extracted[i].SourceURL = resolveURL(r.Request.URL, extracted[i].SourceURL)
		}

		mu.Lock()
		records = append(records, extracted...)
		mu.Unlock()
	})

	c.OnError(func(r *colly.Response, err error) {
		mu.Lock()
		crawlErr = fmt.Errorf("crawler: request to %s failed: %w", r.Request.URL, err)
		mu.Unlock()
	})

	// Start the crawl.
	if err := c.Visit(site.URL); err != nil {
		return nil, fmt.Errorf("crawler: visiting %s: %w", site.URL, err)
	}

	c.Wait()

	// Check context one final time.
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if crawlErr != nil {
		return records, crawlErr
	}

	return records, nil
}

// resolveURL resolves a possibly-relative href against the base request URL.
func resolveURL(base *url.URL, href string) string {
	if href == "" {
		return ""
	}
	ref, err := url.Parse(href)
	if err != nil {
		return href
	}
	if ref.IsAbs() {
		return href
	}
	return base.ResolveReference(ref).String()
}
