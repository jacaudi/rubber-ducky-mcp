package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jacaudi/critical-thinking/internal/thinking"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	idleTimeout   = 60 * time.Minute
	shutdownGrace = 10 * time.Second
)

// runStdio runs the server with one global SequentialThinkingServer instance.
// One process = one session, no cross-stream risk by definition.
func runStdio() {
	state := thinking.NewServer()
	srv := newMCPServer(state)

	transport := &mcp.LoggingTransport{Transport: &mcp.StdioTransport{}, Writer: os.Stderr}
	if err := srv.Run(context.Background(), transport); err != nil {
		log.Printf("server failed: %v", err)
		os.Exit(1)
	}
}

// runHTTP starts a Streamable HTTP server. Each session gets its own
// *mcp.Server with its own SequentialThinkingServer, constructed inside the
// factory closure. There is no map keyed by session-id anywhere in this
// process — the closure scope is the cross-session isolation invariant.
//
// Idle-session lifecycle is delegated to the SDK via
// StreamableHTTPOptions.SessionTimeout: the SDK closes its own per-session
// state after idleTimeout of inactivity, releasing the bound *mcp.Server (and
// the *SequentialThinkingServer it captures) for GC.
//
// We keep a small in-process registry that counts every session ever created.
// The registry is NOT synchronized with the SDK's view of live sessions — once
// the SDK closes a session we have no callback, so the count drifts upward.
// /health exposes it as `sessionsCreated` to make the semantics explicit.
func runHTTP(addr string) {
	// Wire ALLOWED_ORIGINS into the SDK's CSRF protection so browser clients
	// from those origins aren't rejected by the SDK's default same-origin
	// policy. Non-browser callers (no Origin / no Sec-Fetch-Site) are still
	// allowed regardless. Built before the signal context's defer stop() is
	// registered so a fatal config error here can't skip a pending defer.
	csrf := http.NewCrossOriginProtection()
	for _, o := range parseAllowedOrigins(os.Getenv("ALLOWED_ORIGINS")) {
		if err := csrf.AddTrustedOrigin(o); err != nil {
			log.Fatalf("invalid ALLOWED_ORIGINS entry %q: %v", o, err)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	registry := newSessionRegistry()

	handler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		state := thinking.NewServer()
		registry.add(state)
		return newMCPServer(state)
	}, &mcp.StreamableHTTPOptions{
		SessionTimeout:        idleTimeout,
		CrossOriginProtection: csrf,
	})

	host := "127.0.0.1"
	if os.Getenv("DOCKER") == "true" {
		host = "0.0.0.0"
	}
	// addr like ":3000" already includes the colon; combine with host.
	listenAddr := host + addr

	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	mux.HandleFunc("/health", makeHealthHandler(registry))

	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           withCORS(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()

	log.Printf("critical-thinking %s listening on http://%s", version, listenAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		//nolint:gocritic // defer stop() only unregisters signal handlers and is
		// moot at process exit; the alternatives (return / os.Exit) would change the
		// non-zero exit code or require refactoring runHTTP's signature.
		log.Fatalf("listen: %v", err)
	}
}

// newMCPServer constructs a configured *mcp.Server with the criticalthinking
// tool registered. The state argument is captured by the tool handler — this
// is how per-session isolation works in HTTP mode (each call to this function
// inside the StreamableHTTP factory closure produces a server bound to a fresh
// state). Stdio mode calls it once with a single global state.
func newMCPServer(state *thinking.SequentialThinkingServer) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "critical-thinking",
		Version: version,
	}, nil)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "criticalthinking",
		Description: thinking.ToolDescription,
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    true,
			DestructiveHint: ptrFalse(),
			IdempotentHint:  true,
			OpenWorldHint:   ptrFalse(),
		},
	}, makeToolHandler(state))

	srv.AddResource(&mcp.Resource{
		Name:        "thinking_current",
		Description: "Full thought history for the current session, including all critical-thinking fields (confidence, assumptions, critique, counterArgument).",
		URI:         "thinking://current",
		MIMEType:    "application/json",
	}, makeResourceHandler(state))

	return srv
}

func ptrFalse() *bool { f := false; return &f }

// makeToolHandler closes over a per-session state and returns the Go SDK's
// expected handler signature. The second return value (any) becomes the
// CallToolResult's structuredContent — we send the parsed ThoughtResponse.
func makeToolHandler(state *thinking.SequentialThinkingServer) func(context.Context, *mcp.CallToolRequest, thinking.ThoughtData) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args thinking.ThoughtData) (*mcp.CallToolResult, any, error) {
		res, err := state.ProcessThought(args)
		if err != nil {
			return nil, nil, err
		}

		callResult := &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: res.Text}},
			IsError: res.IsError,
		}

		if res.IsError {
			return callResult, nil, nil
		}

		var structured thinking.ThoughtResponse
		if jsonErr := json.Unmarshal([]byte(res.StructuredJSON), &structured); jsonErr != nil {
			// Should not happen — ProcessThought just produced this JSON.
			return callResult, nil, nil
		}
		return callResult, structured, nil
	}
}

// makeResourceHandler closes over a per-session state and returns a
// ResourceHandler that always returns this session's snapshot, regardless of
// the requested URI. We deliberately do NOT support a thinking://sessions
// listing or thinking://{id} lookup — that would expose the existence of
// other sessions and violate the cross-session isolation invariant.
func makeResourceHandler(state *thinking.SequentialThinkingServer) func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	return func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		snap := state.Snapshot()
		body, err := json.MarshalIndent(snap, "", "  ")
		if err != nil {
			return nil, err
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      req.Params.URI,
				MIMEType: "application/json",
				Text:     string(body),
			}},
		}, nil
	}
}

// sessionRegistry counts every session ever created in this process. It does
// NOT mediate access to states — only the factory closure that created a state
// holds the reference used by the tool handler — and it is NOT pruned when the
// SDK closes idle sessions (we have no callback). Treat the count as a
// lifetime "sessions created" counter, not an "active right now" gauge.
type sessionRegistry struct {
	mu     sync.Mutex
	states []*thinking.SequentialThinkingServer
}

func newSessionRegistry() *sessionRegistry { return &sessionRegistry{} }

func (r *sessionRegistry) add(s *thinking.SequentialThinkingServer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states = append(r.states, s)
}

func (r *sessionRegistry) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.states)
}

// withCORS gates browser access via the ALLOWED_ORIGINS env var (comma-
// separated list). Default is empty — no browser origins allowed.
//
// When an origin matches:
//   - Access-Control-Allow-Origin: <origin>
//   - Access-Control-Allow-Credentials: true
//   - Access-Control-Expose-Headers: mcp-session-id  (so JS clients can read it)
//   - Vary: Origin                                   (cache-poisoning mitigation)
//
// Non-browser callers (no Origin header) bypass the check entirely.
func withCORS(h http.Handler) http.Handler {
	allowed := parseAllowedOrigins(os.Getenv("ALLOWED_ORIGINS"))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if !slices.Contains(allowed, origin) {
				http.Error(w, "Origin not allowed", http.StatusForbidden)
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Expose-Headers", "mcp-session-id")
			w.Header().Add("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, mcp-session-id, MCP-Protocol-Version")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func parseAllowedOrigins(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func makeHealthHandler(r *sessionRegistry) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		body := struct {
			Status          string `json:"status"`
			Transport       string `json:"transport"`
			SessionsCreated int    `json:"sessionsCreated"`
			Version         string `json:"version"`
		}{
			Status:          "ok",
			Transport:       "streamable-http",
			SessionsCreated: r.count(),
			Version:         version,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(body)
	}
}
