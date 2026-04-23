package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/service"
	"github.com/mark3labs/mcp-go/mcp"
)

// seedBackendProject inserts a "backend" project row through the service so
// subsequent handler tests have something to modify/delete.
func seedBackendProject(t *testing.T, srv *Server) *domain.Project {
	t.Helper()
	wf, err := srv.workflowSvc.GetByName(context.Background(), "kanban")
	if err != nil {
		t.Fatalf("resolving kanban workflow: %v", err)
	}
	p, err := srv.projectSvc.Create(context.Background(), service.CreateProjectInput{
		Name:       "backend",
		WorkflowID: wf.ID,
	})
	if err != nil {
		t.Fatalf("seed backend: %v", err)
	}
	return p
}

func TestHandleProjectCreate_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":     "backend",
			"workflow": "kanban",
			"urgency": map[string]any{
				"due_weight":      15.0,
				"blocking_weight": 20.0,
			},
			"auto_complete": map[string]any{
				"trigger_status": "completed",
				"target_status":  "completed",
			},
		}},
	}
	res, err := srv.HandleProjectCreateForTest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleProjectCreateForTest: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].(mcp.TextContent).Text)
	}

	p, err := srv.projectSvc.GetByName(context.Background(), "backend")
	if err != nil {
		t.Fatalf("GetByName backend: %v", err)
	}
	if p.Settings.Urgency == nil || p.Settings.Urgency.DueWeight == nil || *p.Settings.Urgency.DueWeight != 15.0 {
		t.Fatalf("due_weight override not persisted: %+v", p.Settings.Urgency)
	}
	if p.Settings.AutoCompleteParent == nil || p.Settings.AutoCompleteParent.TriggerStatus != "completed" {
		t.Fatalf("auto_complete not persisted: %+v", p.Settings.AutoCompleteParent)
	}
}

func TestHandleProjectCreate_UnknownWorkflow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":     "frontend",
			"workflow": "ghost",
		}},
	}
	res, _ := srv.HandleProjectCreateForTest(context.Background(), req)
	if !res.IsError {
		t.Fatalf("expected validation error for unknown workflow")
	}
}

func TestHandleProjectModify_SetAndDelta(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)
	p := seedBackendProject(t, srv)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":    "backend",
			"version": float64(p.Version),
			"urgency_set": map[string]any{
				"blocking_weight": 25.0,
			},
			"urgency_delta": map[string]any{
				"due_weight": 3.0,
			},
		}},
	}
	res, err := srv.HandleProjectModifyForTest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleProjectModifyForTest: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].(mcp.TextContent).Text)
	}

	got, err := srv.projectSvc.GetByName(context.Background(), "backend")
	if err != nil {
		t.Fatalf("GetByName backend: %v", err)
	}
	if got.Settings.Urgency == nil || got.Settings.Urgency.BlockingWeight == nil || *got.Settings.Urgency.BlockingWeight != 25.0 {
		t.Fatalf("blocking_weight set failed: %+v", got.Settings.Urgency)
	}
	if got.Settings.Urgency.DueWeight == nil {
		t.Fatalf("due_weight delta failed: %+v", got.Settings.Urgency)
	}
}

func TestHandleProjectModify_SetDeltaConflict(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)
	p := seedBackendProject(t, srv)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":          "backend",
			"version":       float64(p.Version),
			"urgency_set":   map[string]any{"due_weight": 10.0},
			"urgency_delta": map[string]any{"due_weight": 2.0},
		}},
	}
	res, _ := srv.HandleProjectModifyForTest(context.Background(), req)
	if !res.IsError {
		t.Fatalf("expected conflict error")
	}
}

func TestHandleProjectDelete_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)
	p := seedBackendProject(t, srv)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":    "backend",
			"version": float64(p.Version),
		}},
	}
	res, err := srv.HandleProjectDeleteForTest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleProjectDeleteForTest: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].(mcp.TextContent).Text)
	}
	if _, err := srv.projectSvc.GetByName(context.Background(), "backend"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("backend still present after delete: err=%v", err)
	}
}

func TestHandleProjectModify_BlockedFieldRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)
	srv.cfg.BlockedFields = map[string][]string{
		"tusk_project_modify": {"workflow"},
	}
	p := seedBackendProject(t, srv)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":     "backend",
			"version":  float64(p.Version),
			"workflow": "kanban",
		}},
	}
	res, err := srv.HandleProjectModifyForTest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleProjectModifyForTest: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected block error, got success")
	}
	msg := res.Content[0].(mcp.TextContent).Text
	if !strings.Contains(msg, "mcp.blocked_fields.tusk_project_modify") {
		t.Errorf("error message missing config-key hint: %q", msg)
	}

	got, err := srv.projectSvc.GetByName(context.Background(), "backend")
	if err != nil {
		t.Fatalf("GetByName backend: %v", err)
	}
	if got.Version != p.Version {
		t.Errorf("service was called despite block: version %d -> %d", p.Version, got.Version)
	}
}

func TestHandleProjectModify_BlockedFieldOmitted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)
	srv.cfg.BlockedFields = map[string][]string{
		"tusk_project_modify": {"workflow"},
	}
	p := seedBackendProject(t, srv)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":    "backend",
			"version": float64(p.Version),
			"urgency_set": map[string]any{
				"due_weight": 7.0,
			},
		}},
	}
	res, err := srv.HandleProjectModifyForTest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleProjectModifyForTest: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error when blocked field omitted: %s", res.Content[0].(mcp.TextContent).Text)
	}
}

// extractProjectResponse deserializes the "project" field out of the JSON
// result body produced by handleProjectModify. Keeps the tax-tristate test
// cases concise by localizing the JSON wrangling.
func extractProjectResponse(t *testing.T, res *mcp.CallToolResult) projectResponse {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		t.Fatalf("empty tool result")
	}
	text, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("unexpected content type %T", res.Content[0])
	}
	var body struct {
		Project projectResponse `json:"project"`
	}
	if err := json.Unmarshal([]byte(text.Text), &body); err != nil {
		t.Fatalf("decoding tool result JSON: %v\npayload: %s", err, text.Text)
	}
	return body.Project
}

func TestHandleProjectModify_TaxonomyOmittedLeavesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)
	p := seedBackendProject(t, srv)

	// Seed with a project override first.
	initial := domain.Taxonomy{{"milestone"}, {"story"}}
	set := initial
	if _, err := srv.projectSvc.Modify(context.Background(), service.ModifyProjectInput{
		Name: "backend", ExpectedVersion: p.Version,
		Taxonomy: &service.TaxonomyMutation{Value: set},
	}); err != nil {
		t.Fatalf("seed taxonomy: %v", err)
	}

	got, err := srv.projectSvc.GetByName(context.Background(), "backend")
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":    "backend",
			"version": float64(got.Version),
			"urgency_set": map[string]any{
				"due_weight": 7.0,
			},
		}},
	}
	res, err := srv.HandleProjectModifyForTest(context.Background(), req)
	if err != nil {
		t.Fatalf("modify: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].(mcp.TextContent).Text)
	}

	after, err := srv.projectSvc.GetByName(context.Background(), "backend")
	if err != nil {
		t.Fatalf("get-after: %v", err)
	}
	if after.Settings.Taxonomy == nil {
		t.Fatalf("taxonomy override was cleared unexpectedly")
	}
	if !reflect.DeepEqual(*after.Settings.Taxonomy, initial) {
		t.Fatalf("taxonomy override drifted: got %+v, want %+v", *after.Settings.Taxonomy, initial)
	}
}

func TestHandleProjectModify_TaxonomyNullClears(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)
	p := seedBackendProject(t, srv)

	seed := domain.Taxonomy{{"milestone"}, {"story"}}
	if _, err := srv.projectSvc.Modify(context.Background(), service.ModifyProjectInput{
		Name: "backend", ExpectedVersion: p.Version,
		Taxonomy: &service.TaxonomyMutation{Value: seed},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := srv.projectSvc.GetByName(context.Background(), "backend")
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":     "backend",
			"version":  float64(got.Version),
			"taxonomy": nil,
		}},
	}
	res, err := srv.HandleProjectModifyForTest(context.Background(), req)
	if err != nil {
		t.Fatalf("modify: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].(mcp.TextContent).Text)
	}

	after, err := srv.projectSvc.GetByName(context.Background(), "backend")
	if err != nil {
		t.Fatalf("get-after: %v", err)
	}
	if after.Settings.Taxonomy != nil {
		t.Fatalf("expected taxonomy override to be cleared, got %+v", *after.Settings.Taxonomy)
	}

	resp := extractProjectResponse(t, res)
	if resp.Settings.Taxonomy != nil {
		t.Fatalf("response settings.taxonomy should be omitted after clear, got %+v", resp.Settings.Taxonomy)
	}
	if resp.EffectiveTaxonomy.Source != "none" {
		t.Fatalf("effective_taxonomy.source: got %q, want none", resp.EffectiveTaxonomy.Source)
	}
}

func TestHandleProjectModify_TaxonomyEmptyOptsOut(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)
	p := seedBackendProject(t, srv)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":    "backend",
			"version": float64(p.Version),
			"taxonomy": map[string]any{
				"ranks": []any{},
			},
		}},
	}
	res, err := srv.HandleProjectModifyForTest(context.Background(), req)
	if err != nil {
		t.Fatalf("modify: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].(mcp.TextContent).Text)
	}

	after, err := srv.projectSvc.GetByName(context.Background(), "backend")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if after.Settings.Taxonomy == nil {
		t.Fatalf("expected taxonomy override set to &empty, got nil")
	}
	if len(*after.Settings.Taxonomy) != 0 {
		t.Fatalf("expected empty taxonomy override, got %+v", *after.Settings.Taxonomy)
	}

	resp := extractProjectResponse(t, res)
	if resp.Settings.Taxonomy == nil {
		t.Fatalf("response settings.taxonomy should be present for explicit opt-out")
	}
	if len(resp.Settings.Taxonomy.Ranks) != 0 {
		t.Fatalf("response settings.taxonomy.ranks should be empty, got %+v", resp.Settings.Taxonomy.Ranks)
	}
	if resp.EffectiveTaxonomy.Source != "project_override" {
		t.Fatalf("effective_taxonomy.source: got %q, want project_override", resp.EffectiveTaxonomy.Source)
	}
}

func TestHandleProjectModify_TaxonomyPopulatedReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)
	p := seedBackendProject(t, srv)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":    "backend",
			"version": float64(p.Version),
			"taxonomy": map[string]any{
				"ranks": []any{
					[]any{"milestone"},
					[]any{"story"},
					[]any{"task", "spike"},
				},
			},
		}},
	}
	res, err := srv.HandleProjectModifyForTest(context.Background(), req)
	if err != nil {
		t.Fatalf("modify: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].(mcp.TextContent).Text)
	}

	after, err := srv.projectSvc.GetByName(context.Background(), "backend")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	want := domain.Taxonomy{{"milestone"}, {"story"}, {"task", "spike"}}
	if after.Settings.Taxonomy == nil {
		t.Fatalf("expected taxonomy override set, got nil")
	}
	if !reflect.DeepEqual(*after.Settings.Taxonomy, want) {
		t.Fatalf("taxonomy override: got %+v, want %+v", *after.Settings.Taxonomy, want)
	}

	resp := extractProjectResponse(t, res)
	if resp.Settings.Taxonomy == nil {
		t.Fatalf("response settings.taxonomy should be populated")
	}
	if !reflect.DeepEqual(resp.Settings.Taxonomy.Ranks, [][]string(want)) {
		t.Fatalf("response settings.taxonomy.ranks: got %+v, want %+v", resp.Settings.Taxonomy.Ranks, want)
	}
	if !reflect.DeepEqual(resp.EffectiveTaxonomy.Ranks, [][]string(want)) {
		t.Fatalf("effective_taxonomy.ranks: got %+v, want %+v", resp.EffectiveTaxonomy.Ranks, want)
	}
	if resp.EffectiveTaxonomy.Source != "project_override" {
		t.Fatalf("effective_taxonomy.source: got %q, want project_override", resp.EffectiveTaxonomy.Source)
	}
}

func TestHandleProjectModify_TaxonomyRejectsMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)
	p := seedBackendProject(t, srv)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":    "backend",
			"version": float64(p.Version),
			"taxonomy": map[string]any{
				"ranks": []any{
					[]any{"1bad"}, // violates level name pattern
				},
			},
		}},
	}
	res, _ := srv.HandleProjectModifyForTest(context.Background(), req)
	if !res.IsError {
		t.Fatal("expected validation error for invalid level name")
	}
}

func TestHandleProjectModify_TaxonomyBlocked(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)
	srv.cfg.BlockedFields = map[string][]string{
		"tusk_project_modify": {"taxonomy"},
	}
	p := seedBackendProject(t, srv)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":    "backend",
			"version": float64(p.Version),
			"taxonomy": map[string]any{
				"ranks": []any{
					[]any{"milestone"},
				},
			},
		}},
	}
	res, err := srv.HandleProjectModifyForTest(context.Background(), req)
	if err != nil {
		t.Fatalf("modify: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected block error")
	}
	msg := res.Content[0].(mcp.TextContent).Text
	if !strings.Contains(msg, "mcp.blocked_fields.tusk_project_modify") {
		t.Errorf("error message missing config-key hint: %q", msg)
	}

	after, err := srv.projectSvc.GetByName(context.Background(), "backend")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if after.Settings.Taxonomy != nil {
		t.Fatalf("taxonomy override leaked past block: %+v", *after.Settings.Taxonomy)
	}
}

func TestHandleProjectDelete_DefaultGuarded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":    "default",
			"version": float64(1),
		}},
	}
	res, _ := srv.HandleProjectDeleteForTest(context.Background(), req)
	if !res.IsError {
		t.Fatalf("expected guard error for built-in default project")
	}
}
