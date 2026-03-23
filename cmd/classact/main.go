package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"classact/internal/logging"
	"classact/internal/scraper"
	"classact/internal/storage"
	"classact/internal/web"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "scrape":
		os.Exit(runScrape())
	case "serve":
		os.Exit(runServe())
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: classact <command>")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  scrape    Run the scraping engine")
	fmt.Fprintln(os.Stderr, "  serve     Start the web frontend server")
}

// signalContext returns a context that is cancelled on SIGINT or SIGTERM.
func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-sig:
			cancel()
		case <-ctx.Done():
		}
		signal.Stop(sig)
	}()
	return ctx, cancel
}

func dbPath() string {
	if v := os.Getenv("CLASSACT_DB"); v != "" {
		return v
	}
	return "classact.db"
}

func serverPort() string {
	if v := os.Getenv("CLASSACT_PORT"); v != "" {
		return v
	}
	return "6006"
}

func runScrape() int {
	logger := logging.NewLogger()
	ctx, cancel := signalContext()
	defer cancel()

	store, err := storage.NewSQLiteRepository(dbPath())
	if err != nil {
		logger.ErrorContext(ctx, "failed to open database", slog.String("error", err.Error()))
		return 1
	}
	defer store.Close()

	engine := scraper.NewEngine(store, logger, 3)

	result, err := engine.Run(ctx)
	if err != nil {
		logger.ErrorContext(ctx, "scraping run failed", slog.String("error", err.Error()))
		return 1
	}

	logger.InfoContext(ctx, "scraping run finished",
		slog.Int("total_found", result.TotalFound),
		slog.Int("new_records", result.NewRecords),
		slog.Int("updated_records", result.UpdatedRecords),
		slog.Int("errors", len(result.Errors)),
	)
	return 0
}

func runServe() int {
	logger := logging.NewLogger()
	ctx, cancel := signalContext()
	defer cancel()

	store, err := storage.NewSQLiteRepository(dbPath())
	if err != nil {
		logger.ErrorContext(ctx, "failed to open database", slog.String("error", err.Error()))
		return 1
	}
	defer store.Close()

	engine := scraper.NewEngine(store, logger, 3)

	addr := ":" + serverPort()
	srv := web.NewServer(store, engine, logger)

	logger.InfoContext(ctx, "starting web server", slog.String("addr", addr))
	if err := srv.Start(ctx, addr); err != nil {
		logger.ErrorContext(ctx, "server error", slog.String("error", err.Error()))
		return 1
	}
	return 0
}
