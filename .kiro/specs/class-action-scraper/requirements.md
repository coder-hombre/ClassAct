# Requirements Document

## Introduction

ClassAct is a Go-based web scraper that discovers class action lawsuits from multiple public websites. It runs daily inside a Docker container, stores lawsuit data in a local database, and lets the user filter lawsuits by companies they actually use. The system tracks which lawsuits the user has already applied to. All scraping is performed legally and efficiently using goroutines.

## Glossary

- **Scraper**: The Go application that crawls target websites and extracts class action lawsuit data using Colly and goquery.
- **Lawsuit_Record**: A structured representation of a single class action lawsuit containing title, description, source URL, company name, filing date, deadline, and status.
- **Application_Tracker**: The database component that records which lawsuits the user has marked as "applied to."
- **Company_Filter**: A user-defined list of company names used to filter lawsuits to only those the user can legally join.
- **Target_Site**: A website defined in the constants file from which the Scraper extracts lawsuit data. Initial sites: classaction.org, topclassactions.com, FTC refund database.
- **Constants_File**: A Go source file that defines all Target_Site URLs and related configuration values.
- **Scheduler**: The Docker-based cron mechanism that triggers the Scraper once per day.
- **Database**: A lightweight local database (SQLite) that persists Lawsuit_Records and application status.
- **Headless_Browser**: A Go-Rod-based browser automation component that renders JavaScript-heavy pages when the standard Colly-based Scraper cannot extract data.
- **Web_Frontend**: A local web application that provides a browser-based interface for viewing, filtering, and managing Lawsuit_Records and scraping operations.

## Requirements

### Requirement 1: Website Configuration

**User Story:** As a developer, I want target websites defined in a constants file, so that adding or removing scraping targets requires only a configuration change.

#### Acceptance Criteria

1. THE Constants_File SHALL define the URL, name, and scraping parameters for each Target_Site.
2. THE Constants_File SHALL include entries for classaction.org, topclassactions.com, and the FTC refund database (https://www.ftc.gov/enforcement/refunds).
3. WHEN a new Target_Site is added to the Constants_File, THE Scraper SHALL include the new Target_Site in the next scraping run without code changes beyond the constants entry.

### Requirement 2: Web Crawling

**User Story:** As a user, I want the scraper to crawl target websites for class action lawsuits, so that I can discover lawsuits I may be eligible to join.

#### Acceptance Criteria

1. WHEN the Scraper is triggered, THE Scraper SHALL crawl each Target_Site defined in the Constants_File using the Colly library.
2. WHEN crawling a Target_Site, THE Scraper SHALL parse the HTML response using goquery to extract Lawsuit_Record fields (title, description, source URL, company name, filing date, deadline, status).
3. WHEN a Lawsuit_Record with the same source URL already exists in the Database, THE Scraper SHALL update the existing record instead of creating a duplicate.
4. IF a Target_Site is unreachable or returns an HTTP error, THEN THE Scraper SHALL log the error and continue crawling the remaining Target_Sites.
5. WHEN crawling multiple Target_Sites, THE Scraper SHALL process Target_Sites concurrently using goroutines and a worker pool.

### Requirement 3: Legal Compliance

**User Story:** As a user, I want the scraper to operate within legal boundaries, so that I am not exposed to legal risk from using the tool.

#### Acceptance Criteria

1. BEFORE crawling a Target_Site, THE Scraper SHALL fetch and parse the robots.txt file for the Target_Site and respect all disallowed paths.
2. WHILE crawling a Target_Site, THE Scraper SHALL enforce a configurable rate limit (minimum 1 second delay between requests to the same domain).
3. THE Scraper SHALL set a descriptive User-Agent string that identifies the application name and purpose on every HTTP request.
4. THE Scraper SHALL only access publicly available pages and SHALL NOT attempt to bypass authentication, CAPTCHAs, or access controls.
5. THE Database SHALL store only the minimum data fields necessary to identify and track class action lawsuits.

### Requirement 4: Daily Scheduling via Docker

**User Story:** As a user, I want the scraper to run automatically once per day in a Docker container, so that I always have up-to-date lawsuit information without manual intervention.

#### Acceptance Criteria

1. THE Scheduler SHALL trigger the Scraper exactly once per day at a configurable time.
2. THE project SHALL include a Dockerfile that builds the Go application into a minimal container image.
3. THE project SHALL include a docker-compose.yml that configures the Scheduler, the Database volume mount, and environment variables.
4. WHEN the Scheduler triggers the Scraper, THE Scraper SHALL complete the full crawl cycle and exit with a zero exit code on success.
5. IF the Scraper encounters a fatal error during a scheduled run, THEN THE Scraper SHALL exit with a non-zero exit code and log the error.

### Requirement 5: Application Tracking

**User Story:** As a user, I want to mark lawsuits I've already applied to, so that I can keep track of my submissions and avoid re-applying.

#### Acceptance Criteria

1. THE Application_Tracker SHALL store a record for each Lawsuit_Record the user marks as "applied to," including the date of application.
2. WHEN the user marks a Lawsuit_Record as applied, THE Application_Tracker SHALL persist the status in the Database.
3. WHEN displaying Lawsuit_Records, THE Scraper SHALL indicate which records the user has already applied to.
4. THE Application_Tracker SHALL prevent a Lawsuit_Record from being marked as applied more than once.

### Requirement 6: Company Filter

**User Story:** As a user, I want to filter lawsuits by companies I actually use, so that I only see lawsuits I can legally join.

#### Acceptance Criteria

1. THE Company_Filter SHALL allow the user to define a list of company names to filter by.
2. WHEN the user applies the Company_Filter, THE Scraper SHALL return only Lawsuit_Records where the company name matches an entry in the Company_Filter list (case-insensitive).
3. WHEN no Company_Filter is applied, THE Scraper SHALL return all Lawsuit_Records.
4. THE Company_Filter list SHALL be persisted in the Database so the user does not need to re-enter the list on each run.
5. THE Company_Filter SHALL support adding and removing company names.

### Requirement 7: Concurrent Processing

**User Story:** As a developer, I want the scraper to maximize use of goroutines, so that scraping and data processing are as efficient as possible.

#### Acceptance Criteria

1. WHEN crawling multiple Target_Sites, THE Scraper SHALL launch a separate goroutine for each Target_Site.
2. THE Scraper SHALL use a channel-based worker pool to limit the maximum number of concurrent HTTP requests.
3. WHEN writing Lawsuit_Records to the Database, THE Scraper SHALL batch inserts to minimize database round-trips.
4. THE Scraper SHALL use context.Context for cancellation and timeout control on all HTTP requests and database operations.
5. IF a goroutine encounters a panic, THEN THE Scraper SHALL recover from the panic, log the error, and continue processing remaining work.

### Requirement 8: Data Storage

**User Story:** As a user, I want lawsuit data stored in a local database, so that I can query and review lawsuits between scraping runs.

#### Acceptance Criteria

1. THE Database SHALL use SQLite as the storage engine.
2. THE Database SHALL store Lawsuit_Records with the following fields: unique ID, title, description, source URL, company name, filing date, deadline, status, and created/updated timestamps.
3. THE Database SHALL enforce a unique constraint on the source URL field to prevent duplicate Lawsuit_Records.
4. WHEN the Scraper starts for the first time, THE Database SHALL automatically create the required tables if they do not exist.
5. THE Database file SHALL be stored in a Docker volume so data persists across container restarts.

### Requirement 9: Logging and Observability

**User Story:** As a developer, I want structured logging throughout the scraper, so that I can diagnose issues and monitor scraping performance.

#### Acceptance Criteria

1. THE Scraper SHALL log the start and end of each scraping run, including the total number of Lawsuit_Records found and the duration.
2. WHEN an error occurs during crawling or parsing, THE Scraper SHALL log the error with the Target_Site name, URL, and error details.
3. THE Scraper SHALL write logs to stdout so Docker can capture them natively.
4. THE Scraper SHALL use structured logging (JSON format) with consistent field names.

### Requirement 10: Headless Browser Fallback

**User Story:** As a user, I want the scraper to fall back to a headless browser for JavaScript-heavy sites, so that I can still discover lawsuits from sites that hide content from simple HTTP scrapers.

#### Acceptance Criteria

1. THE Constants_File SHALL include a boolean flag per Target_Site that indicates whether the Headless_Browser fallback is enabled for that site.
2. WHEN the Headless_Browser fallback is enabled for a Target_Site, THE Scraper SHALL use Go-Rod to render the page in a headless browser and extract Lawsuit_Record fields from the rendered DOM.
3. WHEN the Headless_Browser fallback is disabled for a Target_Site, THE Scraper SHALL use the standard Colly-based crawling approach for that site.
4. WHEN the Headless_Browser renders a page, THE Scraper SHALL wait for the page to reach a stable DOM state before extracting data.
5. IF the Headless_Browser fails to launch or render a page, THEN THE Scraper SHALL log the error with the Target_Site name and continue processing the remaining Target_Sites.
6. WHILE using the Headless_Browser, THE Scraper SHALL enforce the same rate limiting and legal compliance rules as the standard Colly-based Scraper.

### Requirement 11: Local Web Frontend

**User Story:** As a user, I want a local web application to interact with the scraper system, so that I can view, filter, and manage lawsuits through a browser instead of the command line.

#### Acceptance Criteria

1. THE Web_Frontend SHALL serve a local web application accessible via a configurable HTTP port.
2. THE Web_Frontend SHALL display a list of all Lawsuit_Records stored in the Database, including title, company name, filing date, deadline, status, and application status.
3. WHEN the user selects a company name filter, THE Web_Frontend SHALL display only Lawsuit_Records matching the selected companies (case-insensitive).
4. WHEN the user marks a Lawsuit_Record as applied through the Web_Frontend, THE Application_Tracker SHALL persist the applied status in the Database.
5. THE Web_Frontend SHALL provide an interface for managing the Company_Filter list, including adding and removing company names.
6. THE Web_Frontend SHALL display the status of the most recent scraping run, including start time, end time, number of Lawsuit_Records found, and any errors encountered.
7. THE Web_Frontend SHALL display scraping logs from the most recent run.
8. WHEN the Database contains no Lawsuit_Records, THE Web_Frontend SHALL display an empty state message indicating no lawsuits have been scraped.
9. THE Web_Frontend SHALL provide a button that triggers an on-demand scraping run.
10. WHEN the user clicks the scrape button, THE Web_Frontend SHALL start the scraping operation asynchronously and display a progress indicator until the run completes.
11. WHILE a scraping run is already in progress, THE Web_Frontend SHALL disable the scrape button and display a message indicating a run is in progress.
