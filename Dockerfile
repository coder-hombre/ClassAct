# Stage 1: Build
FROM golang:1.26-alpine AS builder

WORKDIR /src

# Copy dependency manifests first for layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /bin/classact ./cmd/classact

# Stage 2: Runtime
FROM alpine:latest

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S classact && adduser -S classact -G classact \
    && mkdir -p /data && chown classact:classact /data

COPY --from=builder /bin/classact /usr/local/bin/classact
COPY scripts/scraper-cron.sh /usr/local/bin/scraper-cron.sh
RUN chmod +x /usr/local/bin/scraper-cron.sh

USER classact
WORKDIR /data

ENV CLASSACT_DB=/data/classact.db
ENV CLASSACT_PORT=6006

EXPOSE 6006

CMD ["classact", "serve"]
