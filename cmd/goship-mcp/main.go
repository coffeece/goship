// Command goship-mcp is the hosted GoShip MCP server behind
// https://mcp.goship.sh/mcp, for MCP clients that sign in with OAuth instead
// of running `goship mcp` locally.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/coffeece/goship/internal/mcphost"
)

// version is set by the release build.
var version = "dev"

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error("goship-mcp stopped", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := mcphost.ConfigFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	v := buildVersion()
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mcphost.Handler(cfg, mcphost.NewVerifier(cfg.Issuer, cfg.JWKSURL()), v, log),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		// A tool call waits on the API, whose client gives up after 60s.
		WriteTimeout: 90 * time.Second,
		IdleTimeout:  2 * time.Minute,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("serving", "addr", cfg.Addr, "resource", cfg.ResourceURL, "api", cfg.APIURL, "issuer", cfg.Issuer, "version", v)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// buildVersion is what the release build stamped or, failing that, the
// version the go command recorded, as cmd/goship reports it.
func buildVersion() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return strings.TrimPrefix(bi.Main.Version, "v")
	}
	return version
}
