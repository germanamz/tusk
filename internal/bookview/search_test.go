package bookview

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/query"
)

// TestSearchHandlerOK pins the happy path end to end through the real mux:
// POST /api/search returns 200 and the fixed SearchResponse the fake echoes.
func TestSearchHandlerOK(test *testing.T) {
	fake := &fakeSearcher{resp: SearchResponse{
		Matches: []Match{{ID: "a", Title: "A", Score: 0.9}},
		Model:   "m",
	}}

	srv := New(Deps{Root: test.TempDir(), Search: fake})

	server := httptest.NewServer(srv.Handler())
	test.Cleanup(server.Close)

	resp := postJSON(test, server.URL+"/api/search", SearchRequest{Q: "ok"})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		test.Fatalf("code=%d want 200", resp.StatusCode)
	}

	var got SearchResponse

	if decodeErr := json.NewDecoder(resp.Body).Decode(&got); decodeErr != nil {
		test.Fatalf("decode: %v", decodeErr)
	}

	if got.Model != "m" || len(got.Matches) != 1 || got.Matches[0].ID != "a" {
		test.Fatalf("got=%+v want the fake's fixed response", got)
	}
}

// TestSearchHandlerSemanticUnavailable pins the 422 degradation path: a
// missing embedder (query.ErrSemanticUnavailable) is a client-actionable
// condition, not a server fault.
func TestSearchHandlerSemanticUnavailable(test *testing.T) {
	fake := &fakeSearcher{errFor: map[string]error{"down": query.ErrSemanticUnavailable}}

	srv := New(Deps{Root: test.TempDir(), Search: fake})

	server := httptest.NewServer(srv.Handler())
	test.Cleanup(server.Close)

	resp := postJSON(test, server.URL+"/api/search", SearchRequest{Q: "down"})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		test.Fatalf("code=%d want 422", resp.StatusCode)
	}
}

// TestSearchHandlerTransportErrorUnavailable pins the second half of the
// degradation contract: an unreachable embedder (embed.IsTransportError) must
// classify to 422 exactly like ErrSemanticUnavailable. A handler that matched
// only the sentinel would 503 a real Ollama-down instead of degrading.
func TestSearchHandlerTransportErrorUnavailable(test *testing.T) {
	fake := &fakeSearcher{errFor: map[string]error{
		"unreachable": &embed.TransportError{Err: errors.New("dial tcp: connection refused")},
	}}

	srv := New(Deps{Root: test.TempDir(), Search: fake})

	server := httptest.NewServer(srv.Handler())
	test.Cleanup(server.Close)

	resp := postJSON(test, server.URL+"/api/search", SearchRequest{Q: "unreachable"})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		test.Fatalf("code=%d want 422", resp.StatusCode)
	}
}

// TestSearchHandlerOtherErrorUnavailable pins the discriminating half of the
// classification: an error that is neither ErrSemanticUnavailable nor a
// TransportError is a real fault, 503, not a blanket 422 for anything Search
// returns.
func TestSearchHandlerOtherErrorUnavailable(test *testing.T) {
	fake := &fakeSearcher{errFor: map[string]error{"boom": errors.New("index corrupt")}}

	srv := New(Deps{Root: test.TempDir(), Search: fake})

	server := httptest.NewServer(srv.Handler())
	test.Cleanup(server.Close)

	resp := postJSON(test, server.URL+"/api/search", SearchRequest{Q: "boom"})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		test.Fatalf("code=%d want 503", resp.StatusCode)
	}
}

// TestSearchHandlerNilSearcherUnavailable pins the wiring guard: a Deps built
// without a Searcher (the CLI hasn't wired Task 3.2's adapter, or chose not
// to) reports 503 rather than panicking on a nil interface call.
func TestSearchHandlerNilSearcherUnavailable(test *testing.T) {
	srv := New(Deps{Root: test.TempDir()})

	server := httptest.NewServer(srv.Handler())
	test.Cleanup(server.Close)

	resp := postJSON(test, server.URL+"/api/search", SearchRequest{Q: "ok"})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		test.Fatalf("code=%d want 503", resp.StatusCode)
	}
}

// TestSearchHandlerBadBody pins the 400 path: a malformed JSON body never
// reaches the Searcher.
func TestSearchHandlerBadBody(test *testing.T) {
	fake := &fakeSearcher{}

	srv := New(Deps{Root: test.TempDir(), Search: fake})

	server := httptest.NewServer(srv.Handler())
	test.Cleanup(server.Close)

	resp, postErr := http.Post(server.URL+"/api/search", "application/json", strings.NewReader("{not json"))

	if postErr != nil {
		test.Fatalf("POST: %v", postErr)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		test.Fatalf("code=%d want 400", resp.StatusCode)
	}

	if fake.callCount() != 0 {
		test.Fatalf("callCount=%d want 0: a bad body must never reach Search", fake.callCount())
	}
}

// TestSearchHandlerMatchesMarshalEmptyArray guards the nil-slice trap at the
// wire-byte level: a zero-result search must marshal "matches" as [], not
// null. A JSON decode cannot distinguish the two, so only the raw bytes pin
// it. The fake's zero-value SearchResponse leaves Matches nil, reproducing
// what a Searcher implementation is free to return for "nothing found".
func TestSearchHandlerMatchesMarshalEmptyArray(test *testing.T) {
	fake := &fakeSearcher{resp: SearchResponse{Model: "m"}}

	srv := New(Deps{Root: test.TempDir(), Search: fake})

	server := httptest.NewServer(srv.Handler())
	test.Cleanup(server.Close)

	resp := postJSON(test, server.URL+"/api/search", SearchRequest{Q: "nothing"})
	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)

	if readErr != nil {
		test.Fatalf("read body: %v", readErr)
	}

	if !strings.Contains(string(body), `"matches":[]`) {
		test.Fatalf("body=%s want matches as []", body)
	}
}

// TestSearchHandlerForwardsRequestFields pins that the handler is a pure
// pass-through onto the Searcher: Limit, Expand, and Explain must reach it
// unchanged rather than being dropped or silently reshaped, other than the
// default-limit substitution tested separately.
func TestSearchHandlerForwardsRequestFields(test *testing.T) {
	fake := &fakeSearcher{}

	srv := New(Deps{Root: test.TempDir(), Search: fake})

	server := httptest.NewServer(srv.Handler())
	test.Cleanup(server.Close)

	req := SearchRequest{
		Q:         "ok",
		Filter:    "type=note",
		Expand:    true,
		Hops:      2,
		EdgeTypes: []string{"references"},
		Weight:    0.4,
		Limit:     7,
		Explain:   true,
	}

	resp := postJSON(test, server.URL+"/api/search", req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		test.Fatalf("code=%d want 200", resp.StatusCode)
	}

	if got := fake.lastRequest(); !reflect.DeepEqual(got, req) {
		test.Fatalf("lastRequest=%+v want %+v forwarded unchanged", got, req)
	}
}

// TestSearchHandlerDefaultsNonPositiveLimit pins the brief's default: a
// request with Limit <= 0 must reach the Searcher with Limit substituted,
// never 0 or negative.
func TestSearchHandlerDefaultsNonPositiveLimit(test *testing.T) {
	fake := &fakeSearcher{}

	srv := New(Deps{Root: test.TempDir(), Search: fake})

	server := httptest.NewServer(srv.Handler())
	test.Cleanup(server.Close)

	resp := postJSON(test, server.URL+"/api/search", SearchRequest{Q: "ok", Limit: -1})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		test.Fatalf("code=%d want 200", resp.StatusCode)
	}

	if got := fake.lastRequest().Limit; got != defaultSearchLimit {
		test.Fatalf("forwarded Limit=%d want default %d", got, defaultSearchLimit)
	}
}
