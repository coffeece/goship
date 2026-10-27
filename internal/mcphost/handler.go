package mcphost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coffeece/goship/internal/mcptools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

// maxBody caps a JSON-RPC request; tool arguments are a few kilobytes at most.
const maxBody = 4 << 20

// TokenVerifier checks an access token; *Verifier is the real one.
type TokenVerifier interface {
	Verify(ctx context.Context, token string) (*Claims, error)
}

// Handler is the whole server: the MCP endpoint, its OAuth discovery
// document, a health check and a page for people who open the URL.
func Handler(cfg Config, v TokenVerifier, version string, log *slog.Logger) http.Handler {
	prm := oauthex.ProtectedResourceMetadata{
		Resource:               cfg.ResourceURL,
		AuthorizationServers:   []string{cfg.Issuer},
		BearerMethodsSupported: []string{"header"},
		ResourceName:           "GoShip",
	}

	// Stateless: every request stands alone, so any unit can answer it and
	// nothing breaks when Cloudflare or a deploy drops a connection. One
	// server serves everyone (building one costs ~30ms); the credential
	// travels in each request's context.
	server := mcptools.New(tokenClients{api: cfg.APIURL, version: version}, version, mcptools.Options{})
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, Logger: log})

	mux := http.NewServeMux()
	metadata := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=300")
		_ = json.NewEncoder(w).Encode(prm)
	}
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", metadata)
	if p := strings.TrimRight(cfg.mcpPath(), "/"); p != "" {
		mux.HandleFunc("GET /.well-known/oauth-protected-resource"+p, metadata)
	}
	mux.Handle(cfg.mcpPath(), requireToken(cfg, v, log, mcpHandler))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, rootPage, cfg.ResourceURL, cfg.ResourceURL)
	})
	return logRequests(log, mux)
}

const rootPage = `GoShip MCP server

This is the Model Context Protocol endpoint for GoShip (https://goship.sh):
it lets an AI assistant run and operate your GoShip apps, signed in as you.

  MCP URL: %s

claude.ai: Customize > Connectors > Add custom connector, paste the URL, then
sign in with your GoShip account.

Claude Code: claude mcp add --transport http goship %s

Deploying from a directory on your machine needs the local server instead:
install the goship CLI and run "goship mcp".
`

// requireToken answers any request without a valid access token with the 401
// that starts (or, for an expired token, refreshes) the OAuth sign-in. It has
// to happen here, at the HTTP layer: a tool error would reach the client as a
// 200 and never prompt anyone to sign in.
func requireToken(cfg Config, v TokenVerifier, log *slog.Logger, next http.Handler) http.Handler {
	challenge := func(w http.ResponseWriter, desc string) {
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer error="invalid_token", error_description=%q, resource_metadata=%q`, desc, cfg.ResourceMetadataURL()))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_token", "error_description": desc})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
		token = strings.TrimSpace(token)
		if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
			challenge(w, "sign in to GoShip")
			return
		}
		claims, err := v.Verify(r.Context(), token)
		switch {
		case errors.Is(err, ErrUnavailable):
			// Not the caller's fault, and a 401 would send them round the
			// sign-in again for nothing.
			log.Error("cannot verify tokens", "error", err)
			http.Error(w, "cannot verify the token right now; try again shortly", http.StatusServiceUnavailable)
			return
		case err != nil:
			log.Info("token refused", "error", err)
			desc := "the GoShip token is not valid"
			if errors.Is(err, ErrExpired) {
				desc = "the GoShip token has expired"
			}
			challenge(w, desc)
			return
		}
		if rec, ok := w.(*recorder); ok {
			rec.user = claims.Subject
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		next.ServeHTTP(w, r.WithContext(withToken(r.Context(), token)))
	})
}

// recorder captures what logRequests reports.
type recorder struct {
	http.ResponseWriter
	status int
	user   string
}

func (r *recorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// logRequests logs one line per request, never the Authorization header.
func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &recorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		log.Info("request", "method", r.Method, "path", r.URL.Path, "status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(), "user", rec.user)
	})
}
