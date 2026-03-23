# Implementation Plan: ClassAct — Class Action Scraper

## Overview

Incremental implementation of the ClassAct scraper, building from data models and storage up through the scraping engine, compliance layer, web frontend, and Docker packaging. Each task builds on the previous, with property tests placed close to the code they validate.

## Tasks

- [x] 1. Initialize project structure and dependencies
  - Initialize Go module (`go mod init classact`)
  - Create directory layout: `cmd/classact/`, `internal/{config,scraper,crawler,headless,parser,compliance,storage,model,web,logging}/`
  - Add dependencies: `colly`, `go-rod`, `goquery`, `go-sqlite3`, `robotstxt`, `rapid`
  - Create `cmd/classact/main.go` with stub `scrape` and `serve` subcommands
  - _Requirements: 1.1, 1.2, 1.3_

- [x] 2. Implement data models and storage layer
  - [x] 2.1 Create model types in `internal/model/model.go`
    - Define `LawsuitRecord` struct with all fields (ID, Title, Description, SourceURL, Company, FilingDate, Deadline, Status, Applied, AppliedAt, CreatedAt, UpdatedAt)
    - _Requirements: 8.2_

  - [x] 2.2 Implement SQLite repository in `internal/storage/`
    - Define `Repository` interface and `LawsuitFilter` struct
    - Implement `NewSQLiteRepository` with auto-migration (CREATE TABLE IF NOT EXISTS for lawsuits, company_filter, scrape_runs)
    - Implement `UpsertLawsuits` with INSERT OR REPLACE and batch semantics
    - Implement `ListLawsuits` with optional company filter (case-insensitive LIKE matching)
    - Implement `MarkApplied` with idempotency (only set `applied_at` if not already set)
    - Implement `GetAppliedStatus`
    - Implement `GetCompanyFilter`, `AddCompany`, `RemoveCompany`
    - Implement `SaveRunResult`, `GetLatestRunResult`
    - Enforce unique constraint on `source_url`
    - Use `context.Context` on all operations
    - _Requirements: 8.1, 8.2, 8.3, 8.4, 5.1, 5.2, 5.4, 6.1, 6.4, 6.5, 7.3, 7.4_

  - [x] 2.3 Write property test: Upsert idempotency (Property 4)
    - **Property 4: Upsert idempotency**
    - Generate random LawsuitRecords, upsert 1–5 times each, verify exactly one record per source URL with most recent field values
    - **Validates: Requirements 2.3, 8.3**

  - [x] 2.4 Write property test: Mark-as-applied round trip with idempotency (Property 10)
    - **Property 10: Mark-as-applied round trip with idempotency**
    - Generate random records, mark applied 1–3 times, verify Applied=true, AppliedAt stable after first call
    - **Validates: Requirements 5.1, 5.2, 5.3, 5.4, 11.4**

  - [x] 2.5 Write property test: Company filter round trip (Property 11)
    - **Property 11: Company filter round trip**
    - Generate random company names, add/remove sequences, verify final state matches expected set
    - **Validates: Requirements 6.1, 6.4, 6.5**

  - [x] 2.6 Write property test: Company filter matching (Property 12)
    - **Property 12: Company filter matching**
    - Generate random records and filter lists, verify ListLawsuits returns only matching records (case-insensitive); empty filter returns all
    - **Validates: Requirements 6.2, 6.3, 11.3**

- [x] 3. Checkpoint — Verify storage layer
  - Ensure all tests pass, ask the user if questions arise.

- [x] 4. Implement config and logging
  - [x] 4.1 Create site configuration in `internal/config/config.go`
    - Define `TargetSite` and `SiteSelectors` structs
    - Implement `Sites()` returning the three target sites (classaction.org, topclassactions.com, FTC refund database) with appropriate CSS selectors, rate limits, and UseHeadless flags
    - _Requirements: 1.1, 1.2, 1.3, 10.1_

  - [x] 4.2 Write property test: Config structure validity (Property 1)
    - **Property 1: Config structure validity**
    - Generate random site configs, validate non-empty Name, valid URL, valid RateLimit, non-empty selectors, defined UseHeadless
    - **Validates: Requirements 1.1, 10.1**

  - [x] 4.3 Implement structured JSON logger in `internal/logging/logging.go`
    - Create `NewLogger()` using `log/slog` with JSON handler writing to stdout
    - _Requirements: 9.3, 9.4_

- [x] 5. Implement compliance layer
  - [x] 5.1 Implement compliance policy in `internal/compliance/compliance.go`
    - Implement `NewPolicy` that fetches and parses robots.txt, creates rate limiter, sets User-Agent string
    - Implement `IsAllowed(path)` checking robots.txt rules
    - Implement `Wait(ctx)` for rate limiting
    - Implement `UserAgent()` returning the configured User-Agent
    - Conservative default: skip site if robots.txt can't be fetched/parsed
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 10.6_

  - [x] 5.2 Write property test: robots.txt compliance (Property 6)
    - **Property 6: robots.txt compliance**
    - Generate random robots.txt rules and paths, verify IsAllowed matches expected results
    - **Validates: Requirements 3.1**

  - [x] 5.3 Write property test: Rate limiting applies uniformly (Property 7)
    - **Property 7: Rate limiting applies uniformly**
    - Generate N requests, measure timing, verify elapsed >= (N-1) * rateLimit
    - **Validates: Requirements 3.2, 10.6**

  - [x] 5.4 Write property test: User-Agent is set on all requests (Property 8)
    - **Property 8: User-Agent is set on all requests**
    - Generate requests via mock server, capture and verify User-Agent header is non-empty and identifies the application
    - **Validates: Requirements 3.3**

- [x] 6. Implement parser
  - [x] 6.1 Implement HTML parser in `internal/parser/parser.go`
    - Implement `ExtractRecords(doc, site)` using goquery and site-specific CSS selectors
    - Implement `NormalizeCompanyName(name)` for consistent casing/whitespace
    - Handle missing fields gracefully (partial records)
    - _Requirements: 2.2_

  - [x] 6.2 Write property test: HTML parsing round trip (Property 3)
    - **Property 3: HTML parsing extracts all required fields (round trip)**
    - Generate HTML with known lawsuit data, parse with ExtractRecords, store in DB, retrieve, compare all fields
    - **Validates: Requirements 2.2, 8.2**

- [x] 7. Implement crawlers
  - [x] 7.1 Implement Colly-based crawler in `internal/crawler/crawler.go`
    - Implement `Crawl(ctx, site, policy)` using Colly
    - Respect compliance policy (robots.txt check, rate limit wait, User-Agent)
    - Use `context.Context` for cancellation/timeout
    - Delegate HTML parsing to `parser.ExtractRecords`
    - _Requirements: 2.1, 3.1, 3.2, 3.3, 3.4, 7.4_

  - [x] 7.2 Implement Go-Rod headless crawler in `internal/headless/headless.go`
    - Implement `Crawl(ctx, site, policy)` using Go-Rod
    - Wait for DOM stability before extraction
    - Respect same compliance rules as Colly crawler
    - Handle launch/render failures gracefully (log and return error)
    - _Requirements: 10.2, 10.4, 10.5, 10.6_

- [x] 8. Implement scraper engine and orchestration
  - [x] 8.1 Implement scraper engine in `internal/scraper/engine.go`
    - Define `Engine` struct with store, logger, maxWorkers
    - Implement `Run(ctx)` that:
      - Loads sites from `config.Sites()`
      - Creates buffered channel worker pool
      - Launches goroutine per site with panic recovery
      - Dispatches to `crawler.Crawl` or `headless.Crawl` based on `UseHeadless`
      - Collects results via channel, batches upserts to storage
      - Aggregates errors into `RunResult`
      - Saves RunResult to storage
      - Logs run summary (start, end, total found, duration)
    - _Requirements: 2.1, 2.3, 2.4, 2.5, 7.1, 7.2, 7.3, 7.4, 7.5, 9.1, 9.2_

  - [x] 8.2 Write property test: All configured sites attempted (Property 2)
    - **Property 2: All configured sites are attempted**
    - Generate N mock sites, run engine, verify all N appear in results or errors
    - **Validates: Requirements 2.1**

  - [x] 8.3 Write property test: Fault tolerance across sites (Property 5)
    - **Property 5: Fault tolerance across sites**
    - Generate N sites where K randomly fail, verify remaining N-K succeed and failures appear in RunResult.Errors
    - **Validates: Requirements 2.4, 7.5, 10.5**

  - [x] 8.4 Write property test: Crawl strategy dispatch (Property 15)
    - **Property 15: Crawl strategy dispatch**
    - Generate sites with random UseHeadless flags, verify correct module is called via mock crawl functions
    - **Validates: Requirements 10.2, 10.3**

  - [x] 8.5 Write property test: Context cancellation propagation (Property 13)
    - **Property 13: Context cancellation propagation**
    - Generate operations with cancelled contexts, verify timely return with context error
    - **Validates: Requirements 7.4**

  - [x] 8.6 Write property test: Exit code correctness (Property 9)
    - **Property 9: Exit code correctness**
    - Generate runs with/without fatal errors, verify Run returns nil error on success and non-nil on fatal failure
    - **Validates: Requirements 4.4, 4.5**

- [x] 9. Checkpoint — Verify scraping engine
  - Ensure all tests pass, ask the user if questions arise.

- [x] 10. Wire CLI entrypoint
  - [x] 10.1 Complete `cmd/classact/main.go`
    - Wire `scrape` subcommand: create logger, open DB, create engine, call `Run(ctx)`, exit with appropriate code
    - Wire `serve` subcommand: create logger, open DB, create engine, create web server, start on configured port
    - Use `context.Context` with signal handling for graceful shutdown
    - _Requirements: 4.4, 4.5, 9.3_

  - [x] 10.2 Write property test: Structured JSON logging (Property 14)
    - **Property 14: Structured JSON logging**
    - Generate scraping runs, capture stdout, parse each line as JSON, verify required fields (start time, end time, total found, duration for summaries; site, url, error for errors)
    - **Validates: Requirements 9.1, 9.2, 9.4**

- [x] 11. Implement web frontend
  - [x] 11.1 Implement HTTP server and API handlers in `internal/web/`
    - Create `Server` struct with store, engine, logger, and `atomic.Bool` scraping guard
    - Implement `GET /` — dashboard page serving HTML template with lawsuit list, filters, run status
    - Implement `POST /api/scrape` — trigger on-demand scrape (202 Accepted or 409 Conflict)
    - Implement `GET /api/scrape/status` — poll scrape progress
    - Implement `POST /api/applied/:id` — mark lawsuit as applied
    - Implement `GET /api/companies` — list company filter
    - Implement `POST /api/companies` — add company to filter
    - Implement `DELETE /api/companies/:name` — remove company from filter
    - Implement `GET /api/logs` — recent scraping logs
    - Serve on configurable port (default 6006, range 6000–6010)
    - _Requirements: 11.1, 11.2, 11.3, 11.4, 11.5, 11.6, 11.7, 11.8, 11.9, 11.10, 11.11_

  - [x] 11.2 Create HTML templates and static assets
    - Dashboard template: lawsuit table with title, company, filing date, deadline, status, applied status
    - Company filter management UI (add/remove)
    - Scrape button with progress indicator and disabled state during active scrape
    - Empty state message when no lawsuits exist
    - Recent run status display (start time, end time, records found, errors)
    - Scraping logs display
    - _Requirements: 11.2, 11.5, 11.6, 11.7, 11.8, 11.9, 11.10, 11.11_

  - [x] 11.3 Write property test: Web frontend lists all records and run status (Property 16)
    - **Property 16: Web frontend lists all records and run status**
    - Generate random DB contents, query listing endpoint, verify all records returned with complete fields; verify run status endpoint returns latest RunResult
    - **Validates: Requirements 11.2, 11.6**

  - [x] 11.4 Write property test: Scrape trigger mutual exclusion (Property 17)
    - **Property 17: Scrape trigger mutual exclusion**
    - Trigger concurrent POST /api/scrape requests, verify exactly one 202 and rest are 409
    - **Validates: Requirements 11.10, 11.11**

- [x] 12. Checkpoint — Verify web frontend
  - Ensure all tests pass, ask the user if questions arise.

- [x] 13. Docker packaging
  - [x] 13.1 Create Dockerfile
    - Multi-stage build: Go builder stage + minimal runtime stage
    - Build the `classact` binary
    - _Requirements: 4.2_

  - [x] 13.2 Create docker-compose.yml
    - Configure cron-based scheduler triggering `classact scrape` daily at configurable time
    - Mount SQLite database as a Docker volume for persistence
    - Expose web frontend port
    - Set environment variables for configuration
    - _Requirements: 4.1, 4.3, 8.5_

- [x] 14. Final checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties from the design document
- Unit tests validate specific examples and edge cases
- All 17 correctness properties from the design are covered as property test sub-tasks
