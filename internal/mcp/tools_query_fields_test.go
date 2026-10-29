package mcp_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/mcp"
)

// seedCountServices upserts count service rows, every one with an owner and
// the even ones in the billing domain.
func seedCountServices(test *testing.T, rt *mcp.Runtime, count int) {
	test.Helper()

	for offset := 0; offset < count; offset++ {
		id := fmt.Sprintf("svc/s%02d", offset)
		properties := `{"owner":"me"}`

		if offset%2 == 0 {
			properties = `{"owner":"me","domain":"billing"}`
		}

		if upsertErr := rt.Nodes.Upsert(index.NodeRow{
			ID: id, Type: "service", Path: id + ".md", Title: id, PropertiesJSON: properties, LastChecksum: "x",
		}); upsertErr != nil {
			test.Fatalf("upsert %s: %v", id, upsertErr)
		}
	}
}

// TestTool_Query_FieldsProjectJSON pins finding 14 of #851 on MCP: fields
// projects tusk_query's JSON rows, and properties.<name> keeps one property.
func TestTool_Query_FieldsProjectJSON(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	seedCountServices(test, rt, 2)

	srv := mcp.NewServer(rt)
	text := callToolText(test, srv, "tusk_query", map[string]any{
		"filter": "type=service",
		"sort":   "+id",
		"fields": []any{"id", "properties.domain"},
	})

	want := `{"count":2,"results":[{"id":"svc/s00","properties":{"domain":"billing"}},{"id":"svc/s01"}]}`

	if text != want {
		test.Errorf("tusk_query fields =\n%s\nwant\n%s", text, want)
	}

	_, callErr := callTool(test, srv, "tusk_query", map[string]any{"filter": "", "fields": []any{"titel"}})

	if callErr == nil || !strings.Contains(callErr.Error(), `unknown field "titel"`) {
		test.Errorf("tusk_query fields=[titel] error = %v, want unknown field", callErr)
	}
}

// TestTool_Query_CountIgnoresDefaultTake pins finding 19 of #851 on MCP: a
// count reports every match, not the 50-row page a structural read returns.
func TestTool_Query_CountIgnoresDefaultTake(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	seedCountServices(test, rt, 60)

	srv := mcp.NewServer(rt)

	if text := callToolText(test, srv, "tusk_query", map[string]any{"filter": "type=service", "count": true}); text != `{"count":60}` {
		test.Errorf("tusk_query count = %s, want {\"count\":60}", text)
	}

	grouped := callToolText(test, srv, "tusk_query", map[string]any{"filter": "type=service", "group_by": "domain"})

	if want := `{"count":60,"group_by":"domain","groups":[{"value":"billing","count":30},{"value":null,"count":30}]}`; grouped != want {
		test.Errorf("tusk_query group_by =\n%s\nwant\n%s", grouped, want)
	}

	compact := callToolText(test, srv, "tusk_query", map[string]any{"filter": "type=service", "group_by": "domain", "format": "compact"})

	if want := "DOMAIN   COUNT\nbilling  30\n(none)   30\n"; compact != want {
		test.Errorf("tusk_query group_by compact =\n%s\nwant\n%s", compact, want)
	}

	if _, callErr := callTool(test, srv, "tusk_query", map[string]any{"filter": "", "count": "yes"}); callErr == nil {
		test.Error("tusk_query count=\"yes\" = nil error, want a type error")
	}
}

func TestTool_NodeReads_FieldsProjectJSON(test *testing.T) {
	rt := bootRuntime(test)
	defer rt.Close()

	seedCountServices(test, rt, 2)

	srv := mcp.NewServer(rt)

	if text := callToolText(test, srv, "tusk_node_list", map[string]any{"type": "service", "fields": []any{"id"}}); text != `{"count":2,"results":[{"id":"svc/s00"},{"id":"svc/s01"}]}` {
		test.Errorf("tusk_node_list fields = %s", text)
	}

	writeServiceFile(test, rt.Root, "svc/s00.md", "---\ntype: service\nowner: me\ndomain: billing\n---\n\n# s00\n")

	if text := callToolText(test, srv, "tusk_node_get", map[string]any{"id": "svc/s00", "fields": []any{"id", "properties.owner"}}); text != `{"id":"svc/s00","properties":{"owner":"me"}}` {
		test.Errorf("tusk_node_get fields = %s", text)
	}

	if _, callErr := callTool(test, srv, "tusk_node_get", map[string]any{"id": "svc/s00", "fields": []any{"titel"}}); callErr == nil {
		test.Error("tusk_node_get fields=[titel] = nil error, want unknown field")
	}
}

// TestTool_Run_CountAlias covers tusk_run: a query alias's count and
// group-by args count, in JSON and compact.
func TestTool_Run_CountAlias(test *testing.T) {
	root := test.TempDir()
	body := `[workspace]
name = "x"

[alias.by-domain]
command = "query"
args = { filter = "type=service", group-by = "domain" }

[alias.how-many]
command = "query"
args = { filter = "type=service", count = true, fields = ["id"] }
`

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	introspect := manifest.VerbIntrospector(func(string) ([]manifest.FlagSpec, bool) {
		return []manifest.FlagSpec{
			{Name: "filter", Kind: "string"},
			{Name: "fields", Kind: "stringSlice"},
			{Name: "count", Kind: "bool"},
			{Name: "group-by", Kind: "string"},
		}, true
	})

	rt, openErr := mcp.Open(root, mcp.WithAliasIntrospector(introspect))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer rt.Close()

	seedCountServices(test, rt, 3)

	srv := mcp.NewServer(rt)

	if text := callToolText(test, srv, "tusk_run", map[string]any{"alias": "how-many"}); !strings.Contains(text, `"result":{"count":3}`) {
		test.Errorf("tusk_run how-many = %s, want result {\"count\":3}", text)
	}

	if text := callToolText(test, srv, "tusk_run", map[string]any{"alias": "by-domain", "format": "compact"}); text != "DOMAIN   COUNT\nbilling  2\n(none)   1\n" {
		test.Errorf("tusk_run by-domain compact =\n%s", text)
	}
}

func writeServiceFile(test *testing.T, root, relPath, body string) {
	test.Helper()

	absPath := filepath.Join(root, relPath)

	if mkErr := os.MkdirAll(filepath.Dir(absPath), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	if writeErr := os.WriteFile(absPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write %s: %v", relPath, writeErr)
	}
}
