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

func TestPrintSchema(t *testing.T) {
	var buf bytes.Buffer
	if err := printSchema(&buf); err != nil {
		t.Fatalf("printSchema: %v", err)
	}
	var d struct {
		Name         string          `json:"name"`
		Description  string          `json:"description"`
		InputSchema  json.RawMessage `json:"inputSchema"`
		OutputSchema json.RawMessage `json:"outputSchema"`
	}
	if err := json.Unmarshal(buf.Bytes(), &d); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if d.Name != "criticalthinking" {
		t.Errorf("name = %q; want criticalthinking", d.Name)
	}
	if d.Description != thinking.ToolDescription {
		t.Error("description does not match thinking.ToolDescription")
	}
	if !bytes.Contains(d.InputSchema, []byte(`"thought"`)) {
		t.Errorf("inputSchema missing the 'thought' property: %s", d.InputSchema)
	}
	if !bytes.Contains(d.OutputSchema, []byte(`"sessionConfidence"`)) {
		t.Errorf("outputSchema missing the 'sessionConfidence' property: %s", d.OutputSchema)
	}
}

func TestRunCLIJSONOutput(t *testing.T) {
	in := `{"thought":"x","thoughtNumber":1,"totalThoughts":1,"nextThoughtNeeded":false,"confidence":0.5,"assumptions":[],"critique":"c","counterArgument":"ca"}` + "\n"
	var out, errb bytes.Buffer
	code := runCLI(strings.NewReader(in), &out, &errb, true)
	if code != 0 {
		t.Fatalf("exit = %d; stderr = %s", code, errb.String())
	}
	var resp thinking.ThoughtResponse
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
		t.Fatalf("stdout is not NDJSON ThoughtResponse: %v\n%s", err, out.String())
	}
	if resp.ThoughtNumber != 1 || resp.ThoughtHistoryLength != 1 {
		t.Errorf("resp = %+v", resp)
	}
}

func TestRunCLITranscriptAndAccumulation(t *testing.T) {
	in := strings.Join([]string{
		`{"thought":"first","thoughtNumber":1,"totalThoughts":2,"nextThoughtNeeded":true,"confidence":0.5,"assumptions":[],"critique":"c","counterArgument":"ca","nextStepRationale":"continue"}`,
		`{"thought":"second","thoughtNumber":2,"totalThoughts":2,"nextThoughtNeeded":false,"confidence":0.7,"assumptions":[],"critique":"c2","counterArgument":"ca2"}`,
	}, "\n") + "\n"

	var out, errb bytes.Buffer
	code := runCLI(strings.NewReader(in), &out, &errb, false)
	if code != 0 {
		t.Fatalf("exit = %d; stderr = %s", code, errb.String())
	}
	s := out.String()
	if !strings.Contains(s, "Thought 1 of 2") || !strings.Contains(s, "Thought 2 of 2") {
		t.Errorf("missing thought headers:\n%s", s)
	}
	if !strings.Contains(s, "across 2 thoughts") {
		t.Errorf("expected accumulated footer 'across 2 thoughts':\n%s", s)
	}
}

func TestRunCLIMalformedLineContinues(t *testing.T) {
	in := "{not json\n" +
		`{"thought":"ok","thoughtNumber":1,"totalThoughts":1,"nextThoughtNeeded":false,"confidence":0.5,"assumptions":[],"critique":"c","counterArgument":"ca"}` + "\n"
	var out, errb bytes.Buffer
	code := runCLI(strings.NewReader(in), &out, &errb, false)
	if code != 1 {
		t.Errorf("exit = %d; want 1", code)
	}
	if !strings.Contains(errb.String(), "line 1") {
		t.Errorf("stderr should reference line 1: %q", errb.String())
	}
	if !strings.Contains(out.String(), "Thought 1 of 1") {
		t.Errorf("a valid line after a bad one must still render:\n%s", out.String())
	}
}

func TestRunCLIValidationErrorRouting(t *testing.T) {
	// Missing required "critique" → validation error result (IsError).
	bad := `{"thought":"x","thoughtNumber":1,"totalThoughts":1,"nextThoughtNeeded":false,"confidence":0.5,"assumptions":[],"counterArgument":"ca"}` + "\n"

	// default mode: error JSON to stderr, nothing to stdout.
	var out, errb bytes.Buffer
	if code := runCLI(strings.NewReader(bad), &out, &errb, false); code != 1 {
		t.Errorf("default mode exit = %d; want 1", code)
	}
	if out.Len() != 0 || !strings.Contains(errb.String(), `"status":"failed"`) {
		t.Errorf("default: want error JSON on stderr only; out=%q err=%q", out.String(), errb.String())
	}

	// -json mode: error JSON to stdout (keeps the NDJSON stream complete).
	var out2, errb2 bytes.Buffer
	if code := runCLI(strings.NewReader(bad), &out2, &errb2, true); code != 1 {
		t.Errorf("json mode exit = %d; want 1", code)
	}
	if !strings.Contains(out2.String(), `"status":"failed"`) || errb2.Len() != 0 {
		t.Errorf("json: error JSON should go to stdout only; out=%q err=%q", out2.String(), errb2.String())
	}
}

func TestRunCLIBlankAndEmpty(t *testing.T) {
	var out, errb bytes.Buffer
	if code := runCLI(strings.NewReader("\n   \n"), &out, &errb, false); code != 0 || out.Len() != 0 {
		t.Errorf("blank/empty: code=%d out=%q", code, out.String())
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
