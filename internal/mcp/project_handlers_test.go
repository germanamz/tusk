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
func seedBackendProject(test *testing.T, srv *Server) *domain.Project {
	test.Helper()

	workflow, workflowErr := srv.workflowSvc.GetByName(context.Background(), "kanban")

	if workflowErr != nil {
		test.Fatalf("resolving kanban workflow: %v", workflowErr)
	}

	project, createErr := srv.projectSvc.Create(context.Background(), service.CreateProjectInput{
		Name:       "backend",
		WorkflowID: workflow.ID,
	})

	if createErr != nil {
		test.Fatalf("seed backend: %v", createErr)
	}

	return project
}

func TestHandleProjectCreate_Success(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(test, path)
	srv := newTestServer(test, path)

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

	res, createErr := srv.HandleProjectCreateForTest(context.Background(), req)

	if createErr != nil {
		test.Fatalf("HandleProjectCreateForTest: %v", createErr)
	}

	if res.IsError {
		test.Fatalf("unexpected error: %s", res.Content[0].(mcp.TextContent).Text)
	}

	project, lookupErr := srv.projectSvc.GetByName(context.Background(), "backend")

	if lookupErr != nil {
		test.Fatalf("GetByName backend: %v", lookupErr)
	}

	if project.Settings.Urgency == nil || project.Settings.Urgency.DueWeight == nil || *project.Settings.Urgency.DueWeight != 15.0 {
		test.Fatalf("due_weight override not persisted: %+v", project.Settings.Urgency)
	}
	if project.Settings.AutoCompleteParent == nil || project.Settings.AutoCompleteParent.TriggerStatus != "completed" {
		test.Fatalf("auto_complete not persisted: %+v", project.Settings.AutoCompleteParent)
	}
}

func TestHandleProjectCreate_UnknownWorkflow(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(test, path)
	srv := newTestServer(test, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":     "frontend",
			"workflow": "ghost",
		}},
	}
	res, _ := srv.HandleProjectCreateForTest(context.Background(), req)
	if !res.IsError {
		test.Fatalf("expected validation error for unknown workflow")
	}
}

func TestHandleProjectCreateModify_Description(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(test, path)
	srv := newTestServer(test, path)

	const desc = "the backend project"
	createReq := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":        "backend",
			"workflow":    "kanban",
			"description": desc,
		}},
	}

	res, createErr := srv.HandleProjectCreateForTest(context.Background(), createReq)

	if createErr != nil {
		test.Fatalf("HandleProjectCreateForTest: %v", createErr)
	}

	if res.IsError {
		test.Fatalf("create error: %s", res.Content[0].(mcp.TextContent).Text)
	}

	body := res.Content[0].(mcp.TextContent).Text
	var payload struct {
		Project projectResponse `json:"project"`
	}

	decodeErr := json.Unmarshal([]byte(body), &payload)

	if decodeErr != nil {
		test.Fatalf("decode create response: %v\n%s", decodeErr, body)
	}

	if payload.Project.Description != desc {
		test.Fatalf("create response Description = %q, want %q", payload.Project.Description, desc)
	}
	if !strings.Contains(body, `"description"`) {
		test.Fatalf("create response missing description key: %s", body)
	}

	project, lookupErr := srv.projectSvc.GetByName(context.Background(), "backend")

	if lookupErr != nil {
		test.Fatalf("GetByName: %v", lookupErr)
	}

	if project.Description != desc {
		test.Fatalf("persisted Description = %q, want %q", project.Description, desc)
	}

	clearReq := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":        "backend",
			"version":     float64(project.Version),
			"description": "",
		}},
	}

	res, modifyErr := srv.HandleProjectModifyForTest(context.Background(), clearReq)

	if modifyErr != nil {
		test.Fatalf("HandleProjectModifyForTest: %v", modifyErr)
	}

	if res.IsError {
		test.Fatalf("modify error: %s", res.Content[0].(mcp.TextContent).Text)
	}

	cleared, clearedErr := srv.projectSvc.GetByName(context.Background(), "backend")

	if clearedErr != nil {
		test.Fatalf("GetByName after clear: %v", clearedErr)
	}

	if cleared.Description != "" {
		test.Fatalf("Description after clear = %q, want empty", cleared.Description)
	}
}

func TestHandleProjectModify_SetAndDelta(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(test, path)
	srv := newTestServer(test, path)
	seeded := seedBackendProject(test, srv)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":    "backend",
			"version": float64(seeded.Version),
			"urgency_set": map[string]any{
				"blocking_weight": 25.0,
			},
			"urgency_delta": map[string]any{
				"due_weight": 3.0,
			},
		}},
	}

	res, modifyErr := srv.HandleProjectModifyForTest(context.Background(), req)

	if modifyErr != nil {
		test.Fatalf("HandleProjectModifyForTest: %v", modifyErr)
	}

	if res.IsError {
		test.Fatalf("unexpected error: %s", res.Content[0].(mcp.TextContent).Text)
	}

	got, lookupErr := srv.projectSvc.GetByName(context.Background(), "backend")

	if lookupErr != nil {
		test.Fatalf("GetByName backend: %v", lookupErr)
	}

	if got.Settings.Urgency == nil || got.Settings.Urgency.BlockingWeight == nil || *got.Settings.Urgency.BlockingWeight != 25.0 {
		test.Fatalf("blocking_weight set failed: %+v", got.Settings.Urgency)
	}
	if got.Settings.Urgency.DueWeight == nil {
		test.Fatalf("due_weight delta failed: %+v", got.Settings.Urgency)
	}
}

func TestHandleProjectModify_SetDeltaConflict(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(test, path)
	srv := newTestServer(test, path)
	seeded := seedBackendProject(test, srv)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":          "backend",
			"version":       float64(seeded.Version),
			"urgency_set":   map[string]any{"due_weight": 10.0},
			"urgency_delta": map[string]any{"due_weight": 2.0},
		}},
	}
	res, _ := srv.HandleProjectModifyForTest(context.Background(), req)
	if !res.IsError {
		test.Fatalf("expected conflict error")
	}
}

func TestHandleProjectDelete_Success(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(test, path)
	srv := newTestServer(test, path)
	seeded := seedBackendProject(test, srv)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":    "backend",
			"version": float64(seeded.Version),
		}},
	}

	res, deleteErr := srv.HandleProjectDeleteForTest(context.Background(), req)

	if deleteErr != nil {
		test.Fatalf("HandleProjectDeleteForTest: %v", deleteErr)
	}

	if res.IsError {
		test.Fatalf("unexpected error: %s", res.Content[0].(mcp.TextContent).Text)
	}
	if _, lookupErr := srv.projectSvc.GetByName(context.Background(), "backend"); !errors.Is(lookupErr, domain.ErrNotFound) {
		test.Fatalf("backend still present after delete: err=%v", lookupErr)
	}
}

func TestHandleProjectModify_BlockedFieldRejected(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(test, path)
	srv := newTestServer(test, path)
	srv.cfg.BlockedFields = map[string][]string{
		"tusk_project_modify": {"workflow"},
	}
	seeded := seedBackendProject(test, srv)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":     "backend",
			"version":  float64(seeded.Version),
			"workflow": "kanban",
		}},
	}

	res, modifyErr := srv.HandleProjectModifyForTest(context.Background(), req)

	if modifyErr != nil {
		test.Fatalf("HandleProjectModifyForTest: %v", modifyErr)
	}

	if !res.IsError {
		test.Fatal("expected block error, got success")
	}
	msg := res.Content[0].(mcp.TextContent).Text
	if !strings.Contains(msg, "mcp.blocked_fields.tusk_project_modify") {
		test.Errorf("error message missing config-key hint: %q", msg)
	}

	got, lookupErr := srv.projectSvc.GetByName(context.Background(), "backend")

	if lookupErr != nil {
		test.Fatalf("GetByName backend: %v", lookupErr)
	}

	if got.Version != seeded.Version {
		test.Errorf("service was called despite block: version %d -> %d", seeded.Version, got.Version)
	}
}

func TestHandleProjectModify_BlockedFieldOmitted(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(test, path)
	srv := newTestServer(test, path)
	srv.cfg.BlockedFields = map[string][]string{
		"tusk_project_modify": {"workflow"},
	}
	seeded := seedBackendProject(test, srv)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":    "backend",
			"version": float64(seeded.Version),
			"urgency_set": map[string]any{
				"due_weight": 7.0,
			},
		}},
	}

	res, modifyErr := srv.HandleProjectModifyForTest(context.Background(), req)

	if modifyErr != nil {
		test.Fatalf("HandleProjectModifyForTest: %v", modifyErr)
	}

	if res.IsError {
		test.Fatalf("unexpected error when blocked field omitted: %s", res.Content[0].(mcp.TextContent).Text)
	}
}

// extractProjectResponse deserializes the "project" field out of the JSON
// result body produced by handleProjectModify. Keeps the tax-tristate test
// cases concise by localizing the JSON wrangling.
func extractProjectResponse(test *testing.T, res *mcp.CallToolResult) projectResponse {
	test.Helper()
	if res == nil || len(res.Content) == 0 {
		test.Fatalf("empty tool result")
	}
	text, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		test.Fatalf("unexpected content type %T", res.Content[0])
	}
	var body struct {
		Project projectResponse `json:"project"`
	}

	decodeErr := json.Unmarshal([]byte(text.Text), &body)

	if decodeErr != nil {
		test.Fatalf("decoding tool result JSON: %v\npayload: %s", decodeErr, text.Text)
	}

	return body.Project
}

func TestHandleProjectModify_TaxonomyOmittedLeavesExisting(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(test, path)
	srv := newTestServer(test, path)
	seeded := seedBackendProject(test, srv)

	// Seed with a project override first.
	initial := domain.Taxonomy{{"milestone"}, {"story"}}
	seedTaxonomy := initial

	_, seedErr := srv.projectSvc.Modify(context.Background(), service.ModifyProjectInput{
		Name: "backend", ExpectedVersion: seeded.Version,
		Taxonomy: &service.TaxonomyMutation{Value: seedTaxonomy},
	})

	if seedErr != nil {
		test.Fatalf("seed taxonomy: %v", seedErr)
	}

	got, gotErr := srv.projectSvc.GetByName(context.Background(), "backend")

	if gotErr != nil {
		test.Fatalf("get: %v", gotErr)
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

	res, modifyErr := srv.HandleProjectModifyForTest(context.Background(), req)

	if modifyErr != nil {
		test.Fatalf("modify: %v", modifyErr)
	}

	if res.IsError {
		test.Fatalf("unexpected error: %s", res.Content[0].(mcp.TextContent).Text)
	}

	after, afterErr := srv.projectSvc.GetByName(context.Background(), "backend")

	if afterErr != nil {
		test.Fatalf("get-after: %v", afterErr)
	}

	if after.Settings.Taxonomy == nil {
		test.Fatalf("taxonomy override was cleared unexpectedly")
	}
	if !reflect.DeepEqual(*after.Settings.Taxonomy, initial) {
		test.Fatalf("taxonomy override drifted: got %+v, want %+v", *after.Settings.Taxonomy, initial)
	}
}

func TestHandleProjectModify_TaxonomyNullClears(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(test, path)
	srv := newTestServer(test, path)
	seeded := seedBackendProject(test, srv)

	seedTaxonomy := domain.Taxonomy{{"milestone"}, {"story"}}

	_, seedErr := srv.projectSvc.Modify(context.Background(), service.ModifyProjectInput{
		Name: "backend", ExpectedVersion: seeded.Version,
		Taxonomy: &service.TaxonomyMutation{Value: seedTaxonomy},
	})

	if seedErr != nil {
		test.Fatalf("seed: %v", seedErr)
	}

	got, gotErr := srv.projectSvc.GetByName(context.Background(), "backend")

	if gotErr != nil {
		test.Fatalf("get: %v", gotErr)
	}

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":     "backend",
			"version":  float64(got.Version),
			"taxonomy": nil,
		}},
	}

	res, modifyErr := srv.HandleProjectModifyForTest(context.Background(), req)

	if modifyErr != nil {
		test.Fatalf("modify: %v", modifyErr)
	}

	if res.IsError {
		test.Fatalf("unexpected error: %s", res.Content[0].(mcp.TextContent).Text)
	}

	after, afterErr := srv.projectSvc.GetByName(context.Background(), "backend")

	if afterErr != nil {
		test.Fatalf("get-after: %v", afterErr)
	}

	if after.Settings.Taxonomy != nil {
		test.Fatalf("expected taxonomy override to be cleared, got %+v", *after.Settings.Taxonomy)
	}

	resp := extractProjectResponse(test, res)
	if resp.Settings.Taxonomy != nil {
		test.Fatalf("response settings.taxonomy should be omitted after clear, got %+v", resp.Settings.Taxonomy)
	}
	if resp.EffectiveTaxonomy.Source != "none" {
		test.Fatalf("effective_taxonomy.source: got %q, want none", resp.EffectiveTaxonomy.Source)
	}
}

func TestHandleProjectModify_TaxonomyEmptyOptsOut(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(test, path)
	srv := newTestServer(test, path)
	seeded := seedBackendProject(test, srv)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":    "backend",
			"version": float64(seeded.Version),
			"taxonomy": map[string]any{
				"ranks": []any{},
			},
		}},
	}

	res, modifyErr := srv.HandleProjectModifyForTest(context.Background(), req)

	if modifyErr != nil {
		test.Fatalf("modify: %v", modifyErr)
	}

	if res.IsError {
		test.Fatalf("unexpected error: %s", res.Content[0].(mcp.TextContent).Text)
	}

	after, afterErr := srv.projectSvc.GetByName(context.Background(), "backend")

	if afterErr != nil {
		test.Fatalf("get: %v", afterErr)
	}

	if after.Settings.Taxonomy == nil {
		test.Fatalf("expected taxonomy override set to &empty, got nil")
	}
	if len(*after.Settings.Taxonomy) != 0 {
		test.Fatalf("expected empty taxonomy override, got %+v", *after.Settings.Taxonomy)
	}

	resp := extractProjectResponse(test, res)
	if resp.Settings.Taxonomy == nil {
		test.Fatalf("response settings.taxonomy should be present for explicit opt-out")
	}
	if len(resp.Settings.Taxonomy.Ranks) != 0 {
		test.Fatalf("response settings.taxonomy.ranks should be empty, got %+v", resp.Settings.Taxonomy.Ranks)
	}
	if resp.EffectiveTaxonomy.Source != "project_override" {
		test.Fatalf("effective_taxonomy.source: got %q, want project_override", resp.EffectiveTaxonomy.Source)
	}
}

func TestHandleProjectModify_TaxonomyPopulatedReplaces(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(test, path)
	srv := newTestServer(test, path)
	seeded := seedBackendProject(test, srv)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":    "backend",
			"version": float64(seeded.Version),
			"taxonomy": map[string]any{
				"ranks": []any{
					[]any{"milestone"},
					[]any{"story"},
					[]any{"task", "spike"},
				},
			},
		}},
	}

	res, modifyErr := srv.HandleProjectModifyForTest(context.Background(), req)

	if modifyErr != nil {
		test.Fatalf("modify: %v", modifyErr)
	}

	if res.IsError {
		test.Fatalf("unexpected error: %s", res.Content[0].(mcp.TextContent).Text)
	}

	after, afterErr := srv.projectSvc.GetByName(context.Background(), "backend")

	if afterErr != nil {
		test.Fatalf("get: %v", afterErr)
	}

	want := domain.Taxonomy{{"milestone"}, {"story"}, {"task", "spike"}}
	if after.Settings.Taxonomy == nil {
		test.Fatalf("expected taxonomy override set, got nil")
	}
	if !reflect.DeepEqual(*after.Settings.Taxonomy, want) {
		test.Fatalf("taxonomy override: got %+v, want %+v", *after.Settings.Taxonomy, want)
	}

	resp := extractProjectResponse(test, res)
	if resp.Settings.Taxonomy == nil {
		test.Fatalf("response settings.taxonomy should be populated")
	}
	if !reflect.DeepEqual(resp.Settings.Taxonomy.Ranks, [][]string(want)) {
		test.Fatalf("response settings.taxonomy.ranks: got %+v, want %+v", resp.Settings.Taxonomy.Ranks, want)
	}
	if !reflect.DeepEqual(resp.EffectiveTaxonomy.Ranks, [][]string(want)) {
		test.Fatalf("effective_taxonomy.ranks: got %+v, want %+v", resp.EffectiveTaxonomy.Ranks, want)
	}
	if resp.EffectiveTaxonomy.Source != "project_override" {
		test.Fatalf("effective_taxonomy.source: got %q, want project_override", resp.EffectiveTaxonomy.Source)
	}
}

func TestHandleProjectModify_TaxonomyRejectsMalformed(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(test, path)
	srv := newTestServer(test, path)
	seeded := seedBackendProject(test, srv)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":    "backend",
			"version": float64(seeded.Version),
			"taxonomy": map[string]any{
				"ranks": []any{
					[]any{"1bad"}, // violates level name pattern
				},
			},
		}},
	}
	res, _ := srv.HandleProjectModifyForTest(context.Background(), req)
	if !res.IsError {
		test.Fatal("expected validation error for invalid level name")
	}
}

func TestHandleProjectModify_TaxonomyBlocked(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(test, path)
	srv := newTestServer(test, path)
	srv.cfg.BlockedFields = map[string][]string{
		"tusk_project_modify": {"taxonomy"},
	}
	seeded := seedBackendProject(test, srv)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":    "backend",
			"version": float64(seeded.Version),
			"taxonomy": map[string]any{
				"ranks": []any{
					[]any{"milestone"},
				},
			},
		}},
	}

	res, modifyErr := srv.HandleProjectModifyForTest(context.Background(), req)

	if modifyErr != nil {
		test.Fatalf("modify: %v", modifyErr)
	}

	if !res.IsError {
		test.Fatal("expected block error")
	}
	msg := res.Content[0].(mcp.TextContent).Text
	if !strings.Contains(msg, "mcp.blocked_fields.tusk_project_modify") {
		test.Errorf("error message missing config-key hint: %q", msg)
	}

	after, afterErr := srv.projectSvc.GetByName(context.Background(), "backend")

	if afterErr != nil {
		test.Fatalf("get: %v", afterErr)
	}

	if after.Settings.Taxonomy != nil {
		test.Fatalf("taxonomy override leaked past block: %+v", *after.Settings.Taxonomy)
	}
}

func TestHandleProjectDelete_DefaultGuarded(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(test, path)
	srv := newTestServer(test, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":    "default",
			"version": float64(1),
		}},
	}
	res, _ := srv.HandleProjectDeleteForTest(context.Background(), req)
	if !res.IsError {
		test.Fatalf("expected guard error for built-in default project")
	}
}
