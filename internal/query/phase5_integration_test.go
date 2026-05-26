package query_test

import (
	"context"
	"database/sql"
	"sort"
	"testing"

	"github.com/germanamz/tusk/internal/graphexpand"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/typeref"
)

// TestPhase5_ThreeReferenceFormsAcrossWalker is the Phase 5 acceptance
// test: a single fixture carries one user-namespace edge (source IS
// NULL) and one markdown-namespace edge (source = 'markdown'), both of
// type `contains`. Three Walker.Expand runs — one per reference form —
// must select the correct subset:
//
//	bare `contains`        → union (both edges fire)
//	`:contains`            → user-namespace only
//	`markdown:contains`    → markdown-namespace only
//
// This validates the end-to-end wiring: typeref.Parse → Walker.Expand
// → EdgeRepo.NeighborsByEdgeRefs → SQL scope-aware WHERE clauses.
func TestPhase5_ThreeReferenceFormsAcrossWalker(test *testing.T) {
	store := openTestStore(test)

	nodes := index.NewNodeRepo(store)
	edges := index.NewEdgeRepo(store)

	// Seed the source nodes for both edges. target_id has no FK
	// constraint, so we only need to upsert the rows that appear on
	// the source side.
	for _, id := range []string{"project-launch", "standup"} {
		if err := nodes.Upsert(index.NodeRow{
			ID:             id,
			Type:           "note",
			Path:           id + ".md",
			Title:          id,
			PropertiesJSON: "{}",
			LastChecksum:   "x",
		}); err != nil {
			test.Fatalf("upsert %s: %v", id, err)
		}
	}

	// User-namespace edge: project-launch -contains-> task-a.
	// kind=direct + source NULL satisfies the edges CHECK constraint.
	if err := edges.UpsertAll("project-launch", "project-launch.md", []index.EdgeRow{
		{
			Type:       "contains",
			SourceID:   "project-launch",
			TargetID:   "task-a",
			SourcePath: "project-launch.md",
			Kind:       "direct",
		},
	}); err != nil {
		test.Fatalf("upsert user edge: %v", err)
	}

	// Markdown-namespace edge: standup -contains-> standup#section-a.
	// kind=structural + source='markdown' is the shape the sub-unit
	// pipeline writes in production.
	if err := edges.UpsertAll("standup", "standup.md", []index.EdgeRow{
		{
			Type:       "contains",
			SourceID:   "standup",
			TargetID:   "standup#section-a",
			SourcePath: "standup.md",
			Kind:       "structural",
			Source:     sql.NullString{String: "markdown", Valid: true},
		},
	}); err != nil {
		test.Fatalf("upsert markdown edge: %v", err)
	}

	seeds := []graphexpand.Candidate{
		{NodeID: "project-launch"},
		{NodeID: "standup"},
	}

	cases := []struct {
		name     string
		input    string
		wantHop1 []string // neighbors expected at hop 1 (excludes seeds)
	}{
		{
			name:     "bare matches union",
			input:    "contains",
			wantHop1: []string{"standup#section-a", "task-a"},
		},
		{
			name:     "user-scope matches user only",
			input:    ":contains",
			wantHop1: []string{"task-a"},
		},
		{
			name:     "source-scope matches markdown only",
			input:    "markdown:contains",
			wantHop1: []string{"standup#section-a"},
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			ref, parseErr := typeref.Parse(testCase.input)

			if parseErr != nil {
				test.Fatalf("typeref.Parse(%q): %v", testCase.input, parseErr)
			}

			walker := graphexpand.NewWalker(edges, []typeref.EdgeRef{ref}, 1)

			candidates, _, expandErr := walker.Expand(context.Background(), seeds)

			if expandErr != nil {
				test.Fatalf("Walker.Expand: %v", expandErr)
			}

			gotHop1 := []string{}

			for _, candidate := range candidates {
				if candidate.Distance == 1 {
					gotHop1 = append(gotHop1, candidate.NodeID)
				}
			}

			sort.Strings(gotHop1)
			want := append([]string(nil), testCase.wantHop1...)
			sort.Strings(want)

			if len(gotHop1) != len(want) {
				test.Fatalf("hop-1 candidates = %v, want %v", gotHop1, want)
			}

			for idx, id := range want {
				if gotHop1[idx] != id {
					test.Errorf("hop-1[%d] = %q, want %q (full=%v)", idx, gotHop1[idx], id, gotHop1)
				}
			}
		})
	}
}
