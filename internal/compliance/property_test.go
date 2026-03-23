package compliance

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"classact/internal/config"

	"pgregory.net/rapid"
)

// Feature: class-action-scraper, Property 6: robots.txt compliance

// TestPropertyRobotsTxtCompliance verifies that for any robots.txt content
// and any URL path, Policy.IsAllowed(path) returns false for paths disallowed
// by the robots.txt rules and true for allowed paths.
//
// **Validates: Requirements 3.1**
func TestPropertyRobotsTxtCompliance(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random disallow path segments (e.g. "/foo/", "/bar/baz/").
		numRules := rapid.IntRange(0, 10).Draw(t, "numRules")
		disallowPaths := make([]string, numRules)
		for i := 0; i < numRules; i++ {
			segments := rapid.IntRange(1, 3).Draw(t, fmt.Sprintf("segments_%d", i))
			var parts []string
			for j := 0; j < segments; j++ {
				seg := rapid.StringMatching(`[a-z]{1,8}`).Draw(t, fmt.Sprintf("seg_%d_%d", i, j))
				parts = append(parts, seg)
			}
			disallowPaths[i] = "/" + strings.Join(parts, "/") + "/"
		}

		// Build robots.txt content targeting the wildcard agent.
		var robotsLines []string
		robotsLines = append(robotsLines, "User-agent: *")
		for _, dp := range disallowPaths {
			robotsLines = append(robotsLines, "Disallow: "+dp)
		}
		robotsTxt := strings.Join(robotsLines, "\n") + "\n"

		// Create policy with a fast rate limit for test speed.
		policy, err := NewPolicyFromRobots(robotsTxt, 1*time.Millisecond)
		if err != nil {
			t.Fatalf("NewPolicyFromRobots failed: %v", err)
		}

		// Generate a random test path to check.
		testSegments := rapid.IntRange(1, 4).Draw(t, "testSegments")
		var testParts []string
		for j := 0; j < testSegments; j++ {
			seg := rapid.StringMatching(`[a-z]{1,8}`).Draw(t, fmt.Sprintf("testSeg_%d", j))
			testParts = append(testParts, seg)
		}
		testPath := "/" + strings.Join(testParts, "/")

		// Independently compute expected result: a path is disallowed if it
		// starts with any disallowed prefix.
		expectedDisallowed := false
		for _, dp := range disallowPaths {
			if strings.HasPrefix(testPath, dp) || testPath+"/" == dp {
				expectedDisallowed = true
				break
			}
		}
		expectedAllowed := !expectedDisallowed

		got := policy.IsAllowed(testPath)
		if got != expectedAllowed {
			t.Fatalf(
				"IsAllowed(%q) = %v, want %v\nrobots.txt:\n%s\ndisallowPaths: %v",
				testPath, got, expectedAllowed, robotsTxt, disallowPaths,
			)
		}
	})
}

// Feature: class-action-scraper, Property 7: Rate limiting applies uniformly

// TestPropertyRateLimitingAppliesUniformly verifies that for any sequence of N
// requests to the same domain, the elapsed time between the first and last
// request is at least (N-1) * rateLimit.
//
// **Validates: Requirements 3.2, 10.6**
func TestPropertyRateLimitingAppliesUniformly(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random rate limit between 10ms and 50ms.
		rateLimitMs := rapid.IntRange(10, 50).Draw(t, "rateLimitMs")
		rateLimit := time.Duration(rateLimitMs) * time.Millisecond

		// Generate a random number of requests between 2 and 5.
		n := rapid.IntRange(2, 5).Draw(t, "numRequests")

		// Create a policy with empty robots.txt and the generated rate limit.
		policy, err := NewPolicyFromRobots("", rateLimit)
		if err != nil {
			t.Fatalf("NewPolicyFromRobots failed: %v", err)
		}

		ctx := context.Background()

		// Measure elapsed time across N Wait() calls.
		start := time.Now()
		for i := 0; i < n; i++ {
			if err := policy.Wait(ctx); err != nil {
				t.Fatalf("Wait() call %d failed: %v", i, err)
			}
		}
		elapsed := time.Since(start)

		// The minimum expected elapsed time is (N-1) * rateLimit.
		// We subtract a small tolerance (2ms) per interval to account for
		// timing jitter in the OS scheduler.
		minExpected := time.Duration(n-1) * rateLimit
		tolerance := time.Duration(n-1) * 2 * time.Millisecond
		threshold := minExpected - tolerance

		if elapsed < threshold {
			t.Fatalf(
				"elapsed %v < threshold %v (minExpected=%v, tolerance=%v, N=%d, rateLimit=%v)",
				elapsed, threshold, minExpected, tolerance, n, rateLimit,
			)
		}
	})
}

// Feature: class-action-scraper, Property 8: User-Agent is set on all requests

// TestPropertyUserAgentIsSet verifies that for any HTTP request made via
// NewPolicy, the User-Agent header is non-empty and identifies the application
// by containing "ClassAct".
//
// **Validates: Requirements 3.3**
func TestPropertyUserAgentIsSet(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random site name.
		siteName := rapid.StringMatching(`[a-z]{3,12}\.[a-z]{2,4}`).Draw(t, "siteName")

		// Track captured User-Agent header from the robots.txt request.
		var mu sync.Mutex
		var capturedUA string

		// Create a mock HTTP server that serves robots.txt and captures User-Agent.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			capturedUA = r.Header.Get("User-Agent")
			mu.Unlock()

			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, "User-agent: *\nAllow: /")
		}))
		defer srv.Close()

		// Build a TargetSite pointing at the mock server.
		site := config.TargetSite{
			Name:      siteName,
			URL:       srv.URL,
			RateLimit: 1 * time.Millisecond,
		}

		// Create a policy via NewPolicy which fetches robots.txt from the mock server.
		ctx := context.Background()
		policy, err := NewPolicy(ctx, site)
		if err != nil {
			t.Fatalf("NewPolicy failed: %v", err)
		}

		// Verify the captured User-Agent header from the HTTP request.
		mu.Lock()
		ua := capturedUA
		mu.Unlock()

		if ua == "" {
			t.Fatal("User-Agent header was empty on robots.txt request")
		}
		if !strings.Contains(ua, "ClassAct") {
			t.Fatalf("User-Agent %q does not contain \"ClassAct\"", ua)
		}

		// Verify policy.UserAgent() returns a consistent, valid value.
		policyUA := policy.UserAgent()
		if policyUA == "" {
			t.Fatal("policy.UserAgent() returned empty string")
		}
		if !strings.Contains(policyUA, "ClassAct") {
			t.Fatalf("policy.UserAgent() = %q does not contain \"ClassAct\"", policyUA)
		}
	})
}
