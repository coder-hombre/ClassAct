package parser_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"classact/internal/config"
	"classact/internal/parser"
	"classact/internal/storage"

	"github.com/PuerkitoBio/goquery"
	"pgregory.net/rapid"
)

// Feature: class-action-scraper, Property 3: HTML parsing extracts all required fields (round trip)
// **Validates: Requirements 2.2, 8.2**
func TestProperty3_HTMLParsingRoundTrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate random lawsuit data
		numItems := rapid.IntRange(1, 5).Draw(rt, "numItems")

		type lawsuitInput struct {
			title       string
			description string
			company     string
			filingDate  time.Time
			deadline    time.Time
			status      string
			sourceURL   string
		}

		items := make([]lawsuitInput, numItems)
		for i := 0; i < numItems; i++ {
			title := rapid.StringMatching(`[A-Za-z]{3,10}( [A-Za-z]{3,10}){1,3}`).Draw(rt, fmt.Sprintf("title_%d", i))
			description := rapid.StringMatching(`[A-Za-z]{3,8}( [A-Za-z]{3,8}){2,6}`).Draw(rt, fmt.Sprintf("desc_%d", i))
			companyRaw := rapid.StringMatching(`[A-Za-z]{3,10}( [A-Za-z]{3,10}){0,2}`).Draw(rt, fmt.Sprintf("company_%d", i))
			status := rapid.SampledFrom([]string{"open", "closed", "settled"}).Draw(rt, fmt.Sprintf("status_%d", i))

			filingDate := time.Date(
				rapid.IntRange(2020, 2025).Draw(rt, fmt.Sprintf("fdYear_%d", i)),
				time.Month(rapid.IntRange(1, 12).Draw(rt, fmt.Sprintf("fdMonth_%d", i))),
				rapid.IntRange(1, 28).Draw(rt, fmt.Sprintf("fdDay_%d", i)),
				0, 0, 0, 0, time.UTC,
			)
			deadline := time.Date(
				rapid.IntRange(2025, 2030).Draw(rt, fmt.Sprintf("dlYear_%d", i)),
				time.Month(rapid.IntRange(1, 12).Draw(rt, fmt.Sprintf("dlMonth_%d", i))),
				rapid.IntRange(1, 28).Draw(rt, fmt.Sprintf("dlDay_%d", i)),
				0, 0, 0, 0, time.UTC,
			)

			sourceURL := fmt.Sprintf("https://example.com/case/%d/%s", i,
				rapid.StringMatching(`[a-z]{4,10}`).Draw(rt, fmt.Sprintf("urlSlug_%d", i)))

			items[i] = lawsuitInput{
				title:       title,
				description: description,
				company:     companyRaw,
				filingDate:  filingDate,
				deadline:    deadline,
				status:      status,
				sourceURL:   sourceURL,
			}
		}

		// Build HTML document with the generated data using the test site's CSS selectors
		var htmlBuilder strings.Builder
		htmlBuilder.WriteString("<html><body>")
		for _, item := range items {
			htmlBuilder.WriteString(`<div class="lawsuit-item">`)
			htmlBuilder.WriteString(fmt.Sprintf(`<h3 class="title"><a href="%s">%s</a></h3>`,
				item.sourceURL, item.title))
			htmlBuilder.WriteString(fmt.Sprintf(`<p class="desc">%s</p>`, item.description))
			htmlBuilder.WriteString(fmt.Sprintf(`<span class="company">%s</span>`, item.company))
			htmlBuilder.WriteString(fmt.Sprintf(`<span class="date">%s</span>`, item.filingDate.Format("2006-01-02")))
			htmlBuilder.WriteString(fmt.Sprintf(`<span class="deadline">%s</span>`, item.deadline.Format("2006-01-02")))
			htmlBuilder.WriteString(fmt.Sprintf(`<span class="status">%s</span>`, item.status))
			htmlBuilder.WriteString("</div>")
		}
		htmlBuilder.WriteString("</body></html>")

		// Parse the HTML using ExtractRecords
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlBuilder.String()))
		if err != nil {
			rt.Fatalf("goquery.NewDocumentFromReader: %v", err)
		}

		site := config.TargetSite{
			Name: "test-site",
			URL:  "https://example.com",
			Selectors: config.SiteSelectors{
				ListingContainer: "div.lawsuit-item",
				Title:            "h3.title a",
				Description:      "p.desc",
				CompanyName:      "span.company",
				FilingDate:       "span.date",
				Deadline:         "span.deadline",
				Status:           "span.status",
				DetailLink:       "h3.title a",
			},
		}

		records, err := parser.ExtractRecords(doc, site)
		if err != nil {
			rt.Fatalf("ExtractRecords: %v", err)
		}

		if len(records) != numItems {
			rt.Fatalf("ExtractRecords returned %d records, expected %d", len(records), numItems)
		}

		// Store parsed records in an in-memory SQLite database
		repo, err := storage.NewSQLiteRepository(":memory:")
		if err != nil {
			rt.Fatalf("NewSQLiteRepository: %v", err)
		}
		defer repo.Close()

		ctx := context.Background()
		inserted, _, err := repo.UpsertLawsuits(ctx, records)
		if err != nil {
			rt.Fatalf("UpsertLawsuits: %v", err)
		}
		if inserted != numItems {
			rt.Fatalf("UpsertLawsuits inserted %d, expected %d", inserted, numItems)
		}

		// Retrieve records from DB
		retrieved, err := repo.ListLawsuits(ctx, storage.LawsuitFilter{})
		if err != nil {
			rt.Fatalf("ListLawsuits: %v", err)
		}
		if len(retrieved) != numItems {
			rt.Fatalf("ListLawsuits returned %d records, expected %d", len(retrieved), numItems)
		}

		// Build lookup by source URL for comparison
		byURL := make(map[string]int, len(retrieved))
		for idx, rec := range retrieved {
			byURL[rec.SourceURL] = idx
		}

		// Compare all fields between original generated data and retrieved records
		for i, item := range items {
			idx, ok := byURL[item.sourceURL]
			if !ok {
				rt.Fatalf("item %d: source URL %q not found in retrieved records", i, item.sourceURL)
			}
			got := retrieved[idx]

			// Title
			if got.Title != item.title {
				rt.Errorf("item %d: title = %q, want %q", i, got.Title, item.title)
			}

			// Description
			if got.Description != item.description {
				rt.Errorf("item %d: description = %q, want %q", i, got.Description, item.description)
			}

			// Company — parser normalizes via NormalizeCompanyName (title case, whitespace collapsed)
			expectedCompany := parser.NormalizeCompanyName(item.company)
			if got.Company != expectedCompany {
				rt.Errorf("item %d: company = %q, want %q", i, got.Company, expectedCompany)
			}

			// SourceURL
			if got.SourceURL != item.sourceURL {
				rt.Errorf("item %d: sourceURL = %q, want %q", i, got.SourceURL, item.sourceURL)
			}

			// Status — parser lowercases the status
			if got.Status != strings.ToLower(item.status) {
				rt.Errorf("item %d: status = %q, want %q", i, got.Status, strings.ToLower(item.status))
			}

			// Filing date — stored as ISO 8601 in SQLite, truncated to date precision
			if got.FilingDate == nil {
				rt.Errorf("item %d: filingDate is nil, want %v", i, item.filingDate)
			} else if !got.FilingDate.Equal(item.filingDate) {
				rt.Errorf("item %d: filingDate = %v, want %v", i, *got.FilingDate, item.filingDate)
			}

			// Deadline
			if got.Deadline == nil {
				rt.Errorf("item %d: deadline is nil, want %v", i, item.deadline)
			} else if !got.Deadline.Equal(item.deadline) {
				rt.Errorf("item %d: deadline = %v, want %v", i, *got.Deadline, item.deadline)
			}
		}
	})
}
