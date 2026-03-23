package headless

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"

	"classact/internal/compliance"
	"classact/internal/config"
	"classact/internal/model"
	"classact/internal/parser"
)

// Crawl renders a target site in a headless browser and extracts lawsuit records
// from the rendered DOM. Waits for DOM stability before extraction.
// Respects robots.txt and rate limits via the provided compliance.Policy.
func Crawl(ctx context.Context, site config.TargetSite, policy *compliance.Policy) ([]model.LawsuitRecord, error) {
	// Parse the site URL for compliance checks and URL resolution.
	siteURL, err := url.Parse(site.URL)
	if err != nil {
		return nil, fmt.Errorf("headless: invalid site URL %q: %w", site.URL, err)
	}

	// Check robots.txt before navigating.
	if !policy.IsAllowed(siteURL.Path) {
		return nil, fmt.Errorf("headless: path %q disallowed by robots.txt for %s", siteURL.Path, site.Name)
	}

	// Wait for rate limiter.
	if err := policy.Wait(ctx); err != nil {
		return nil, fmt.Errorf("headless: rate limit wait for %s: %w", site.Name, err)
	}

	// Launch headless browser.
	launchURL, err := launcher.New().Headless(true).Launch()
	if err != nil {
		return nil, fmt.Errorf("headless: browser launch failed for %s: %w", site.Name, err)
	}

	browser := rod.New().ControlURL(launchURL)
	if err := browser.Connect(); err != nil {
		return nil, fmt.Errorf("headless: browser connect failed for %s: %w", site.Name, err)
	}
	defer browser.MustClose()

	// Create a page with context for cancellation/timeout.
	page, err := browser.Page(proto.TargetCreateTarget{URL: ""})
	if err != nil {
		return nil, fmt.Errorf("headless: page creation failed for %s: %w", site.Name, err)
	}
	defer page.MustClose()

	// Set User-Agent.
	if err := page.SetUserAgent(&proto.NetworkSetUserAgentOverride{
		UserAgent: policy.UserAgent(),
	}); err != nil {
		return nil, fmt.Errorf("headless: setting user-agent for %s: %w", site.Name, err)
	}

	// Navigate to the target URL.
	if err := page.Context(ctx).Navigate(site.URL); err != nil {
		return nil, fmt.Errorf("headless: navigation to %s failed: %w", site.URL, err)
	}

	// Wait for page load and DOM stability.
	if err := page.Context(ctx).WaitLoad(); err != nil {
		return nil, fmt.Errorf("headless: waiting for page load at %s: %w", site.URL, err)
	}
	if err := page.Context(ctx).WaitStable(0); err != nil {
		return nil, fmt.Errorf("headless: waiting for DOM stability at %s: %w", site.URL, err)
	}

	// Check context after waiting.
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Get the rendered HTML.
	html, err := page.HTML()
	if err != nil {
		return nil, fmt.Errorf("headless: getting rendered HTML from %s: %w", site.URL, err)
	}

	// Parse the rendered HTML with goquery.
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("headless: parsing rendered HTML from %s: %w", site.URL, err)
	}

	// Extract records using the shared parser.
	records, err := parser.ExtractRecords(doc, site)
	if err != nil {
		return nil, fmt.Errorf("headless: extracting records from %s: %w", site.URL, err)
	}

	// Resolve relative URLs to absolute.
	for i := range records {
		records[i].SourceURL = resolveURL(siteURL, records[i].SourceURL)
	}

	return records, nil
}

// resolveURL resolves a possibly-relative href against the base site URL.
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
