package bookview

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRelatedHandlerOK pins the happy path end to end through the real mux:
// GET /api/related/{id} returns 200 and the fixed RelatedResponse the fake
// echoes.
func TestRelatedHandlerOK(test *testing.T) {
	fake := &fakeRelated{resp: RelatedResponse{
		Related: []RelatedNode{{ID: "d", Title: "D", GraphScore: 0.6, Distance: 1}},
	}}

	srv := New(Deps{Root: test.TempDir(), Related: fake})

	server := httptest.NewServer(srv.Handler())
	test.Cleanup(server.Close)

	resp, getErr := http.Get(server.URL + "/api/related/a")

	if getErr != nil {
		test.Fatalf("GET: %v", getErr)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		test.Fatalf("code=%d want 200", resp.StatusCode)
	}

	var got RelatedResponse

	if decodeErr := json.NewDecoder(resp.Body).Decode(&got); decodeErr != nil {
		test.Fatalf("decode: %v", decodeErr)
	}

	if len(got.Related) != 1 || got.Related[0].ID != "d" {
		test.Fatalf("got=%+v want the fake's fixed response", got)
	}
}

// TestRelatedHandlerNilSourceEmpty pins the wiring guard: a Deps built without
// a RelatedSource (the CLI hasn't wired Task 3.4's adapter, or chose not to)
// reports an empty rail rather than panicking on a nil interface call or
// erroring like the Search endpoint does.
func TestRelatedHandlerNilSourceEmpty(test *testing.T) {
	srv := New(Deps{Root: test.TempDir()})

	server := httptest.NewServer(srv.Handler())
	test.Cleanup(server.Close)

	resp, getErr := http.Get(server.URL + "/api/related/a")

	if getErr != nil {
		test.Fatalf("GET: %v", getErr)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		test.Fatalf("code=%d want 200", resp.StatusCode)
	}

	body, readErr := io.ReadAll(resp.Body)

	if readErr != nil {
		test.Fatalf("read body: %v", readErr)
	}

	if strings.TrimSpace(string(body)) != `{"related":[]}` {
		test.Fatalf("body=%s want {\"related\":[]}", body)
	}
}

// TestRelatedHandlerMatchesMarshalEmptyArray guards the nil-slice trap at the
// wire-byte level: a RelatedSource that found nothing (the fake's zero-value
// RelatedResponse leaves Related nil) must marshal "related" as [], not null.
// A JSON decode cannot distinguish the two, so only the raw bytes pin it.
func TestRelatedHandlerMatchesMarshalEmptyArray(test *testing.T) {
	fake := &fakeRelated{}

	srv := New(Deps{Root: test.TempDir(), Related: fake})

	server := httptest.NewServer(srv.Handler())
	test.Cleanup(server.Close)

	resp, getErr := http.Get(server.URL + "/api/related/a")

	if getErr != nil {
		test.Fatalf("GET: %v", getErr)
	}

	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)

	if readErr != nil {
		test.Fatalf("read body: %v", readErr)
	}

	if !strings.Contains(string(body), `"related":[]`) {
		test.Fatalf("body=%s want related as []", body)
	}
}

// TestRelatedHandlerAbsentParamsForwardNil pins the ruling this handler exists
// to enforce: absent hops/weight query params must reach RelatedSource.Related
// as nil, not as a pointer to zero. A handler that substituted &0 for "absent"
// would silently zero the manifest's configured graph-expansion weight and
// this test would fail against it.
func TestRelatedHandlerAbsentParamsForwardNil(test *testing.T) {
	fake := &fakeRelated{}

	srv := New(Deps{Root: test.TempDir(), Related: fake})

	server := httptest.NewServer(srv.Handler())
	test.Cleanup(server.Close)

	resp, getErr := http.Get(server.URL + "/api/related/a")

	if getErr != nil {
		test.Fatalf("GET: %v", getErr)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		test.Fatalf("code=%d want 200", resp.StatusCode)
	}

	_, hops, edgeTypes, weight := fake.lastRequest()

	if hops != nil {
		test.Fatalf("hops=%v want nil (absent)", *hops)
	}

	if weight != nil {
		test.Fatalf("weight=%v want nil (absent)", *weight)
	}

	if edgeTypes != nil {
		test.Fatalf("edgeTypes=%v want nil (absent)", edgeTypes)
	}
}

// TestRelatedHandlerWeightZeroForwardsNonNilPointer proves presence is
// expressible in the other direction: an explicit ?weight=0 must reach
// RelatedSource as a non-nil pointer to 0, distinguishable from an absent
// weight even though both dereference to the same float.
func TestRelatedHandlerWeightZeroForwardsNonNilPointer(test *testing.T) {
	fake := &fakeRelated{}

	srv := New(Deps{Root: test.TempDir(), Related: fake})

	server := httptest.NewServer(srv.Handler())
	test.Cleanup(server.Close)

	resp, getErr := http.Get(server.URL + "/api/related/a?weight=0")

	if getErr != nil {
		test.Fatalf("GET: %v", getErr)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		test.Fatalf("code=%d want 200", resp.StatusCode)
	}

	_, _, _, weight := fake.lastRequest()

	if weight == nil {
		test.Fatalf("weight=nil want non-nil pointer to 0")
	}

	if *weight != 0 {
		test.Fatalf("weight=%v want 0", *weight)
	}
}

// TestRelatedHandlerHopsPresentForwardsPointer is TestRelatedHandlerWeightZeroForwardsNonNilPointer's
// counterpart for hops: an explicit ?hops=2 must reach RelatedSource as a
// non-nil pointer to 2.
func TestRelatedHandlerHopsPresentForwardsPointer(test *testing.T) {
	fake := &fakeRelated{}

	srv := New(Deps{Root: test.TempDir(), Related: fake})

	server := httptest.NewServer(srv.Handler())
	test.Cleanup(server.Close)

	resp, getErr := http.Get(server.URL + "/api/related/a?hops=2")

	if getErr != nil {
		test.Fatalf("GET: %v", getErr)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		test.Fatalf("code=%d want 200", resp.StatusCode)
	}

	_, hops, _, _ := fake.lastRequest()

	if hops == nil || *hops != 2 {
		test.Fatalf("hops=%v want pointer to 2", hops)
	}
}

// TestRelatedHandlerEdgeTypesParses pins the comma-separated edge_types param:
// "a,b" must reach RelatedSource as []string{"a", "b"}.
func TestRelatedHandlerEdgeTypesParses(test *testing.T) {
	fake := &fakeRelated{}

	srv := New(Deps{Root: test.TempDir(), Related: fake})

	server := httptest.NewServer(srv.Handler())
	test.Cleanup(server.Close)

	resp, getErr := http.Get(server.URL + "/api/related/a?edge_types=a,b")

	if getErr != nil {
		test.Fatalf("GET: %v", getErr)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		test.Fatalf("code=%d want 200", resp.StatusCode)
	}

	_, _, edgeTypes, _ := fake.lastRequest()

	want := []string{"a", "b"}

	if len(edgeTypes) != len(want) || edgeTypes[0] != want[0] || edgeTypes[1] != want[1] {
		test.Fatalf("edgeTypes=%v want %v", edgeTypes, want)
	}
}

// TestRelatedHandlerSlashedID pins the {id...} wildcard through the real mux:
// a node id containing slashes must reach the handler, and RelatedSource,
// intact rather than truncated at the first segment.
func TestRelatedHandlerSlashedID(test *testing.T) {
	fake := &fakeRelated{resp: RelatedResponse{Related: []RelatedNode{{ID: "z"}}}}

	srv := New(Deps{Root: test.TempDir(), Related: fake})

	server := httptest.NewServer(srv.Handler())
	test.Cleanup(server.Close)

	resp, getErr := http.Get(server.URL + "/api/related/specs/nested/note.md")

	if getErr != nil {
		test.Fatalf("GET: %v", getErr)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		test.Fatalf("code=%d want 200", resp.StatusCode)
	}

	nodeID, _, _, _ := fake.lastRequest()

	if nodeID != "specs/nested/note.md" {
		test.Fatalf("nodeID=%q want %q", nodeID, "specs/nested/note.md")
	}
}

// TestRelatedHandlerMalformedHopsBadRequest pins the malformed-param decision:
// a hops value that fails to parse (a typo'd frontend, not an absent param)
// returns 400 and never reaches RelatedSource — silently falling back to the
// default would hide the typo from whoever is debugging it.
func TestRelatedHandlerMalformedHopsBadRequest(test *testing.T) {
	fake := &fakeRelated{}

	srv := New(Deps{Root: test.TempDir(), Related: fake})

	server := httptest.NewServer(srv.Handler())
	test.Cleanup(server.Close)

	resp, getErr := http.Get(server.URL + "/api/related/a?hops=abc")

	if getErr != nil {
		test.Fatalf("GET: %v", getErr)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		test.Fatalf("code=%d want 400", resp.StatusCode)
	}

	if fake.callCount() != 0 {
		test.Fatalf("callCount=%d want 0: a malformed param must never reach Related", fake.callCount())
	}
}

// TestRelatedHandlerMalformedWeightBadRequest is the malformed-hops test's
// counterpart for weight.
func TestRelatedHandlerMalformedWeightBadRequest(test *testing.T) {
	fake := &fakeRelated{}

	srv := New(Deps{Root: test.TempDir(), Related: fake})

	server := httptest.NewServer(srv.Handler())
	test.Cleanup(server.Close)

	resp, getErr := http.Get(server.URL + "/api/related/a?weight=notanumber")

	if getErr != nil {
		test.Fatalf("GET: %v", getErr)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		test.Fatalf("code=%d want 400", resp.StatusCode)
	}

	if fake.callCount() != 0 {
		test.Fatalf("callCount=%d want 0: a malformed param must never reach Related", fake.callCount())
	}
}

// TestRelatedHandlerSourceErrorUnavailable pins the error-mapping path: a
// RelatedSource failure (the graph adapter hit an index error, not a missing
// embedder — Related is embedder-free by spec) reports 503, mirroring
// handleIndex/handleNode's fallback classification.
func TestRelatedHandlerSourceErrorUnavailable(test *testing.T) {
	fake := &fakeRelated{errFor: map[string]error{"broken": errors.New("index corrupt")}}

	srv := New(Deps{Root: test.TempDir(), Related: fake})

	server := httptest.NewServer(srv.Handler())
	test.Cleanup(server.Close)

	resp, getErr := http.Get(server.URL + "/api/related/broken")

	if getErr != nil {
		test.Fatalf("GET: %v", getErr)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		test.Fatalf("code=%d want 503", resp.StatusCode)
	}
}
