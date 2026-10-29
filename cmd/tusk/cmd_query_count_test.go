package main

import (
	"strings"
	"testing"
)

// countManifest declares a service type with an enum and a list property and
// an alias that counts through `tusk run`.
const countManifest = `[workspace]
name = "count"

[node-types.service]
description = "s"
properties = [
  { name = "domain", type = "enum", values = ["billing", "checkout"] },
  { name = "products", type = "list-of", item-type = "enum", values = ["a", "b"] },
]

[node-types.note]
description = "n"

[alias.services-by-domain]
command = "query"
args = { filter = "type=service", group-by = "domain" }

[alias.service-ids]
command = "query"
args = { filter = "type=service", fields = ["id"] }
`

// setupCountWorkspace writes three services (two billing, one checkout) and
// a note, and indexes them.
func setupCountWorkspace(test *testing.T) string {
	test.Helper()

	root := initWorkspaceWithManifest(test, countManifest)

	writeNodeFile(test, root, "a.md", "---\ntype: service\ndomain: billing\nproducts: [a, b]\n---\n\n# a\n")
	writeNodeFile(test, root, "b.md", "---\ntype: service\ndomain: billing\n---\n\n# b\n")
	writeNodeFile(test, root, "c.md", "---\ntype: service\ndomain: checkout\n---\n\n# c\n")
	writeNodeFile(test, root, "n.md", "---\ntype: note\n---\n\n# n\n")

	if _, stderr, ok := runCLISplit(root, "reindex"); !ok {
		test.Fatalf("reindex: %s", stderr)
	}

	return root
}

func runCountCLI(test *testing.T, root string, args ...string) string {
	test.Helper()

	stdout, stderr, ok := runCLISplit(root, args...)

	if !ok {
		test.Fatalf("%v: %s", args, stderr)
	}

	return stdout.String()
}

// TestQueryCmd_JSONHonorsFields pins finding 14 of #851: --fields projects
// JSON rows too, not only compact output.
func TestQueryCmd_JSONHonorsFields(test *testing.T) {
	root := setupCountWorkspace(test)

	out := runCountCLI(test, root, "query", "type=service", "--json", "--fields", "id", "--sort", "+id")
	want := "[\n  {\n    \"id\": \"a\"\n  },\n  {\n    \"id\": \"b\"\n  },\n  {\n    \"id\": \"c\"\n  }\n]\n"

	if out != want {
		test.Errorf("query --json --fields id =\n%s\nwant\n%s", out, want)
	}

	named := runCountCLI(test, root, "query", "id=a", "--json", "--fields", "id,properties.domain")

	if !strings.Contains(named, `"domain": "billing"`) || strings.Contains(named, "products") || strings.Contains(named, `"path"`) {
		test.Errorf("query --fields id,properties.domain =\n%s\nwant id and the domain property only", named)
	}

	if _, stderr, ok := runCLISplit(root, "query", "type=service", "--json", "--fields", "titel"); ok || !strings.Contains(stderr.String(), `unknown field "titel"`) {
		test.Errorf("query --fields titel: ok=%v stderr=%q, want an unknown-field error", ok, stderr)
	}
}

func TestNodeCmds_JSONHonorsFields(test *testing.T) {
	root := setupCountWorkspace(test)

	list := runCountCLI(test, root, "node", "list", "--json", "--fields", "id", "--sort", "+id")

	if strings.Contains(list, `"type"`) || !strings.Contains(list, `"id": "n"`) {
		test.Errorf("node list --json --fields id =\n%s\nwant id-only rows", list)
	}

	got := runCountCLI(test, root, "node", "get", "a", "--json", "--fields", "id,properties.domain")
	want := "{\n  \"id\": \"a\",\n  \"properties\": {\n    \"domain\": \"billing\"\n  }\n}\n"

	if got != want {
		test.Errorf("node get --json --fields id,properties.domain =\n%s\nwant\n%s", got, want)
	}

	if _, stderr, ok := runCLISplit(root, "node", "get", "a", "--fields", "titel"); ok || !strings.Contains(stderr.String(), `unknown field "titel"`) {
		test.Errorf("node get --fields titel: ok=%v stderr=%q, want an unknown-field error", ok, stderr)
	}
}

// TestQueryCmd_Count pins finding 19 of #851: --count prints the total of
// every match, whatever --take says, and --group-by counts per value.
func TestQueryCmd_Count(test *testing.T) {
	root := setupCountWorkspace(test)

	if out := runCountCLI(test, root, "query", "type=service", "--count", "--take", "1"); out != "3\n" {
		test.Errorf("query --count = %q, want %q", out, "3\n")
	}

	if out := runCountCLI(test, root, "query", "type=service", "--count", "--json"); out != "{\"count\":3}\n" {
		test.Errorf("query --count --json = %q", out)
	}

	grouped := runCountCLI(test, root, "query", "type=service", "--group-by", "domain")

	if want := "DOMAIN    COUNT\nbilling   2\ncheckout  1\n"; grouped != want {
		test.Errorf("query --group-by domain =\n%s\nwant\n%s", grouped, want)
	}

	// The count covers what the filter matches, so "" counts each file's
	// section sub-unit too, as `tusk query ''` lists them.
	groupedJSON := runCountCLI(test, root, "query", "", "--group-by", "type", "--json")

	if want := `{"count":8,"group_by":"type","groups":[{"value":"section","count":4},{"value":"service","count":3},{"value":"note","count":1}]}` + "\n"; groupedJSON != want {
		test.Errorf("query --group-by type --json = %s, want %s", groupedJSON, want)
	}

	if _, stderr, ok := runCLISplit(root, "query", "", "--count", "--semantic", "billing"); ok || !strings.Contains(stderr.String(), "semantic") {
		test.Errorf("query --count --semantic: ok=%v stderr=%q, want a semantic error", ok, stderr)
	}
}

// TestRunCmd_AliasCountAndFields covers the tusk run adapter: an alias's
// group-by arg counts and its fields arg projects rows, in both formats.
func TestRunCmd_AliasCountAndFields(test *testing.T) {
	root := setupCountWorkspace(test)

	if out := runCountCLI(test, root, "run", "services-by-domain", "--format", "compact"); out != "DOMAIN    COUNT\nbilling   2\ncheckout  1\n" {
		test.Errorf("run services-by-domain =\n%s", out)
	}

	counted := runCountCLI(test, root, "run", "services-by-domain", "--json")

	if !strings.Contains(counted, `"group_by": "domain"`) || !strings.Contains(counted, `"count": 3`) {
		test.Errorf("run services-by-domain --json =\n%s\nwant the grouped count", counted)
	}

	projected := runCountCLI(test, root, "run", "service-ids", "--json")

	if strings.Contains(projected, `"path"`) || !strings.Contains(projected, `"id": "a"`) {
		test.Errorf("run service-ids --json =\n%s\nwant id-only rows", projected)
	}
}
