package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/jacaudi/critical-thinking/internal/thinking"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCORSDefaultRejectsBrowser(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "")
	h := withCORS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestCORSAllowsConfiguredOrigin(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "https://app.example,https://other.example")

	h := withCORS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Origin", "https://app.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example" {
		t.Errorf("Allow-Origin = %q, want https://app.example", got)
	}
	if got := rec.Header().Get("Access-Control-Expose-Headers"); got != "mcp-session-id" {
		t.Errorf("Expose-Headers = %q, want mcp-session-id", got)
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Errorf("Vary = %q, want Origin", got)
	}
}

func TestCORSAllowsNoOrigin(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "")
	h := withCORS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	// no Origin header
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for no-origin request, got %d", rec.Code)
	}
}

func TestHealthEndpoint(t *testing.T) {
	registry := newSessionRegistry()
	registry.add(thinking.NewServer())
	registry.add(thinking.NewServer())

	h := makeHealthHandler(registry)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q, want application/json", ct)
	}

	var body struct {
		Status          string `json:"status"`
		Transport       string `json:"transport"`
		SessionsCreated int    `json:"sessionsCreated"`
		Version         string `json:"version"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("status = %q, want ok", body.Status)
	}
	if body.Transport != "streamable-http" {
		t.Errorf("transport = %q, want streamable-http", body.Transport)
	}
	if body.SessionsCreated != 2 {
		t.Errorf("sessionsCreated = %d, want 2", body.SessionsCreated)
	}
	// version may be "dev" or whatever -ldflags set; just confirm non-empty.
	if body.Version == "" {
		t.Errorf("version is empty")
	}
}

// httpClient does the minimal MCP plumbing: initialize, capture session id,
// then POST tools/call requests. We intentionally stay below the SDK to keep
// the test as a black-box integration check.
type httpClient struct {
	base      string
	sessionID string
	id        int
}

func newHTTPClient(t *testing.T, base string) *httpClient {
	t.Helper()
	c := &httpClient{base: base}
	c.id++
	resp, body := c.post(t, `{"jsonrpc":"2.0","id":`+strconv.Itoa(c.id)+`,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`, false)
	c.sessionID = resp.Header.Get("mcp-session-id")
	if c.sessionID == "" {
		t.Fatalf("no mcp-session-id in initialize response: %s", body)
	}
	// Send the initialized notification so the server treats us as a live client.
	c.post(t, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, true)
	return c
}

func (c *httpClient) callTool(t *testing.T, args thinking.ThoughtData) thinking.ThoughtResponse {
	t.Helper()
	c.id++
	bodyJSON, _ := json.Marshal(args)
	payload := `{"jsonrpc":"2.0","id":` + strconv.Itoa(c.id) +
		`,"method":"tools/call","params":{"name":"criticalthinking","arguments":` + string(bodyJSON) + `}}`
	_, body := c.post(t, payload, true)

	// The MCP SDK can return the result either as a JSON body OR as a single
	// SSE event with `data: {...}`. Handle both by extracting the first
	// JSON-RPC envelope from the body.
	jsonText := extractFirstJSON(body)

	var rpc struct {
		Result struct {
			StructuredContent thinking.ThoughtResponse `json:"structuredContent"`
			IsError           bool                     `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(jsonText), &rpc); err != nil {
		t.Fatalf("unmarshal tool response: %v\nbody=%s\nextracted=%s", err, body, jsonText)
	}
	if rpc.Result.IsError {
		t.Fatalf("tool returned isError=true: %s", body)
	}
	return rpc.Result.StructuredContent
}

func (c *httpClient) readResource(t *testing.T, uri string) string {
	t.Helper()
	c.id++
	payload := `{"jsonrpc":"2.0","id":` + strconv.Itoa(c.id) +
		`,"method":"resources/read","params":{"uri":"` + uri + `"}}`
	_, body := c.post(t, payload, true)
	jsonText := extractFirstJSON(body)
	var rpc struct {
		Result struct {
			Contents []struct {
				Text string `json:"text"`
			} `json:"contents"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(jsonText), &rpc); err != nil {
		t.Fatalf("unmarshal resource read: %v\nbody=%s\nextracted=%s", err, body, jsonText)
	}
	if len(rpc.Result.Contents) == 0 {
		t.Fatalf("no contents in resource response: %s", body)
	}
	return rpc.Result.Contents[0].Text
}

func (c *httpClient) post(t *testing.T, payload string, withSession bool) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, c.base+"/mcp", bytes.NewBufferString(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if withSession && c.sessionID != "" {
		req.Header.Set("mcp-session-id", c.sessionID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

// extractFirstJSON returns the first JSON-RPC envelope from a server response,
// handling both plain JSON bodies and Server-Sent Event framings.
func extractFirstJSON(body string) string {
	// Plain JSON body: starts with {.
	trimmed := strings.TrimSpace(body)
	if strings.HasPrefix(trimmed, "{") {
		return trimmed
	}
	// SSE framing: "data: {...}\n". Find the first "data: " line and return
	// everything after the prefix up to the next newline (or end of string).
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "data: ") {
			return strings.TrimPrefix(line, "data: ")
		}
	}
	return body
}

// validInputN is the integration-test analog of the package's validInput;
// defined here to avoid importing test-only helpers across packages.
func validInputN(num int, sessionTag string) thinking.ThoughtData {
	yes := true
	return thinking.ThoughtData{
		Thought:           sessionTag + " thought " + strconv.Itoa(num),
		ThoughtNumber:     num,
		TotalThoughts:     20,
		NextThoughtNeeded: &yes,
		Confidence:        0.5,
		Assumptions:       []string{},
		Critique:          "narrow",
		CounterArgument:   "alternative",
		NextStepRationale: "next",
	}
}

func TestCrossSessionIsolation(t *testing.T) {
	// Build the same handler chain main() uses.
	registry := newSessionRegistry()
	mcpHandler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		state := thinking.NewServer()
		registry.add(state)
		return newMCPServer(state)
	}, nil)
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpHandler)
	mux.HandleFunc("/health", makeHealthHandler(registry))

	ts := httptest.NewServer(withCORS(mux))
	defer ts.Close()

	clientA := newHTTPClient(t, ts.URL)
	clientB := newHTTPClient(t, ts.URL)

	if clientA.sessionID == clientB.sessionID {
		t.Fatal("two clients got the same session id")
	}

	// Drive 10 thoughts through each, interleaved.
	const N = 10
	var wg sync.WaitGroup

	wg.Go(func() {
		for i := 1; i <= N; i++ {
			clientA.callTool(t, validInputN(i, "A"))
		}
	})
	wg.Go(func() {
		for i := 1; i <= N; i++ {
			clientB.callTool(t, validInputN(i, "B"))
		}
	})
	wg.Wait()

	// Final call on each session: assert each only sees its own thoughts.
	respA := clientA.callTool(t, validInputN(N+1, "A"))
	respB := clientB.callTool(t, validInputN(N+1, "B"))

	if respA.ThoughtHistoryLength != N+1 {
		t.Errorf("session A history length = %d, want %d", respA.ThoughtHistoryLength, N+1)
	}
	if respB.ThoughtHistoryLength != N+1 {
		t.Errorf("session B history length = %d, want %d", respB.ThoughtHistoryLength, N+1)
	}

	// Registry should see both sessions live.
	if got := registry.count(); got < 2 {
		t.Errorf("registry count = %d, want >= 2", got)
	}
}

func TestThinkingCurrentResourceIsPerSession(t *testing.T) {
	registry := newSessionRegistry()
	mcpHandler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		state := thinking.NewServer()
		registry.add(state)
		return newMCPServer(state)
	}, nil)
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpHandler)
	ts := httptest.NewServer(withCORS(mux))
	defer ts.Close()

	clientA := newHTTPClient(t, ts.URL)
	clientB := newHTTPClient(t, ts.URL)
	clientA.callTool(t, validInputN(1, "A-resource"))
	clientB.callTool(t, validInputN(1, "B-resource"))

	readA := clientA.readResource(t, "thinking://current")
	readB := clientB.readResource(t, "thinking://current")

	if !strings.Contains(readA, "A-resource") {
		t.Errorf("session A snapshot missing its tag; got:\n%s", readA)
	}
	if strings.Contains(readA, "B-resource") {
		t.Errorf("session A snapshot LEAKED session B's tag; got:\n%s", readA)
	}
	if !strings.Contains(readB, "B-resource") {
		t.Errorf("session B snapshot missing its tag; got:\n%s", readB)
	}
	if strings.Contains(readB, "A-resource") {
		t.Errorf("session B snapshot LEAKED session A's tag; got:\n%s", readB)
	}
}
