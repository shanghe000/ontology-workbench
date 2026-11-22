package workbench

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shanghe000/ontology-workbench/examples"
	"github.com/shanghe000/ontology-workbench/internal/catalog"
	"github.com/shanghe000/ontology-workbench/internal/graph"
	"github.com/shanghe000/ontology-workbench/web"
)

type transport func(*http.Request) (*http.Response, error)

func (t transport) RoundTrip(req *http.Request) (*http.Response, error) { return t(req) }

func sampleServer(t *testing.T) (*catalog.Store, graph.Snapshot, graph.Patch) {
	t.Helper()
	s, err := catalog.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	snap, patch, err := examples.Parcel()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.EnsureSpace(ctx, "parcel", "Parcel", snap); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureSpace(ctx, "other", "Other", graph.Empty()); err != nil {
		t.Fatal(err)
	}
	return s, snap, patch
}

func TestMCPScopesAndBrowserShareAPublishedTrace(t *testing.T) {
	s, snap, patch := sampleServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	read, submit, reviewer := rand.Text(), rand.Text(), rand.Text()
	hash := func(token string) tokenHash { return sha256.Sum256([]byte(token)) }
	access := Access{review: hash(reviewer), spaces: map[string]spaceAccess{
		"parcel": {read: hash(read), propose: hash(submit)}, "other": {read: hash(rand.Text())},
	}}
	server := httptest.NewServer(Handler(s, Options{Assets: web.Assets, Access: access}))
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "workbench-test", Version: "1"}, nil)
	connect := func(space, token string) (*mcp.ClientSession, error) {
		transport := transport(func(r *http.Request) (*http.Response, error) {
			clone := r.Clone(r.Context())
			clone.Header.Set("Authorization", "Bearer "+token)
			return http.DefaultTransport.RoundTrip(clone)
		})
		return client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp/" + space,
			HTTPClient: &http.Client{Transport: transport}, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	}
	if _, err := connect("other", read); err == nil {
		t.Fatal("read token was accepted by a different workspace")
	}
	reader, err := connect("parcel", read)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	tools, err := reader.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 2 {
		t.Fatalf("read tools: %+v / %v", tools, err)
	}
	for _, tool := range tools.Tools {
		if tool.Name == "submit_proposal" {
			t.Fatal("read scope can submit")
		}
	}
	writer, err := connect("parcel", submit)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	call, err := writer.CallTool(ctx, &mcp.CallToolParams{Name: "submit_proposal", Arguments: catalog.Submission{
		RequestID: "example-change", Title: "Weekend policy", BaseRevision: &snap.Revision, Patch: patch}})
	if err != nil || call.IsError {
		t.Fatalf("proposal failed: %+v / %v", call, err)
	}

	call, err = reader.CallTool(ctx, &mcp.CallToolParams{Name: "trace_evidence", Arguments: map[string]any{"node": "on-time", "budget": 6}})
	if err != nil || call.IsError {
		t.Fatalf("MCP trace failed: %+v / %v", call, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/spaces/parcel/trace?node=on-time&budget=6&revision=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+reviewer)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var browser any
	if r.StatusCode != http.StatusOK || json.NewDecoder(r.Body).Decode(&browser) != nil || !reflect.DeepEqual(browser, call.StructuredContent) {
		t.Fatalf("browser and MCP did not use the same published trace (HTTP %d)", r.StatusCode)
	}

	list, err := s.Proposals(ctx, "parcel")
	if err != nil || len(list) != 1 {
		t.Fatalf("expected one pending proposal: %+v / %v", list, err)
	}
	reviewURL := server.URL + "/api/spaces/parcel/proposals/" + list[0].ID + "/review"
	for _, auth := range []struct {
		token  string
		status int
	}{{submit, 401}, {reviewer, 200}} {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, reviewURL, strings.NewReader(`{"decision":"approve"}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+auth.token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != auth.status {
			t.Fatalf("review HTTP status = %d, want %d", res.StatusCode, auth.status)
		}
	}
	call, err = reader.CallTool(ctx, &mcp.CallToolParams{Name: "read_graph", Arguments: map[string]any{}})
	raw := ""
	if call != nil {
		data, _ := json.Marshal(call.StructuredContent)
		raw = string(data)
	}
	if err != nil || call.IsError || !strings.Contains(raw, `"revision":2`) || !strings.Contains(raw, `"id":"weekend"`) {
		t.Fatalf("the approved revision was not visible to MCP: %v", err)
	}
}

func TestDeepSeekContractUsesMetadataAndRejectsUnknownAnchors(t *testing.T) {
	_, snap, _ := sampleServer(t)
	p := DeepSeek(rand.Text(), "")
	out := `{"title":"Connect a source","patch":{"upsert_edges":[{"id":"wrong","from":"on-time","to":"events","kind":"measured_from","anchor":"invented-heading"}]}}`
	p.client.Transport = transport(func(req *http.Request) (*http.Response, error) {
		t.Helper()
		var request struct {
			Model    string                     `json:"model"`
			Format   map[string]string          `json:"response_format"`
			Messages []struct{ Content string } `json:"messages"`
		}
		if err := json.NewDecoder(req.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if req.URL.String() != modelURL || req.Header.Get("Authorization") != "Bearer "+p.key ||
			request.Model != "deepseek-flash" || request.Format["type"] != "json_object" || len(request.Messages) != 2 ||
			!strings.Contains(request.Messages[1].Content, "sources/event-dictionary.md") ||
			strings.Contains(request.Messages[1].Content, "The earliest carrier-accepted scan closes") {
			t.Fatal("unexpected request mode or source content")
		}
		reply, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{
			"finish_reason": "stop", "message": map[string]any{"content": out},
		}}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(reply)))}, nil
	})
	if _, err := p.plan(context.Background(), snap, "Add a citation"); err == nil {
		t.Fatal("unknown source anchor accepted")
	}
	out = strings.Replace(out, "invented-heading", "carrier-accepted", 1)
	if _, err := p.plan(context.Background(), snap, "Add a citation"); err != nil {
		t.Fatalf("valid proposal should be editable: %v", err)
	}
}
