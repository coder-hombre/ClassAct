#!/bin/sh
# Entrypoint for the scraper service.
# Sets up a crontab from the SCRAPE_SCHEDULE env var and runs crond in the foreground.

set -e

SCHEDULE="${SCRAPE_SCHEDULE:-0 3 * * *}"

echo "Setting up cron schedule: ${SCHEDULE}"

# Write crontab — pipe env vars into the scrape command
echo "${SCHEDULE} CLASSACT_DB=${CLASSACT_DB:-/data/classact.db} /usr/local/bin/classact scrape >> /proc/1/fd/1 2>> /proc/1/fd/2" > /etc/crontabs/root

# Run crond in the foreground so the container stays alive
exec crond -f -l 2
