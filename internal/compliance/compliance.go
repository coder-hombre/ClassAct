package compliance

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/temoto/robotstxt"
	"golang.org/x/time/rate"

	"classact/internal/config"
)

const defaultUserAgent = "ClassAct/1.0 (class action lawsuit tracker; +https://github.com/classact)"

// Policy encapsulates legal compliance rules for a target site.
type Policy struct {
	robotsData *robotstxt.RobotsData
	limiter    *rate.Limiter
	userAgent  string
}

// NewPolicy fetches robots.txt for the site and creates a rate limiter.
// Returns an error if robots.txt cannot be fetched or parsed (conservative default: skip site).
func NewPolicy(ctx context.Context, site config.TargetSite) (*Policy, error) {
	robotsURL, err := robotsTxtURL(site.URL)
	if err != nil {
		return nil, fmt.Errorf("compliance: invalid site URL %q: %w", site.URL, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, robotsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("compliance: creating robots.txt request: %w", err)
	}
	req.Header.Set("User-Agent", defaultUserAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("compliance: fetching robots.txt from %s: %w", robotsURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("compliance: reading robots.txt body: %w", err)
	}

	robots, err := robotstxt.FromBytes(body)
	if err != nil {
		return nil, fmt.Errorf("compliance: parsing robots.txt: %w", err)
	}

	return newPolicyFromParsed(robots, site.RateLimit), nil
}

// NewPolicyFromRobots creates a Policy from raw robots.txt content.
// Intended for testing — avoids HTTP calls.
func NewPolicyFromRobots(robotsTxtContent string, rateLimit time.Duration) (*Policy, error) {
	robots, err := robotstxt.FromString(robotsTxtContent)
	if err != nil {
		return nil, fmt.Errorf("compliance: parsing robots.txt: %w", err)
	}
	return newPolicyFromParsed(robots, rateLimit), nil
}

// IsAllowed checks if a URL path is permitted by robots.txt.
func (p *Policy) IsAllowed(path string) bool {
	group := p.robotsData.FindGroup(p.userAgent)
	if group == nil {
		return true
	}
	return group.Test(path)
}

// Wait blocks until the rate limiter allows the next request.
// Respects context cancellation.
func (p *Policy) Wait(ctx context.Context) error {
	return p.limiter.Wait(ctx)
}

// UserAgent returns the configured User-Agent string.
func (p *Policy) UserAgent() string {
	return p.userAgent
}

// newPolicyFromParsed builds a Policy from already-parsed robots data and a rate limit duration.
func newPolicyFromParsed(robots *robotstxt.RobotsData, rateLimit time.Duration) *Policy {
	var lim *rate.Limiter
	if rateLimit > 0 {
		lim = rate.NewLimiter(rate.Every(rateLimit), 1)
	} else {
		// No rate limit — allow unlimited requests.
		lim = rate.NewLimiter(rate.Inf, 1)
	}
	return &Policy{
		robotsData: robots,
		limiter:    lim,
		userAgent:  defaultUserAgent,
	}
}

// robotsTxtURL derives the robots.txt URL from a site's base URL.
func robotsTxtURL(siteURL string) (string, error) {
	u, err := url.Parse(siteURL)
	if err != nil {
		return "", err
	}
	u.Path = "/robots.txt"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}
