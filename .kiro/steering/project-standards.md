---
inclusion: always
---

# Project Standards & Guidelines

## Performance & Efficiency

Efficiency is a core principle of this project. All code must be written with performance in mind:

- Make maximum use of Go routines for concurrent operations (web scraping, parsing, database writes)
- Use channels and worker pools for managing concurrent scraper tasks
- Avoid blocking operations where possible; prefer async patterns
- Profile and benchmark critical paths (scraping, parsing, DB queries)
- Minimize memory allocations in hot loops (reuse buffers, avoid unnecessary copies)
- Use connection pooling for HTTP clients and database connections

## Legal Compliance

Everything in this project must be done within the confines of the law:

- Respect robots.txt on all target websites — do not scrape disallowed paths
- Implement polite crawling: honor rate limits, use reasonable delays between requests
- Set a proper User-Agent string identifying the scraper
- Do not circumvent any access controls, CAPTCHAs, or authentication mechanisms
- Only scrape publicly available information
- Comply with each website's Terms of Service
- Store only the minimum data necessary for the application's purpose
- Do not redistribute or republish scraped content

## Go Best Practices

- Use idiomatic Go patterns (error handling, interfaces, struct embedding)
- Keep packages small and focused
- Use `context.Context` for cancellation and timeouts on all I/O operations
- Prefer the standard library where it suffices; only add dependencies when they provide clear value
- Write table-driven tests
- Use `go vet`, `staticcheck`, and `golangci-lint` in CI/development

## Target Websites (Constants)

The following websites are the scraping targets, defined in a constants file:

1. classaction.org
2. topclassactions.com
3. FTC Refund Database (https://www.ftc.gov/enforcement/refunds)
