package tui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
)

func TestResolveSummaryMode(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantMode summaryMode
		wantID   string
	}{
		{name: "no args is roots", args: nil, wantMode: summaryModeRoots},
		{name: "lower hex 8 is single", args: []string{"a3f8b2c1"}, wantMode: summaryModeSingle, wantID: "a3f8b2c1"},
		{name: "upper hex 8 is single", args: []string{"AAAA1234"}, wantMode: summaryModeSingle, wantID: "AAAA1234"},
		{name: "all digits 8 is single", args: []string{"12345678"}, wantMode: summaryModeSingle, wantID: "12345678"},
		{name: "non-hex 8 chars falls through to filter", args: []string{"bogusXYZ"}, wantMode: summaryModeFilter},
		{name: "9 hex chars is filter", args: []string{"abcdef012"}, wantMode: summaryModeFilter},
		{name: "single key=value is filter", args: []string{"level=story"}, wantMode: summaryModeFilter},
		{name: "tag positional is filter", args: []string{"+urgent"}, wantMode: summaryModeFilter},
		{name: "two args is filter", args: []string{"a3f8b2c1", "level=story"}, wantMode: summaryModeFilter},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotMode, gotID := resolveSummaryMode(tc.args)
			if gotMode != tc.wantMode {
				t.Fatalf("mode: got %d, want %d", gotMode, tc.wantMode)
			}
			if gotID != tc.wantID {
				t.Fatalf("id: got %q, want %q", gotID, tc.wantID)
			}
		})
	}
}

func TestComputeTotals_Empty(t *testing.T) {
	got := computeTotals(nil)
	if got == nil {
		t.Fatal("computeTotals(nil) must return non-nil")
	}
	if got.Done != 0 || got.Total != 0 || got.Percent != 0 {
		t.Fatalf("zero rollup expected, got %+v", got)
	}
	if got.StatusCounts == nil {
		t.Fatal("StatusCounts must be a non-nil empty slice")
	}
	if len(got.StatusCounts) != 0 {
		t.Fatalf("expected zero buckets, got %d", len(got.StatusCounts))
	}
}

func TestComputeTotals_SingleBlock(t *testing.T) {
	block := &domain.SummaryBlock{
		Rollup: domain.Rollup{
			Done:    2,
			Total:   5,
			Percent: 0.4,
			StatusCounts: []domain.StatusCount{
				{Name: "pending", Count: 3},
				{Name: "completed", Count: 2},
			},
		},
	}
	got := computeTotals([]*domain.SummaryBlock{block})
	if got.Done != 2 || got.Total != 5 {
		t.Fatalf("counts: got Done=%d Total=%d", got.Done, got.Total)
	}
	if got.Percent != 0.4 {
		t.Fatalf("percent: got %v", got.Percent)
	}
	if len(got.StatusCounts) != 2 || got.StatusCounts[0].Name != "pending" || got.StatusCounts[1].Name != "completed" {
		t.Fatalf("buckets out of order: %+v", got.StatusCounts)
	}
}

func TestComputeTotals_MergeBuckets(t *testing.T) {
	blocks := []*domain.SummaryBlock{
		{Rollup: domain.Rollup{
			Done: 1, Total: 3,
			StatusCounts: []domain.StatusCount{
				{Name: "pending", Count: 2},
				{Name: "completed", Count: 1},
			},
		}},
		{Rollup: domain.Rollup{
			Done: 2, Total: 4,
			StatusCounts: []domain.StatusCount{
				{Name: "active", Count: 2},
				{Name: "completed", Count: 2},
			},
		}},
	}
	got := computeTotals(blocks)
	if got.Done != 3 || got.Total != 7 {
		t.Fatalf("totals: got Done=%d Total=%d, want 3 and 7", got.Done, got.Total)
	}
	wantPercent := float64(3) / float64(7)
	if got.Percent != wantPercent {
		t.Fatalf("percent: got %v want %v", got.Percent, wantPercent)
	}

	wantNames := []string{"pending", "completed", "active"}
	if len(got.StatusCounts) != len(wantNames) {
		t.Fatalf("bucket count: got %d want %d", len(got.StatusCounts), len(wantNames))
	}
	for i, want := range wantNames {
		if got.StatusCounts[i].Name != want {
			t.Fatalf("bucket %d: got %q want %q", i, got.StatusCounts[i].Name, want)
		}
	}
	wantCounts := map[string]int{"pending": 2, "active": 2, "completed": 3}
	for _, sc := range got.StatusCounts {
		if sc.Count != wantCounts[sc.Name] {
			t.Fatalf("bucket %s: got count %d want %d", sc.Name, sc.Count, wantCounts[sc.Name])
		}
	}
}

func TestComputeTotals_ZeroPercentWhenNoTotal(t *testing.T) {
	blocks := []*domain.SummaryBlock{
		{Rollup: domain.Rollup{Done: 0, Total: 0, StatusCounts: []domain.StatusCount{}}},
	}
	got := computeTotals(blocks)
	if got.Percent != 0 {
		t.Fatalf("percent: got %v want 0", got.Percent)
	}
	if got.StatusCounts == nil {
		t.Fatal("StatusCounts must be non-nil")
	}
}

func TestRenderBlockText_Branch(t *testing.T) {
	taskID := uuid.New()
	level := "milestone"
	block := &domain.SummaryBlock{
		Task: &domain.Task{
			ID:      taskID,
			ShortID: "abc12345",
			Title:   "Implement v0.13 milestone",
			Status:  "active",
			Level:   &level,
		},
		Rollup: domain.Rollup{
			Done: 3, Total: 5, Percent: 0.6,
			StatusCounts: []domain.StatusCount{
				{Name: "pending", Count: 1},
				{Name: "active", Count: 1},
				{Name: "completed", Count: 3},
			},
		},
	}
	r := NewRenderer(nil, "text", false, nil)
	var buf bytes.Buffer
	if err := renderBlockText(&buf, r, block); err != nil {
		t.Fatalf("renderBlockText: %v", err)
	}
	got := buf.String()
	want := "abc12345  Implement v0.13 milestone\n" +
		"  status:    active\n" +
		"  level:     milestone\n" +
		"  progress:  3/5 done, 60%\n" +
		"  breakdown: pending: 1, active: 1, completed: 3\n"
	if got != want {
		t.Fatalf("renderBlockText output mismatch\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}

func TestRenderBlockText_LeafNoLevel(t *testing.T) {
	block := &domain.SummaryBlock{
		Task: &domain.Task{
			ShortID: "deadbeef",
			Title:   "Plain task",
			Status:  "pending",
		},
		Rollup: domain.Rollup{
			// Empty rollup — no Total, no buckets.
			StatusCounts: []domain.StatusCount{},
		},
	}
	r := NewRenderer(nil, "text", false, nil)
	var buf bytes.Buffer
	if err := renderBlockText(&buf, r, block); err != nil {
		t.Fatalf("renderBlockText: %v", err)
	}
	got := buf.String()
	if strings.Contains(got, "level:") {
		t.Fatalf("expected no level: line, got:\n%s", got)
	}
	if strings.Contains(got, "breakdown:") {
		t.Fatalf("expected no breakdown: line for empty rollup, got:\n%s", got)
	}
	if !strings.Contains(got, "0/0 done, –%") {
		t.Fatalf("expected empty progress dash, got:\n%s", got)
	}
}

func TestRenderTotalsText_Basic(t *testing.T) {
	totals := &domain.Rollup{
		Done: 7, Total: 9, Percent: 7.0 / 9.0,
		StatusCounts: []domain.StatusCount{
			{Name: "pending", Count: 1},
			{Name: "active", Count: 1},
			{Name: "completed", Count: 7},
		},
	}
	r := NewRenderer(nil, "text", false, nil)
	var buf bytes.Buffer
	if err := renderTotalsText(&buf, r, totals); err != nil {
		t.Fatalf("renderTotalsText: %v", err)
	}
	got := buf.String()
	want := "TOTALS    7/9 done, 78%\n" +
		"          pending: 1, active: 1, completed: 7\n"
	if got != want {
		t.Fatalf("renderTotalsText output mismatch\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}
