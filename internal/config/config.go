package config

import "time"

// TargetSite defines a scraping target.
type TargetSite struct {
	Name        string
	URL         string
	UseHeadless bool          // true = Go-Rod, false = Colly
	RateLimit   time.Duration // minimum delay between requests to this domain
	Selectors   SiteSelectors // CSS selectors for extracting fields
}

// SiteSelectors holds CSS selectors for goquery field extraction.
type SiteSelectors struct {
	ListingContainer string
	Title            string
	Description      string
	CompanyName      string
	FilingDate       string
	Deadline         string
	Status           string
	DetailLink       string
}

// Sites returns all configured target sites.
func Sites() []TargetSite {
	return []TargetSite{
		{
			Name:        "classaction.org",
			URL:         "https://www.classaction.org/lawsuits",
			UseHeadless: false,
			RateLimit:   2 * time.Second,
			Selectors: SiteSelectors{
				ListingContainer: "div.lawsuit-item",
				Title:            "h3.lawsuit-title a",
				Description:      "div.lawsuit-description",
				CompanyName:      "span.lawsuit-company",
				FilingDate:       "span.lawsuit-date",
				Deadline:         "span.lawsuit-deadline",
				Status:           "span.lawsuit-status",
				DetailLink:       "h3.lawsuit-title a",
			},
		},
		{
			Name:        "topclassactions.com",
			URL:         "https://topclassactions.com/lawsuit-settlements/open-lawsuit-settlements/",
			UseHeadless: false,
			RateLimit:   2 * time.Second,
			Selectors: SiteSelectors{
				ListingContainer: "article.post",
				Title:            "h2.entry-title a",
				Description:      "div.entry-content p",
				CompanyName:      "span.company-name",
				FilingDate:       "time.entry-date",
				Deadline:         "span.deadline-date",
				Status:           "span.settlement-status",
				DetailLink:       "h2.entry-title a",
			},
		},
		{
			Name:        "FTC Refund Database",
			URL:         "https://www.ftc.gov/enforcement/refunds",
			UseHeadless: true,
			RateLimit:   3 * time.Second,
			Selectors: SiteSelectors{
				ListingContainer: "div.views-row",
				Title:            "h3.field-title a",
				Description:      "div.field-body",
				CompanyName:      "span.field-company",
				FilingDate:       "span.date-display-single",
				Deadline:         "span.field-deadline",
				Status:           "span.field-status",
				DetailLink:       "h3.field-title a",
			},
		},
	}
}
