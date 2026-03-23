# ClassAct

Class action lawsuit scraper. Discovers lawsuits from public websites, stores them locally in SQLite, and serves a web dashboard for browsing, filtering, and tracking applications.

## Sources

- classaction.org
- topclassactions.com
- FTC Refund Database (ftc.gov/enforcement/refunds)

## Prerequisites

- Go 1.26+
- Docker & Docker Compose (optional, for containerized deployment)

## Quick Start

```bash
# Build
go build -o classact ./cmd/classact

# Run a one-off scrape
./classact scrape

# Start the web dashboard
./classact serve
# Open http://localhost:6006
```

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `CLASSACT_DB` | `classact.db` | Path to SQLite database file |
| `CLASSACT_PORT` | `6006` | Web server port |

## Docker

```bash
# Build and start both web server and cron-based scraper
docker compose up -d

# Override defaults
CLASSACT_PORT=6008 SCRAPE_SCHEDULE="0 6 * * *" docker compose up -d
```

The `web` service runs the dashboard on port 6006. The `scraper` service runs `classact scrape` on a cron schedule (default: daily at 3 AM UTC). Both share a persistent volume for the SQLite database.

## API

| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/` | Dashboard |
| `POST` | `/api/scrape` | Trigger scrape (202 or 409 if already running) |
| `GET` | `/api/scrape/status` | Latest run result |
| `POST` | `/api/applied/:id` | Mark lawsuit as applied |
| `GET` | `/api/companies` | List company filter |
| `POST` | `/api/companies` | Add company to filter |
| `DELETE` | `/api/companies/:name` | Remove company from filter |
| `GET` | `/api/logs` | Recent scrape logs |

## Tests

```bash
go test ./... -count=1
```

67 tests across 7 packages, including 17 property-based tests via [rapid](https://github.com/flyingmutant/rapid).
