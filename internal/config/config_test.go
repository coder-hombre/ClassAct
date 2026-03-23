package config

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// Feature: class-action-scraper, Property 1: Config structure validity
// **Validates: Requirements 1.1, 10.1**
func TestProperty1_ConfigStructureValidity(t *testing.T) {
	// Part 1: Validate all actual Sites() entries
	t.Run("actual_sites", func(t *testing.T) {
		sites := Sites()
		if len(sites) == 0 {
			t.Fatal("Sites() returned no sites")
		}
		for _, site := range sites {
			if errs := assertSiteValid(site); len(errs) > 0 {
				t.Errorf("site %q invalid: %s", site.Name, strings.Join(errs, "; "))
			}
		}
	})

	// Part 2: Property test — random TargetSite configs must satisfy the same constraints
	t.Run("random_configs", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) {
			site := drawValidTargetSite(rt)
			if errs := assertSiteValid(site); len(errs) > 0 {
				rt.Fatalf("generated site %q invalid: %s", site.Name, strings.Join(errs, "; "))
			}
		})
	})
}

// assertSiteValid checks that a TargetSite has non-empty Name, valid URL,
// positive RateLimit, non-empty CSS selectors, and a defined UseHeadless flag.
// Returns a list of validation errors (empty if valid).
func assertSiteValid(site TargetSite) []string {
	var errs []string

	// Non-empty Name
	if strings.TrimSpace(site.Name) == "" {
		errs = append(errs, "site Name is empty")
	}

	// Valid URL starting with http:// or https://
	parsed, err := url.Parse(site.URL)
	if err != nil {
		errs = append(errs, "site URL is not valid: "+err.Error())
	} else {
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			errs = append(errs, "site URL scheme must be http or https, got "+parsed.Scheme)
		}
		if parsed.Host == "" {
			errs = append(errs, "site URL has empty host")
		}
	}

	// Positive RateLimit
	if site.RateLimit <= 0 {
		errs = append(errs, "site RateLimit must be positive")
	}

	// Non-empty CSS selectors (at minimum ListingContainer, Title, and DetailLink)
	if site.Selectors.ListingContainer == "" {
		errs = append(errs, "Selectors.ListingContainer is empty")
	}
	if site.Selectors.Title == "" {
		errs = append(errs, "Selectors.Title is empty")
	}
	if site.Selectors.DetailLink == "" {
		errs = append(errs, "Selectors.DetailLink is empty")
	}

	// UseHeadless is a bool — always defined in Go (compile-time guarantee).
	_ = site.UseHeadless

	return errs
}

// drawValidTargetSite generates a random but valid TargetSite using rapid.
func drawValidTargetSite(rt *rapid.T) TargetSite {
	scheme := rapid.SampledFrom([]string{"http", "https"}).Draw(rt, "scheme")
	host := rapid.StringMatching(`[a-z]{3,12}\.(com|org|net|gov)`).Draw(rt, "host")
	path := rapid.StringMatching(`/[a-z]{2,8}(/[a-z]{2,8})?`).Draw(rt, "path")

	rateLimitMs := rapid.IntRange(100, 10000).Draw(rt, "rateLimitMs")

	return TargetSite{
		Name:        rapid.StringMatching(`[A-Za-z][A-Za-z0-9 ]{2,25}`).Draw(rt, "name"),
		URL:         scheme + "://" + host + path,
		UseHeadless: rapid.Bool().Draw(rt, "useHeadless"),
		RateLimit:   time.Duration(rateLimitMs) * time.Millisecond,
		Selectors: SiteSelectors{
			ListingContainer: rapid.StringMatching(`[a-z]{1,5}\.[a-z-]{3,15}`).Draw(rt, "listingContainer"),
			Title:            rapid.StringMatching(`[a-z]{1,5}\.[a-z-]{3,15}`).Draw(rt, "title"),
			Description:      rapid.StringMatching(`[a-z]{1,5}\.[a-z-]{3,15}`).Draw(rt, "description"),
			CompanyName:      rapid.StringMatching(`[a-z]{1,5}\.[a-z-]{3,15}`).Draw(rt, "companyName"),
			FilingDate:       rapid.StringMatching(`[a-z]{1,5}\.[a-z-]{3,15}`).Draw(rt, "filingDate"),
			Deadline:         rapid.StringMatching(`[a-z]{1,5}\.[a-z-]{3,15}`).Draw(rt, "deadline"),
			Status:           rapid.StringMatching(`[a-z]{1,5}\.[a-z-]{3,15}`).Draw(rt, "status"),
			DetailLink:       rapid.StringMatching(`[a-z]{1,5}\.[a-z-]{3,15}`).Draw(rt, "detailLink"),
		},
	}
}

// TestKnownSitesExist verifies the three known target sites are present in Sites().
func TestKnownSitesExist(t *testing.T) {
	sites := Sites()

	expected := map[string]bool{
		"classaction.org":    false,
		"topclassactions.com": false,
		"FTC Refund Database": false,
	}

	for _, site := range sites {
		if _, ok := expected[site.Name]; ok {
			expected[site.Name] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("expected site %q not found in Sites()", name)
		}
	}
}
