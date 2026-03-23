package parser

import (
	"strings"
	"time"

	"classact/internal/config"
	"classact/internal/model"

	"github.com/PuerkitoBio/goquery"
	"github.com/google/uuid"
)

// Common date formats to try when parsing filing dates and deadlines.
var dateFormats = []string{
	"January 2, 2006",
	"Jan 2, 2006",
	"2006-01-02",
	"01/02/2006",
	"1/2/2006",
	"02-01-2006",
	"2 January 2006",
	"2 Jan 2006",
	"January 2006",
	"Jan 2006",
	"2006-01-02T15:04:05Z",
	"2006-01-02T15:04:05-07:00",
}

// ExtractRecords parses an HTML document using the provided site selectors and
// returns a slice of LawsuitRecords. Works on both raw HTML (Colly) and
// rendered DOM (Go-Rod). Records missing Title or SourceURL are skipped.
func ExtractRecords(doc *goquery.Document, site config.TargetSite) ([]model.LawsuitRecord, error) {
	var records []model.LawsuitRecord
	sel := site.Selectors

	doc.Find(sel.ListingContainer).Each(func(_ int, container *goquery.Selection) {
		title := strings.TrimSpace(container.Find(sel.Title).First().Text())

		sourceURL := ""
		if sel.DetailLink != "" {
			if href, exists := container.Find(sel.DetailLink).First().Attr("href"); exists {
				sourceURL = strings.TrimSpace(href)
			}
		}

		// Title and SourceURL are minimum required fields.
		if title == "" || sourceURL == "" {
			return
		}

		now := time.Now()
		rec := model.LawsuitRecord{
			ID:        uuid.New().String(),
			Title:     title,
			SourceURL: sourceURL,
			Status:    "open",
			CreatedAt: now,
			UpdatedAt: now,
		}

		// Description (optional).
		if sel.Description != "" {
			rec.Description = strings.TrimSpace(container.Find(sel.Description).First().Text())
		}

		// Company name (optional, normalized).
		if sel.CompanyName != "" {
			raw := strings.TrimSpace(container.Find(sel.CompanyName).First().Text())
			if raw != "" {
				rec.Company = NormalizeCompanyName(raw)
			}
		}

		// Filing date (optional).
		if sel.FilingDate != "" {
			raw := strings.TrimSpace(container.Find(sel.FilingDate).First().Text())
			if t := parseDate(raw); t != nil {
				rec.FilingDate = t
			}
		}

		// Deadline (optional).
		if sel.Deadline != "" {
			raw := strings.TrimSpace(container.Find(sel.Deadline).First().Text())
			if t := parseDate(raw); t != nil {
				rec.Deadline = t
			}
		}

		// Status (optional, default "open").
		if sel.Status != "" {
			raw := strings.TrimSpace(container.Find(sel.Status).First().Text())
			if raw != "" {
				rec.Status = strings.ToLower(raw)
			}
		}

		records = append(records, rec)
	})

	return records, nil
}

// NormalizeCompanyName trims whitespace, collapses multiple spaces, and applies
// title casing for consistent filtering and deduplication.
func NormalizeCompanyName(name string) string {
	name = strings.TrimSpace(name)
	// Collapse multiple spaces into one.
	words := strings.Fields(name)
	if len(words) == 0 {
		return ""
	}
	name = strings.Join(words, " ")
	return strings.Title(name) //nolint:staticcheck // strings.Title is fine for simple title casing
}

// parseDate tries common date formats and returns the first successful parse,
// or nil if none match.
func parseDate(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	for _, fmt := range dateFormats {
		if t, err := time.Parse(fmt, s); err == nil {
			return &t
		}
	}
	return nil
}
