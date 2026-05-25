package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func TestDoctor_RendersPropertyTypeMismatch(test *testing.T) {
	root := newWorkspaceWithNodeTypes(test)

	if mkErr := os.MkdirAll(filepath.Join(root, "tickets"), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	if writeErr := os.WriteFile(filepath.Join(root, "tickets/bar.md"), []byte(`---
type: ticket
summary: hi
priority: high
---
`), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	if _, _, reindexOk := runCLISplit(root, "reindex"); !reindexOk {
		test.Fatalf("reindex failed")
	}

	stdout, _, ok := runCLISplit(root, "doctor")

	if !ok {
		test.Errorf("exit non-zero, want 0")
	}

	if !strings.Contains(stdout.String(), "type-mismatch") {
		test.Errorf("stdout = %q, want mention of type-mismatch", stdout.String())
	}

	if !strings.Contains(stdout.String(), "tickets/bar") {
		test.Errorf("stdout = %q, want mention of tickets/bar", stdout.String())
	}
}

func TestDoctor_PrintsCleanReport(test *testing.T) {
	root := setupTempWorkspace(test)

	chdir(test, root)
	defer chdir(test, "")

	out, runErr := runCLI("doctor")

	if runErr != nil {
		test.Fatalf("CLI: %v\n%s", runErr, out)
	}

	if !strings.Contains(out, "no issues") {
		test.Errorf("expected 'no issues', got:\n%s", out)
	}
}

func TestDoctor_RendersWorkflowViolation(test *testing.T) {
	root := newWorkspaceWithWorkflow(test)

	// Use mustCreateNode to create a node with off-schema status.
	mustCreateNode(test, root, "tickets/foo", "ticket", map[string]string{"status": "blocked"})

	stdout, _, ok := runCLISplit(root, "doctor")

	if !ok {
		test.Errorf("exit non-zero, want 0")
	}

	if !strings.Contains(stdout.String(), "workflow-violation") {
		test.Errorf("stdout = %q, want mention of workflow-violation", stdout.String())
	}

	if !strings.Contains(stdout.String(), "tickets/foo") {
		test.Errorf("stdout = %q, want mention of tickets/foo", stdout.String())
	}
}

func TestDoctor_PrintsEmbedStatsBlock(test *testing.T) {
	tmpDir := initWorkspace(test)

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_ = json.NewEncoder(writer).Encode(map[string]any{"embedding": []float64{1, 0, 0}})
	}))

	defer server.Close()

	manifestBody := `[workspace]
name = "test"

[embeddings]
provider = "ollama"
model = "test-model"
endpoint = "` + server.URL + `"
dim = 3
`

	if writeErr := os.WriteFile(filepath.Join(tmpDir, "tusk.toml"), []byte(manifestBody), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	if mkErr := os.MkdirAll(filepath.Join(tmpDir, "notes"), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	if writeErr := os.WriteFile(filepath.Join(tmpDir, "notes/a.md"), []byte("---\ntype: note\ntitle: A\n---\n\nBody for A.\n"), 0o644); writeErr != nil {
		test.Fatalf("write A: %v", writeErr)
	}

	reindexCmd := newRootCmd()
	reindexCmd.SetArgs([]string{"reindex"})

	if execErr := reindexCmd.Execute(); execErr != nil {
		test.Fatalf("reindex: %v", execErr)
	}

	out := &bytes.Buffer{}

	doctorCmd := newRootCmd()
	doctorCmd.SetOut(out)
	doctorCmd.SetErr(out)
	doctorCmd.SetArgs([]string{"doctor"})

	if execErr := doctorCmd.Execute(); execErr != nil {
		test.Fatalf("doctor: %v\nout:\n%s", execErr, out.String())
	}

	if !strings.Contains(out.String(), "embed stats:") {
		test.Errorf("output missing 'embed stats:' line:\n%s", out.String())
	}

	if !strings.Contains(out.String(), "top by chunks:") {
		test.Errorf("output missing 'top by chunks:' block:\n%s", out.String())
	}
}

func TestDoctor_AutoMigratesLegacyCLIRows(test *testing.T) {
	dir := initWorkspaceWithManifest(test, edgeManifestBody())

	createNode(test, dir, "tickets/a.md", "ticket", "A", "")
	createNode(test, dir, "tickets/b.md", "ticket", "B", "")

	// Seed a legacy __cli__ row directly.
	store, openErr := index.Open(filepath.Join(dir, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open: %v", openErr)
	}

	edgeRepo := index.NewEdgeRepo(store)

	if upsertErr := edgeRepo.UpsertAll("tickets/a", index.CLISourcePath, []index.EdgeRow{
		{Type: "blocks", SourceID: "tickets/a", TargetID: "tickets/b", SourcePath: index.CLISourcePath},
	}); upsertErr != nil {
		test.Fatalf("seed __cli__: %v", upsertErr)
	}

	store.Close()

	chdir(test, dir)

	cmd := newRootCmd()
	output := &bytes.Buffer{}
	cmd.SetOut(output)
	cmd.SetErr(output)
	cmd.SetArgs([]string{"doctor"})

	if execErr := cmd.Execute(); execErr != nil {
		test.Fatalf("doctor: %v", execErr)
	}

	body, _ := os.ReadFile(filepath.Join(dir, "tickets/a.md"))

	if !strings.Contains(string(body), "blocks: tickets/b") {
		test.Errorf("doctor should have migrated the legacy CLI edge into frontmatter, got:\n%s", body)
	}

	// The legacy row should have been deleted.
	store2, _ := index.Open(filepath.Join(dir, ".tusk", "index.db"))
	defer store2.Close()

	rows, _ := index.NewEdgeRepo(store2).ListBySource("tickets/a")

	for _, row := range rows {
		if row.SourcePath == index.CLISourcePath {
			test.Errorf("expected legacy __cli__ row to be removed; still have: %+v", row)
		}
	}

	if !strings.Contains(output.String(), "migrated") {
		test.Errorf("doctor should report the migration; got: %s", output.String())
	}
}

func TestDoctor_AutoMigratesLegacyMCPRows(test *testing.T) {
	dir := initWorkspaceWithManifest(test, edgeManifestBody())

	createNode(test, dir, "tickets/a.md", "ticket", "A", "")
	createNode(test, dir, "tickets/b.md", "ticket", "B", "")

	store, openErr := index.Open(filepath.Join(dir, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open: %v", openErr)
	}

	edgeRepo := index.NewEdgeRepo(store)

	if upsertErr := edgeRepo.UpsertAll("tickets/a", index.MCPSourcePath, []index.EdgeRow{
		{Type: "blocks", SourceID: "tickets/a", TargetID: "tickets/b", SourcePath: index.MCPSourcePath},
	}); upsertErr != nil {
		test.Fatalf("seed __mcp__: %v", upsertErr)
	}

	store.Close()

	chdir(test, dir)

	cmd := newRootCmd()
	output := &bytes.Buffer{}
	cmd.SetOut(output)
	cmd.SetErr(output)
	cmd.SetArgs([]string{"doctor"})

	if execErr := cmd.Execute(); execErr != nil {
		test.Fatalf("doctor: %v", execErr)
	}

	body, _ := os.ReadFile(filepath.Join(dir, "tickets/a.md"))

	if !strings.Contains(string(body), "blocks: tickets/b") {
		test.Errorf("doctor should have migrated the legacy MCP edge into frontmatter, got:\n%s", body)
	}

	store2, _ := index.Open(filepath.Join(dir, ".tusk", "index.db"))
	defer store2.Close()

	rows, _ := index.NewEdgeRepo(store2).ListBySource("tickets/a")

	for _, row := range rows {
		if row.SourcePath == index.MCPSourcePath {
			test.Errorf("expected legacy __mcp__ row to be removed; still have: %+v", row)
		}
	}

	if !strings.Contains(output.String(), "migrated") {
		test.Errorf("doctor should report the migration; got: %s", output.String())
	}
}

func TestDoctor_AutoMigratesMixedCLIAndMCPRows(test *testing.T) {
	dir := initWorkspaceWithManifest(test, edgeManifestBody())

	createNode(test, dir, "tickets/a.md", "ticket", "A", "")
	createNode(test, dir, "tickets/b.md", "ticket", "B", "")
	createNode(test, dir, "tickets/c.md", "ticket", "C", "")

	store, openErr := index.Open(filepath.Join(dir, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open: %v", openErr)
	}

	edgeRepo := index.NewEdgeRepo(store)

	// Seed a legacy __cli__ row (blocks: a → b) and a legacy __mcp__ row
	// (parent: a → c) for the same source node.
	if upsertErr := edgeRepo.UpsertAll("tickets/a", index.CLISourcePath, []index.EdgeRow{
		{Type: "blocks", SourceID: "tickets/a", TargetID: "tickets/b", SourcePath: index.CLISourcePath},
	}); upsertErr != nil {
		test.Fatalf("seed __cli__: %v", upsertErr)
	}

	if upsertErr := edgeRepo.UpsertAll("tickets/a", index.MCPSourcePath, []index.EdgeRow{
		{Type: "parent", SourceID: "tickets/a", TargetID: "tickets/c", SourcePath: index.MCPSourcePath},
	}); upsertErr != nil {
		test.Fatalf("seed __mcp__: %v", upsertErr)
	}

	store.Close()

	chdir(test, dir)

	cmd := newRootCmd()
	output := &bytes.Buffer{}
	cmd.SetOut(output)
	cmd.SetErr(output)
	cmd.SetArgs([]string{"doctor"})

	if execErr := cmd.Execute(); execErr != nil {
		test.Fatalf("doctor: %v", execErr)
	}

	body, _ := os.ReadFile(filepath.Join(dir, "tickets/a.md"))

	if !strings.Contains(string(body), "blocks: tickets/b") {
		test.Errorf("doctor should have migrated the legacy CLI edge into frontmatter, got:\n%s", body)
	}

	if !strings.Contains(string(body), "parent: tickets/c") {
		test.Errorf("doctor should have migrated the legacy MCP edge into frontmatter, got:\n%s", body)
	}

	// Both legacy rows should have been deleted.
	store2, _ := index.Open(filepath.Join(dir, ".tusk", "index.db"))
	defer store2.Close()

	rows, _ := index.NewEdgeRepo(store2).ListBySource("tickets/a")

	for _, row := range rows {
		if row.SourcePath == index.CLISourcePath || row.SourcePath == index.MCPSourcePath {
			test.Errorf("expected legacy sentinel rows to be removed; still have: %+v", row)
		}
	}

	// Report should mention both sentinels so the user can tell origin.
	if !strings.Contains(output.String(), "[__cli__]") {
		test.Errorf("doctor output should mention [__cli__] sentinel; got: %s", output.String())
	}

	if !strings.Contains(output.String(), "[__mcp__]") {
		test.Errorf("doctor output should mention [__mcp__] sentinel; got: %s", output.String())
	}
}

func TestDoctor_NoMigrateFlagSkipsMutation(test *testing.T) {
	dir := initWorkspaceWithManifest(test, edgeManifestBody())

	createNode(test, dir, "tickets/a.md", "ticket", "A", "")
	createNode(test, dir, "tickets/b.md", "ticket", "B", "")

	store, openErr := index.Open(filepath.Join(dir, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open: %v", openErr)
	}

	edgeRepo := index.NewEdgeRepo(store)

	if upsertErr := edgeRepo.UpsertAll("tickets/a", index.CLISourcePath, []index.EdgeRow{
		{Type: "blocks", SourceID: "tickets/a", TargetID: "tickets/b", SourcePath: index.CLISourcePath},
	}); upsertErr != nil {
		test.Fatalf("seed __cli__: %v", upsertErr)
	}

	store.Close()

	chdir(test, dir)

	originalBody, _ := os.ReadFile(filepath.Join(dir, "tickets/a.md"))

	cmd := newRootCmd()
	output := &bytes.Buffer{}
	cmd.SetOut(output)
	cmd.SetErr(output)
	cmd.SetArgs([]string{"doctor", "--no-migrate"})

	if execErr := cmd.Execute(); execErr != nil {
		test.Fatalf("doctor: %v", execErr)
	}

	body, _ := os.ReadFile(filepath.Join(dir, "tickets/a.md"))

	if string(body) != string(originalBody) {
		test.Errorf("doctor --no-migrate should leave frontmatter unchanged; before:\n%s\nafter:\n%s", originalBody, body)
	}

	store2, _ := index.Open(filepath.Join(dir, ".tusk", "index.db"))
	defer store2.Close()

	rows, _ := index.NewEdgeRepo(store2).ListBySource("tickets/a")

	var sawLegacy bool

	for _, row := range rows {
		if row.SourcePath == index.CLISourcePath {
			sawLegacy = true
		}
	}

	if !sawLegacy {
		test.Errorf("doctor --no-migrate should leave the legacy __cli__ row in place; rows: %+v", rows)
	}

	if strings.Contains(output.String(), "migrated") {
		test.Errorf("doctor --no-migrate should not announce migrations; got: %s", output.String())
	}

	if !strings.Contains(output.String(), "legacy-cli-edge") {
		test.Errorf("doctor --no-migrate should surface legacy __cli__ rows as drift; got:\n%s", output.String())
	}

	if !strings.Contains(output.String(), "tickets/a") {
		test.Errorf("doctor --no-migrate drift line should mention the source node; got:\n%s", output.String())
	}
}

func TestDoctor_AutoMigrateSkipsRowsWithMissingSourceFile(test *testing.T) {
	dir := initWorkspaceWithManifest(test, edgeManifestBody())

	// Only the target exists; the source node was never written to disk.
	createNode(test, dir, "tickets/b.md", "ticket", "B", "")

	store, openErr := index.Open(filepath.Join(dir, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open: %v", openErr)
	}

	// Seed a placeholder node row for the ghost source so the P2 FK on
	// edges.source_id is satisfied; the test's premise (the source
	// markdown FILE is absent on disk) still holds — only the index row
	// is present, mirroring what a legacy __cli__-only state would
	// produce after the FK migration.
	nodeRepo := index.NewNodeRepo(store)

	if upsertErr := nodeRepo.Upsert(index.NodeRow{
		ID:             "tickets/ghost",
		Type:           "ticket",
		Path:           "tickets/ghost.md",
		Title:          "ghost",
		PropertiesJSON: "{}",
		LastChecksum:   "x",
	}); upsertErr != nil {
		test.Fatalf("seed ghost node: %v", upsertErr)
	}

	edgeRepo := index.NewEdgeRepo(store)

	if upsertErr := edgeRepo.UpsertAll("tickets/ghost", index.CLISourcePath, []index.EdgeRow{
		{Type: "blocks", SourceID: "tickets/ghost", TargetID: "tickets/b", SourcePath: index.CLISourcePath},
	}); upsertErr != nil {
		test.Fatalf("seed __cli__: %v", upsertErr)
	}

	store.Close()

	chdir(test, dir)

	cmd := newRootCmd()
	output := &bytes.Buffer{}
	cmd.SetOut(output)
	cmd.SetErr(output)
	cmd.SetArgs([]string{"doctor"})

	if execErr := cmd.Execute(); execErr != nil {
		test.Fatalf("doctor: %v\noutput:\n%s", execErr, output.String())
	}

	if !strings.Contains(output.String(), "skipped") {
		test.Errorf("doctor should log a skip for missing source file; got:\n%s", output.String())
	}

	// The legacy row must stay in place — no silent data loss.
	store2, _ := index.Open(filepath.Join(dir, ".tusk", "index.db"))
	defer store2.Close()

	rows, _ := index.NewEdgeRepo(store2).ListBySource("tickets/ghost")

	var sawLegacy bool

	for _, row := range rows {
		if row.SourcePath == index.CLISourcePath {
			sawLegacy = true
		}
	}

	if !sawLegacy {
		test.Errorf("doctor should preserve the legacy row for the missing source; rows: %+v", rows)
	}
}

func TestDoctor_AutoMigrateIsIdempotent(test *testing.T) {
	dir := initWorkspaceWithManifest(test, edgeManifestBody())

	createNode(test, dir, "tickets/a.md", "ticket", "A", "")
	createNode(test, dir, "tickets/b.md", "ticket", "B", "")

	store, openErr := index.Open(filepath.Join(dir, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open: %v", openErr)
	}

	edgeRepo := index.NewEdgeRepo(store)

	if upsertErr := edgeRepo.UpsertAll("tickets/a", index.CLISourcePath, []index.EdgeRow{
		{Type: "blocks", SourceID: "tickets/a", TargetID: "tickets/b", SourcePath: index.CLISourcePath},
	}); upsertErr != nil {
		test.Fatalf("seed __cli__: %v", upsertErr)
	}

	store.Close()

	chdir(test, dir)

	// First run: should migrate.
	firstCmd := newRootCmd()
	firstOut := &bytes.Buffer{}
	firstCmd.SetOut(firstOut)
	firstCmd.SetErr(firstOut)
	firstCmd.SetArgs([]string{"doctor"})

	if execErr := firstCmd.Execute(); execErr != nil {
		test.Fatalf("first doctor: %v", execErr)
	}

	if !strings.Contains(firstOut.String(), "migrated") {
		test.Fatalf("first doctor should migrate, got:\n%s", firstOut.String())
	}

	// Second run: no-op, no migration count in output.
	secondCmd := newRootCmd()
	secondOut := &bytes.Buffer{}
	secondCmd.SetOut(secondOut)
	secondCmd.SetErr(secondOut)
	secondCmd.SetArgs([]string{"doctor"})

	if execErr := secondCmd.Execute(); execErr != nil {
		test.Fatalf("second doctor: %v", execErr)
	}

	if strings.Contains(secondOut.String(), "migrated ") {
		test.Errorf("second doctor should be a no-op; got:\n%s", secondOut.String())
	}
}

func TestDoctor_OmitsEmbedStatsWithoutConfig(test *testing.T) {
	// initWorkspace creates a workspace without an [embeddings] section, so
	// loaded.Embeddings.Provider == "" and the stats branch must be skipped.
	_ = initWorkspace(test)

	out := &bytes.Buffer{}

	doctorCmd := newRootCmd()
	doctorCmd.SetOut(out)
	doctorCmd.SetErr(out)
	doctorCmd.SetArgs([]string{"doctor"})

	if execErr := doctorCmd.Execute(); execErr != nil {
		test.Fatalf("doctor: %v", execErr)
	}

	if strings.Contains(out.String(), "embed stats:") {
		test.Errorf("output includes 'embed stats:' when no embeddings configured:\n%s", out.String())
	}
}
