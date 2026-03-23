package compliance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"classact/internal/config"
)

func TestNewPolicyFromRobots_AllowedPaths(t *testing.T) {
	robotsTxt := `User-agent: *
Disallow: /private/
Disallow: /admin/
Allow: /public/
`
	policy, err := NewPolicyFromRobots(robotsTxt, 1*time.Second)
	if err != nil {
		t.Fatalf("NewPolicyFromRobots: %v", err)
	}

	tests := []struct {
		path    string
		allowed bool
	}{
		{"/public/page", true},
		{"/public/", true},
		{"/", true},
		{"/open", true},
		{"/private/secret", false},
		{"/private/", false},
		{"/admin/dashboard", false},
		{"/admin/", false},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			got := policy.IsAllowed(tc.path)
			if got != tc.allowed {
				t.Errorf("IsAllowed(%q) = %v, want %v", tc.path, got, tc.allowed)
			}
		})
	}
}

func TestNewPolicyFromRobots_EmptyRobotsTxt(t *testing.T) {
	policy, err := NewPolicyFromRobots("", 1*time.Second)
	if err != nil {
		t.Fatalf("NewPolicyFromRobots: %v", err)
	}

	// Empty robots.txt means everything is allowed.
	if !policy.IsAllowed("/anything") {
		t.Error("expected all paths allowed with empty robots.txt")
	}
}

func TestNewPolicyFromRobots_UserAgentSpecificRules(t *testing.T) {
	robotsTxt := `User-agent: ClassAct/1.0 (class action lawsuit tracker; +https://github.com/classact)
Disallow: /blocked-for-classact/

User-agent: *
Disallow: /blocked-for-all/
`
	policy, err := NewPolicyFromRobots(robotsTxt, 1*time.Second)
	if err != nil {
		t.Fatalf("NewPolicyFromRobots: %v", err)
	}

	if policy.IsAllowed("/blocked-for-classact/page") {
		t.Error("expected /blocked-for-classact/page to be disallowed for our user agent")
	}
	// The ClassAct-specific group applies, so the wildcard group's rules don't.
	if !policy.IsAllowed("/blocked-for-all/page") {
		t.Error("expected /blocked-for-all/page to be allowed (our agent has its own group)")
	}
}

func TestUserAgent(t *testing.T) {
	policy, err := NewPolicyFromRobots("", 1*time.Second)
	if err != nil {
		t.Fatalf("NewPolicyFromRobots: %v", err)
	}

	ua := policy.UserAgent()
	if ua == "" {
		t.Fatal("UserAgent() returned empty string")
	}
	if ua != defaultUserAgent {
		t.Errorf("UserAgent() = %q, want %q", ua, defaultUserAgent)
	}
}

func TestWait_RespectsRateLimit(t *testing.T) {
	rateLimit := 100 * time.Millisecond
	policy, err := NewPolicyFromRobots("", rateLimit)
	if err != nil {
		t.Fatalf("NewPolicyFromRobots: %v", err)
	}

	ctx := context.Background()
	n := 4
	start := time.Now()
	for i := 0; i < n; i++ {
		if err := policy.Wait(ctx); err != nil {
			t.Fatalf("Wait: %v", err)
		}
	}
	elapsed := time.Since(start)

	// (n-1) intervals should have elapsed at minimum.
	minExpected := time.Duration(n-1) * rateLimit
	if elapsed < minExpected {
		t.Errorf("elapsed %v < expected minimum %v for %d requests at %v rate", elapsed, minExpected, n, rateLimit)
	}
}

func TestWait_RespectsContextCancellation(t *testing.T) {
	policy, err := NewPolicyFromRobots("", 10*time.Second)
	if err != nil {
		t.Fatalf("NewPolicyFromRobots: %v", err)
	}

	ctx := context.Background()
	// Consume the initial token.
	if err := policy.Wait(ctx); err != nil {
		t.Fatalf("first Wait: %v", err)
	}

	// Cancel context before next wait.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = policy.Wait(ctx)
	if err == nil {
		t.Fatal("expected error from Wait with cancelled context")
	}
}

func TestNewPolicy_FetchesRobotsTxt(t *testing.T) {
	robotsTxt := `User-agent: *
Disallow: /secret/
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.Write([]byte(robotsTxt))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	site := config.TargetSite{
		Name:      "test-site",
		URL:       srv.URL + "/lawsuits",
		RateLimit: 100 * time.Millisecond,
	}

	policy, err := NewPolicy(context.Background(), site)
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}

	if policy.IsAllowed("/secret/page") {
		t.Error("expected /secret/page to be disallowed")
	}
	if !policy.IsAllowed("/public/page") {
		t.Error("expected /public/page to be allowed")
	}
}

func TestNewPolicy_ErrorOnFetchFailure(t *testing.T) {
	// Point at a server that immediately closes connections.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "hijack not supported", http.StatusInternalServerError)
			return
		}
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer srv.Close()

	site := config.TargetSite{
		Name:      "broken-site",
		URL:       srv.URL,
		RateLimit: 1 * time.Second,
	}

	_, err := NewPolicy(context.Background(), site)
	if err == nil {
		t.Fatal("expected error when robots.txt fetch fails")
	}
}

func TestRobotsTxtURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://www.example.com/lawsuits", "https://www.example.com/robots.txt"},
		{"https://example.com/path/to/page?q=1", "https://example.com/robots.txt"},
		{"http://localhost:8080/foo", "http://localhost:8080/robots.txt"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := robotsTxtURL(tc.input)
			if err != nil {
				t.Fatalf("robotsTxtURL(%q): %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("robotsTxtURL(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNewPolicyFromRobots_ZeroRateLimit(t *testing.T) {
	policy, err := NewPolicyFromRobots("", 0)
	if err != nil {
		t.Fatalf("NewPolicyFromRobots: %v", err)
	}

	// Zero rate limit should allow unlimited requests without blocking.
	ctx := context.Background()
	start := time.Now()
	for i := 0; i < 100; i++ {
		if err := policy.Wait(ctx); err != nil {
			t.Fatalf("Wait: %v", err)
		}
	}
	elapsed := time.Since(start)

	if elapsed > 1*time.Second {
		t.Errorf("100 requests with zero rate limit took %v, expected near-instant", elapsed)
	}
}
