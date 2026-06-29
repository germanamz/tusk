package graphview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/query"
)

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
	srv := New(Deps{Query: &fakeQuerier{err: query.ErrSemanticUnavailable}})
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
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"missing embedder sentinel", query.ErrSemanticUnavailable, true},
		{"wrapped sentinel", fmt.Errorf("query: %w", query.ErrSemanticUnavailable), true},
		{"transport error (backend down)", &embed.TransportError{Err: errors.New("ollama: post: connection refused")}, true},
		// A non-transport ollama error (a 4xx / dim mismatch) is caller-fixable,
		// not "unavailable" — it must NOT be downgraded to 422.
		{"non-transport ollama error", errors.New("ollama: returned 2 dims, expected 768"), false},
		{"real index fault", errors.New("index: disk i/o error"), false},
	}

	for _, testCase := range cases {
		if got := isSemanticUnavailable(testCase.err); got != testCase.want {
			t.Errorf("%s: isSemanticUnavailable = %v, want %v", testCase.name, got, testCase.want)
		}
	}
}
