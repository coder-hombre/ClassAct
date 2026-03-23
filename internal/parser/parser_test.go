package parser

import (
	"strings"
	"testing"
	"time"

	"classact/internal/config"

	"github.com/PuerkitoBio/goquery"
)

// testSite returns a TargetSite with selectors matching the test HTML fixtures.
func testSite() config.TargetSite {
	return config.TargetSite{
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
}

func docFromHTML(t *testing.T, html string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("failed to parse HTML: %v", err)
	}
	return doc
}

func TestExtractRecords_FullRecord(t *testing.T) {
	html := `<html><body>
		<div class="lawsuit-item">
			<h3 class="title"><a href="https://example.com/case/1">Big Lawsuit</a></h3>
			<p class="desc">A description of the lawsuit.</p>
			<span class="company">  acme  corp  </span>
			<span class="date">January 15, 2024</span>
			<span class="deadline">2024-06-30</span>
			<span class="status">Open</span>
		</div>
	</body></html>`

	doc := docFromHTML(t, html)
	records, err := ExtractRecords(doc, testSite())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	r := records[0]
	if r.ID == "" {
		t.Error("expected non-empty ID")
	}
	if r.Title != "Big Lawsuit" {
		t.Errorf("title = %q, want %q", r.Title, "Big Lawsuit")
	}
	if r.SourceURL != "https://example.com/case/1" {
		t.Errorf("sourceURL = %q, want %q", r.SourceURL, "https://example.com/case/1")
	}
	if r.Description != "A description of the lawsuit." {
		t.Errorf("description = %q, want %q", r.Description, "A description of the lawsuit.")
	}
	if r.Company != "Acme Corp" {
		t.Errorf("company = %q, want %q", r.Company, "Acme Corp")
	}
	if r.FilingDate == nil {
		t.Fatal("expected non-nil FilingDate")
	}
	wantFiling := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	if !r.FilingDate.Equal(wantFiling) {
		t.Errorf("filingDate = %v, want %v", r.FilingDate, wantFiling)
	}
	if r.Deadline == nil {
		t.Fatal("expected non-nil Deadline")
	}
	wantDeadline := time.Date(2024, 6, 30, 0, 0, 0, 0, time.UTC)
	if !r.Deadline.Equal(wantDeadline) {
		t.Errorf("deadline = %v, want %v", r.Deadline, wantDeadline)
	}
	if r.Status != "open" {
		t.Errorf("status = %q, want %q", r.Status, "open")
	}
}

func TestExtractRecords_EmptyHTML(t *testing.T) {
	doc := docFromHTML(t, "<html><body></body></html>")
	records, err := ExtractRecords(doc, testSite())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records, got %d", len(records))
	}
}

func TestExtractRecords_MissingTitle_Skipped(t *testing.T) {
	html := `<html><body>
		<div class="lawsuit-item">
			<h3 class="title"><a href="https://example.com/case/1"></a></h3>
			<span class="company">Acme</span>
		</div>
	</body></html>`

	doc := docFromHTML(t, html)
	records, err := ExtractRecords(doc, testSite())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records (missing title), got %d", len(records))
	}
}

func TestExtractRecords_MissingLink_Skipped(t *testing.T) {
	html := `<html><body>
		<div class="lawsuit-item">
			<h3 class="title">No Link Here</h3>
			<span class="company">Acme</span>
		</div>
	</body></html>`

	doc := docFromHTML(t, html)
	records, err := ExtractRecords(doc, testSite())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records (missing link), got %d", len(records))
	}
}

func TestExtractRecords_PartialRecord_MissingOptionalFields(t *testing.T) {
	html := `<html><body>
		<div class="lawsuit-item">
			<h3 class="title"><a href="https://example.com/case/2">Partial Case</a></h3>
		</div>
	</body></html>`

	doc := docFromHTML(t, html)
	records, err := ExtractRecords(doc, testSite())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	r := records[0]
	if r.Title != "Partial Case" {
		t.Errorf("title = %q, want %q", r.Title, "Partial Case")
	}
	if r.Description != "" {
		t.Errorf("description = %q, want empty", r.Description)
	}
	if r.Company != "" {
		t.Errorf("company = %q, want empty", r.Company)
	}
	if r.FilingDate != nil {
		t.Errorf("filingDate = %v, want nil", r.FilingDate)
	}
	if r.Deadline != nil {
		t.Errorf("deadline = %v, want nil", r.Deadline)
	}
	if r.Status != "open" {
		t.Errorf("status = %q, want %q", r.Status, "open")
	}
}

func TestExtractRecords_MultipleRecords(t *testing.T) {
	html := `<html><body>
		<div class="lawsuit-item">
			<h3 class="title"><a href="https://example.com/1">Case One</a></h3>
			<span class="company">Alpha Inc</span>
		</div>
		<div class="lawsuit-item">
			<h3 class="title"><a href="https://example.com/2">Case Two</a></h3>
			<span class="company">Beta LLC</span>
		</div>
		<div class="lawsuit-item">
			<h3 class="title"><a href="https://example.com/3">Case Three</a></h3>
		</div>
	</body></html>`

	doc := docFromHTML(t, html)
	records, err := ExtractRecords(doc, testSite())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}
	if records[0].Title != "Case One" {
		t.Errorf("records[0].Title = %q, want %q", records[0].Title, "Case One")
	}
	if records[1].Company != "Beta LLC" {
		t.Errorf("records[1].Company = %q, want %q", records[1].Company, "Beta LLC")
	}
	if records[2].Company != "" {
		t.Errorf("records[2].Company = %q, want empty", records[2].Company)
	}
}

func TestExtractRecords_MalformedHTML_NoPanic(t *testing.T) {
	html := `<html><body>
		<div class="lawsuit-item">
			<h3 class="title"><a href="https://example.com/x">Broken
			<span class="company">Test</span>
		</div>
		<div class="lawsuit-item"
	</body></html>`

	doc := docFromHTML(t, html)
	// Should not panic.
	_, err := ExtractRecords(doc, testSite())
	if err != nil {
		t.Fatalf("unexpected error on malformed HTML: %v", err)
	}
}

func TestNormalizeCompanyName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"  acme  corp  ", "Acme Corp"},
		{"APPLE INC", "APPLE INC"},
		{"google", "Google"},
		{"  ", ""},
		{"", ""},
		{"a", "A"},
		{"hello   world   test", "Hello World Test"},
		{"\t  spaced\tout  \n", "Spaced Out"},
	}
	for _, tc := range tests {
		got := NormalizeCompanyName(tc.input)
		if got != tc.want {
			t.Errorf("NormalizeCompanyName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestParseDate(t *testing.T) {
	tests := []struct {
		input string
		want  *time.Time
	}{
		{"January 15, 2024", timePtr(time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC))},
		{"2024-06-30", timePtr(time.Date(2024, 6, 30, 0, 0, 0, 0, time.UTC))},
		{"01/15/2024", timePtr(time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC))},
		{"Jan 2, 2006", timePtr(time.Date(2006, 1, 2, 0, 0, 0, 0, time.UTC))},
		{"not a date", nil},
		{"", nil},
		{"  ", nil},
	}
	for _, tc := range tests {
		got := parseDate(tc.input)
		if tc.want == nil {
			if got != nil {
				t.Errorf("parseDate(%q) = %v, want nil", tc.input, got)
			}
		} else {
			if got == nil {
				t.Errorf("parseDate(%q) = nil, want %v", tc.input, tc.want)
			} else if !got.Equal(*tc.want) {
				t.Errorf("parseDate(%q) = %v, want %v", tc.input, got, tc.want)
			}
		}
	}
}

func timePtr(t time.Time) *time.Time { return &t }
