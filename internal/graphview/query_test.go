package graphview

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeQuerier + the error stub are defined here (first use), not in fakes_test.go,
// so they aren't flagged unused at the earlier-task commits.
var errSemanticUnavailableStub = errors.New("semantic ranking requires [embeddings] in tusk.toml")

type fakeQuerier struct {
	matches []Match
	err     error
}

func (fake *fakeQuerier) Run(_ context.Context, _ QueryInput) ([]Match, error) {
	return fake.matches, fake.err
}

func TestQuery_ReturnsMatches(t *testing.T) {
	srv := New(Deps{Query: &fakeQuerier{matches: []Match{{ID: "notes/a", Score: 0.9}}}})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/query", "application/json", strings.NewReader(`{"q":"auth keys","limit":10}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Matches []Match `json:"matches"`
	}
	if decodeErr := json.NewDecoder(resp.Body).Decode(&body); decodeErr != nil {
		t.Fatalf("decode: %v", decodeErr)
	}

	if len(body.Matches) != 1 || body.Matches[0].ID != "notes/a" {
		t.Fatalf("matches = %+v", body.Matches)
	}
}

func TestQuery_SemanticUnavailable(t *testing.T) {
	srv := New(Deps{Query: &fakeQuerier{err: errSemanticUnavailableStub}})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/query", "application/json", strings.NewReader(`{"q":"x"}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
}

func TestIsSemanticUnavailable(t *testing.T) {
	cases := map[string]bool{
		"semantic ranking requires [embeddings] in tusk.toml": true,
		"ollama: post: connection refused":                    true,
		"index: disk i/o error":                               false,
	}

	for msg, want := range cases {
		if got := isSemanticUnavailable(errors.New(msg)); got != want {
			t.Errorf("isSemanticUnavailable(%q) = %v, want %v", msg, got, want)
		}
	}
}
