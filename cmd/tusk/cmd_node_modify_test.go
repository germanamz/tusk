package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNodeModify_SetProperty(test *testing.T) {
	root := setupTempWorkspace(test)

	createNode(test, root, "notes/x.md", "note", "X", "")

	chdir(test, root)
	defer chdir(test, "")

	out, runErr := runCLI("node", "modify", "notes/x", "--prop", "priority=5")

	if runErr != nil {
		test.Fatalf("CLI: %v\n%s", runErr, out)
	}

	body, _ := os.ReadFile(filepath.Join(root, "notes/x.md"))

	if !strings.Contains(string(body), "priority: 5") {
		test.Errorf("file missing priority: 5\n%s", body)
	}
}

func TestNodeModify_UnsetProperty(test *testing.T) {
	root := setupTempWorkspace(test)

	createNode(test, root, "notes/x.md", "note", "X", "")

	chdir(test, root)
	defer chdir(test, "")

	if _, runErr := runCLI("node", "modify", "notes/x", "--prop", "priority=5"); runErr != nil {
		test.Fatalf("set: %v", runErr)
	}

	if _, runErr := runCLI("node", "modify", "notes/x", "--unset", "priority"); runErr != nil {
		test.Fatalf("unset: %v", runErr)
	}

	body, _ := os.ReadFile(filepath.Join(root, "notes/x.md"))

	if strings.Contains(string(body), "priority:") {
		test.Errorf("priority should be removed:\n%s", body)
	}
}

// runCLI is defined in cmd_test_helpers_test.go; setupTempWorkspace, createNode, chdir
// are existing helpers used by other cmd_*_test.go files.
var _ = bytes.Buffer{} // keep import alive when only one helper uses it

// TestParseSetFlags_Float pins C1: a non-whole decimal is typed as float64
// (tried between bool and string), while whole numbers stay int and
// bools/strings are unchanged.
func TestParseSetFlags_Float(test *testing.T) {
	props, err := parseSetFlags([]string{"cost=3.14", "qty=2", "ratio=-1.5", "flag=true", "name=hello"})

	if err != nil {
		test.Fatalf("parseSetFlags: %v", err)
	}

	if cost, ok := props["cost"].(float64); !ok || cost != 3.14 {
		test.Errorf("cost = %#v, want float64(3.14)", props["cost"])
	}

	if _, ok := props["qty"].(int); !ok {
		test.Errorf("qty = %#v, want int (whole numbers stay int)", props["qty"])
	}

	if ratio, ok := props["ratio"].(float64); !ok || ratio != -1.5 {
		test.Errorf("ratio = %#v, want float64(-1.5)", props["ratio"])
	}

	if _, ok := props["flag"].(bool); !ok {
		test.Errorf("flag = %#v, want bool", props["flag"])
	}

	if _, ok := props["name"].(string); !ok {
		test.Errorf("name = %#v, want string", props["name"])
	}
}

// TestNodeModify_FloatPropertyRoundTrip pins C1 end-to-end via the CLI: a float
// property declared in the manifest is set, validated, and rendered back as a
// YAML number.
func TestNodeModify_FloatPropertyRoundTrip(test *testing.T) {
	root := test.TempDir()

	manifestBody := `
[workspace]
name = "test"

[node-types.expense]
properties = [
    { name = "cost", type = "float" },
]
`

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(manifestBody), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	mustCreateNode(test, root, "expenses/lunch", "expense", nil)

	_, stderr, ok := runCLISplit(root, "node", "modify", "expenses/lunch", "--prop", "cost=3.14")

	if !ok {
		test.Fatalf("modify failed: %s", stderr.String())
	}

	body, readErr := os.ReadFile(filepath.Join(root, "expenses/lunch.md"))

	if readErr != nil {
		test.Fatalf("read: %v", readErr)
	}

	if !strings.Contains(string(body), "cost: 3.14") {
		test.Errorf("frontmatter missing float prop; got:\n%s", string(body))
	}
}

// TestNodeModify_DatedTicketAdvancesStatus reproduces issue #662: advancing the
// kanban status of a ticket that carries a correctly quoted date property must
// succeed. The modify render path previously re-emitted the date unquoted, so
// the re-parse resolved it to a time.Time and the validator rejected its own
// round-trip.
func TestNodeModify_DatedTicketAdvancesStatus(test *testing.T) {
	root := test.TempDir()

	manifestBody := `
[workspace]
name = "test"

[node-types.ticket]
properties = [
    { name = "due", type = "date" },
]

[behaviors.workflow.tickets]
applies-to = ["ticket"]
states = [
  { name = "pending", initial = true },
  { name = "active" },
  { name = "completed", terminal = true, done = true },
]
transitions = [
  { from = "pending", to = "active" },
  { from = "active", to = "completed" },
]
`

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(manifestBody), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	// Quoted ISO date, exactly as a hand-authored / reindexed file carries it.
	mustCreateNode(test, root, "tickets/foo", "ticket", map[string]string{
		"status": "pending",
		"due":    `"2026-06-11"`,
	})

	stdout, stderr, ok := runCLISplit(root, "node", "modify", "tickets/foo", "--prop", "status=active")

	if !ok {
		test.Fatalf("modify failed: %s", stderr.String())
	}

	if !strings.Contains(stdout.String(), "Modified tickets/foo") {
		test.Errorf("stdout = %q, want success line", stdout.String())
	}

	body, readErr := os.ReadFile(filepath.Join(root, "tickets/foo.md"))

	if readErr != nil {
		test.Fatalf("read: %v", readErr)
	}

	if !strings.Contains(string(body), "status: active") {
		test.Errorf("frontmatter should show advanced status; got:\n%s", body)
	}

	// The date must remain a quoted string on disk so the next parse keeps it a
	// string (and exact-match queries keep working).
	if !strings.Contains(string(body), `due: "2026-06-11"`) {
		test.Errorf("date should stay quoted; got:\n%s", body)
	}
}

// TestReindex_SelfHealsUnquotedDate pins the auto-self-heal behavior: a ticket
// whose date is unquoted on disk (yaml parses it to a time.Time) is rewritten to
// canonical quoted form by reindex, becomes exact-match queryable, leaves doctor
// clean, and a second reindex is a byte-identical no-op (converges).
func TestReindex_SelfHealsUnquotedDate(test *testing.T) {
	root := test.TempDir()

	manifestBody := `
[workspace]
name = "test"

[node-types.ticket]
properties = [
    { name = "due", type = "date" },
]
`

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(manifestBody), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	if mkErr := os.MkdirAll(filepath.Join(root, "tickets"), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	// Unquoted date, written directly to disk (no reindex yet so we observe the
	// heal). yaml resolves `due: 2026-06-11` to a time.Time.
	unquoted := "---\ntype: ticket\ndue: 2026-06-11\n---\n\nbody\n"

	if writeErr := os.WriteFile(filepath.Join(root, "tickets/foo.md"), []byte(unquoted), 0o644); writeErr != nil {
		test.Fatalf("write node: %v", writeErr)
	}

	if _, stderr, ok := runCLISplit(root, "reindex"); !ok {
		test.Fatalf("reindex: %s", stderr.String())
	}

	body, _ := os.ReadFile(filepath.Join(root, "tickets/foo.md"))

	if !strings.Contains(string(body), `due: "2026-06-11"`) {
		test.Errorf("reindex should self-heal the unquoted date; got:\n%s", body)
	}

	// Formerly-unquoted date is now exact-match queryable.
	stdout, _, ok := runCLISplit(root, "query", "due=2026-06-11", "--json")

	if !ok {
		test.Fatalf("query failed")
	}

	if !strings.Contains(stdout.String(), "tickets/foo") {
		test.Errorf("date should be exact-match queryable; got:\n%s", stdout.String())
	}

	// Doctor reports no date type-mismatch.
	doctorOut, _, _ := runCLISplit(root, "doctor")

	if strings.Contains(doctorOut.String(), "type-mismatch") {
		test.Errorf("doctor should be clean; got:\n%s", doctorOut.String())
	}

	// Convergence: a second reindex leaves the file byte-identical.
	before, _ := os.ReadFile(filepath.Join(root, "tickets/foo.md"))

	if _, stderr, ok := runCLISplit(root, "reindex"); !ok {
		test.Fatalf("second reindex: %s", stderr.String())
	}

	after, _ := os.ReadFile(filepath.Join(root, "tickets/foo.md"))

	if !bytes.Equal(before, after) {
		test.Errorf("second reindex must be a no-op (converged):\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// runCLISplit executes the CLI and returns separate stdout and stderr buffers
// plus a boolean indicating whether the command succeeded (exit 0).
// When the command fails, the error message is written to stderr.
func runCLISplit(root string, args ...string) (stdout, stderr *bytes.Buffer, ok bool) {
	stdout = &bytes.Buffer{}
	stderr = &bytes.Buffer{}

	originalCwd, getCwdErr := os.Getwd()
	if getCwdErr == nil {
		defer func() { _ = os.Chdir(originalCwd) }()
	}

	_ = os.Chdir(root)

	rootCmd := newRootCmd()
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(stderr)
	rootCmd.SetArgs(args)

	execErr := rootCmd.Execute()

	if execErr != nil {
		fmt.Fprintln(stderr, execErr.Error())
	}

	return stdout, stderr, execErr == nil
}

func TestNodeModify_PropertyUndeclaredDriftsAndDoctorSurfaces(test *testing.T) {
	root := newWorkspaceWithNodeTypes(test)

	mustCreateNode(test, root, "tickets/foo", "ticket", map[string]string{"summary": "hi"})

	stdout, stderr, ok := runCLISplit(root, "node", "modify", "tickets/foo", "--prop", "assignee=bob")

	if !ok {
		test.Errorf("exit non-zero, want 0")
	}

	if !strings.Contains(stderr.String(), "assignee") {
		test.Errorf("stderr = %q, want drift warning", stderr.String())
	}

	if !strings.Contains(stdout.String(), "Modified tickets/foo") {
		test.Errorf("stdout = %q, want success line", stdout.String())
	}

	doctorStdout, _, doctorOk := runCLISplit(root, "doctor")

	if !doctorOk {
		test.Errorf("doctor exit non-zero, want 0")
	}

	if !strings.Contains(doctorStdout.String(), "undeclared-property") {
		test.Errorf("doctor stdout = %q", doctorStdout.String())
	}
}

func TestNodeModify_UnsetRequiredRejected(test *testing.T) {
	root := newWorkspaceWithNodeTypes(test)

	mustCreateNode(test, root, "tickets/foo", "ticket", map[string]string{"summary": "hi"})

	_, stderr, ok := runCLISplit(root, "node", "modify", "tickets/foo", "--unset", "summary")

	if ok {
		test.Errorf("exit 0, want non-zero")
	}

	if !strings.Contains(stderr.String(), "cannot unset required") {
		test.Errorf("stderr = %q, want mention of cannot-unset-required", stderr.String())
	}
}

func TestNodeModify_WorkflowLegalTransition(test *testing.T) {
	root := newWorkspaceWithWorkflow(test)

	mustCreateNode(test, root, "tickets/foo", "ticket", map[string]string{"status": "pending"})

	stdout, stderr, ok := runCLISplit(root, "node", "modify", "tickets/foo", "--prop", "status=active")

	if !ok {
		test.Errorf("exit non-zero; stderr = %s", stderr.String())
	}

	if !strings.Contains(stdout.String(), "Modified tickets/foo") {
		test.Errorf("stdout = %q, want 'Modified tickets/foo'", stdout.String())
	}
}

func TestNodeModify_WorkflowIllegalTransitionRejected(test *testing.T) {
	root := newWorkspaceWithWorkflow(test)

	mustCreateNode(test, root, "tickets/foo", "ticket", map[string]string{"status": "pending"})

	_, stderr, ok := runCLISplit(root, "node", "modify", "tickets/foo", "--prop", "status=completed")

	if ok {
		test.Errorf("exit 0, want non-zero")
	}

	if !strings.Contains(stderr.String(), "cannot transition") && !strings.Contains(stderr.String(), "illegal-transition") {
		test.Errorf("stderr = %q, want mention of illegal transition", stderr.String())
	}
}

func TestNodeModify_WorkflowRecoveryWarnsAndPersistsDrift(test *testing.T) {
	root := newWorkspaceWithWorkflow(test)

	mustCreateNode(test, root, "tickets/foo", "ticket", map[string]string{"status": "blocked"}) // off-schema

	stdout, stderr, ok := runCLISplit(root, "node", "modify", "tickets/foo", "--prop", "status=active")

	if !ok {
		test.Errorf("exit non-zero, want 0")
	}

	if !strings.Contains(stderr.String(), "recovered from unknown status") {
		test.Errorf("stderr = %q, want recovery warning", stderr.String())
	}

	if !strings.Contains(stdout.String(), "Modified tickets/foo") {
		test.Errorf("stdout = %q, want success line", stdout.String())
	}

	// Drift should now be visible to `tusk doctor`.
	doctorOut, _, doctorOk := runCLISplit(root, "doctor")

	if !doctorOk {
		test.Errorf("doctor exit non-zero, want 0")
	}

	if !strings.Contains(doctorOut.String(), "workflow-violation") {
		test.Errorf("doctor stdout = %q, want workflow-violation", doctorOut.String())
	}
}

func TestNodeModify_WorkflowUnsetRejected(test *testing.T) {
	root := newWorkspaceWithWorkflow(test)

	mustCreateNode(test, root, "tickets/foo", "ticket", map[string]string{"status": "active"})

	_, stderr, ok := runCLISplit(root, "node", "modify", "tickets/foo", "--unset", "status")

	if ok {
		test.Errorf("exit 0, want non-zero")
	}

	if !strings.Contains(stderr.String(), "cannot unset") && !strings.Contains(stderr.String(), "cannot-unset-status") {
		test.Errorf("stderr = %q, want mention of unset rejection", stderr.String())
	}
}

// newWorkspaceWithWorkflow seeds a workspace under test.TempDir() with a
// tusk.toml that activates the workflow pack on tickets. Returns the
// workspace root.
func newWorkspaceWithWorkflow(test *testing.T) string {
	test.Helper()

	root := test.TempDir()

	manifestBody := `
[workspace]
name = "test"

[behaviors.workflow.tickets]
applies-to = ["ticket"]
states = [
  { name = "pending", initial = true },
  { name = "active" },
  { name = "completed", terminal = true, done = true },
]
transitions = [
  { from = "pending", to = "active" },
  { from = "active", to = "completed" },
]
`

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(manifestBody), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	return root
}

// mustCreateNode writes a node file directly to disk under root with the
// given frontmatter properties, then runs `tusk reindex` to populate the
// index.
func mustCreateNode(test *testing.T, root, id, typ string, props map[string]string) {
	test.Helper()

	relPath := filepath.Join(root, id+".md")

	if mkErr := os.MkdirAll(filepath.Dir(relPath), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	var sb strings.Builder
	sb.WriteString("---\n")
	fmt.Fprintf(&sb, "type: %s\n", typ)

	for key, value := range props {
		fmt.Fprintf(&sb, "%s: %s\n", key, value)
	}

	sb.WriteString("---\n\nbody\n")

	if writeErr := os.WriteFile(relPath, []byte(sb.String()), 0o644); writeErr != nil {
		test.Fatalf("write node: %v", writeErr)
	}

	_, _, _ = runCLISplit(root, "reindex")
}
