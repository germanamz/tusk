package manifest_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
)

// fixtureIntrospector returns a closure over the given flag map.
func fixtureIntrospector(flags map[string][]manifest.FlagSpec) manifest.VerbIntrospector {
	return func(verb string) ([]manifest.FlagSpec, bool) {
		spec, ok := flags[verb]

		return spec, ok
	}
}

func defaultFixtureFlags() map[string][]manifest.FlagSpec {
	return map[string][]manifest.FlagSpec{
		"node list": {
			{Name: "sort", Kind: "string"},
			{Name: "take", Kind: "int"},
			{Name: "skip", Kind: "int"},
			{Name: "include", Kind: "stringSlice"},
			{Name: "fields", Kind: "stringSlice"},
			{Name: "format", Kind: "string"},
			{Name: "json", Kind: "bool"},
		},
		"node get": {
			{Name: "include", Kind: "stringSlice"},
			{Name: "fields", Kind: "stringSlice"},
			{Name: "format", Kind: "string"},
			{Name: "json", Kind: "bool"},
		},
		"query": {
			{Name: "sort", Kind: "string"},
			{Name: "take", Kind: "int"},
			{Name: "skip", Kind: "int"},
			{Name: "semantic", Kind: "string"},
			{Name: "include", Kind: "stringSlice"},
			{Name: "fields", Kind: "stringSlice"},
			{Name: "format", Kind: "string"},
			{Name: "json", Kind: "bool"},
		},
		"edge list": {
			{Name: "from", Kind: "string"},
			{Name: "to", Kind: "string"},
			{Name: "type", Kind: "string"},
			{Name: "format", Kind: "string"},
			{Name: "json", Kind: "bool"},
		},
		"doctor": {
			{Name: "no-migrate", Kind: "bool"},
		},
		"status": {},
	}
}

func writeAliasManifest(test *testing.T, body string) string {
	test.Helper()

	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	return manifestPath
}

func TestLoad_ParsesAliases(test *testing.T) {
	body := `[workspace]
name = "brain"

[alias.open-tickets]
command = "node list"
description = "Active tickets, by priority"
args.filter = "type=ticket status=active"
args.sort   = "priority-desc"
args.take   = 10
`

	manifestPath := writeAliasManifest(test, body)
	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	alias, ok := loaded.Aliases["open-tickets"]

	if !ok {
		test.Fatalf("Aliases[open-tickets] missing; got %v", loaded.Aliases)
	}

	if alias.Command != "node list" {
		test.Errorf("Command = %q, want %q", alias.Command, "node list")
	}

	if alias.Description != "Active tickets, by priority" {
		test.Errorf("Description = %q", alias.Description)
	}

	if alias.Args["filter"] != "type=ticket status=active" {
		test.Errorf("Args[filter] = %v", alias.Args["filter"])
	}

	takeVal, ok := alias.Args["take"]

	if !ok {
		test.Fatalf("Args[take] missing")
	}

	// BurntSushi/toml decodes integers as int64 when target is map[string]any.
	switch typed := takeVal.(type) {
	case int64:
		if typed != 10 {
			test.Errorf("Args[take] = %d, want 10", typed)
		}

	default:
		test.Errorf("Args[take] type = %T, want int64", takeVal)
	}
}

func TestValidateAliases_AcceptsValidAlias(test *testing.T) {
	body := `[workspace]
name = "brain"

[alias.open-tickets]
command = "node list"
args.filter = "type=ticket status=active"
args.sort   = "priority-desc"
args.take   = 10
`

	manifestPath := writeAliasManifest(test, body)
	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	manifest.ValidateAliases(loaded, fixtureIntrospector(defaultFixtureFlags()))

	if len(loaded.AliasErrors) > 0 {
		test.Fatalf("AliasErrors = %v, want empty", loaded.AliasErrors)
	}

	alias, ok := loaded.Aliases["open-tickets"]

	if !ok {
		test.Fatalf("Aliases[open-tickets] missing after validate")
	}

	if alias.Verb.Verb != "node list" {
		test.Errorf("Verb.Verb = %q, want %q", alias.Verb.Verb, "node list")
	}

	if !alias.Verb.ReadOnly {
		test.Errorf("Verb.ReadOnly = false, want true")
	}
}

func TestValidateAliases_RejectsUnknownCommand(test *testing.T) {
	body := `[workspace]
name = "brain"

[alias.bogus]
command = "foo bar"
`

	manifestPath := writeAliasManifest(test, body)
	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	manifest.ValidateAliases(loaded, fixtureIntrospector(defaultFixtureFlags()))

	if _, ok := loaded.Aliases["bogus"]; ok {
		test.Errorf("Aliases[bogus] kept after validate; want removed")
	}

	if len(loaded.AliasErrors) != 1 {
		test.Fatalf("AliasErrors len = %d, want 1: %v", len(loaded.AliasErrors), loaded.AliasErrors)
	}

	got := loaded.AliasErrors[0]

	if got.Name != "bogus" {
		test.Errorf("AliasErrors[0].Name = %q, want bogus", got.Name)
	}

	if got.Message == "" || got.Message[:7] != "unknown" {
		test.Errorf("AliasErrors[0].Message = %q, want 'unknown verb …'", got.Message)
	}
}

func TestValidateAliases_RejectsWriteVerb(test *testing.T) {
	body := `[workspace]
name = "brain"

[alias.mut]
command = "node create"
`

	manifestPath := writeAliasManifest(test, body)
	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	manifest.ValidateAliases(loaded, fixtureIntrospector(defaultFixtureFlags()))

	if _, ok := loaded.Aliases["mut"]; ok {
		test.Errorf("Aliases[mut] kept after validate; want removed")
	}

	if len(loaded.AliasErrors) != 1 {
		test.Fatalf("AliasErrors len = %d, want 1: %v", len(loaded.AliasErrors), loaded.AliasErrors)
	}

	got := loaded.AliasErrors[0]

	if got.Name != "mut" {
		test.Errorf("Name = %q, want mut", got.Name)
	}

	wantSubstring := "write verb"

	if !contains(got.Message, wantSubstring) {
		test.Errorf("Message = %q, want to contain %q", got.Message, wantSubstring)
	}
}

func TestValidateAliases_RejectsUnknownArgKey(test *testing.T) {
	body := `[workspace]
name = "brain"

[alias.bad-arg]
command = "node list"
args.filter = "type=ticket"
args.bogus  = "value"
`

	manifestPath := writeAliasManifest(test, body)
	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	manifest.ValidateAliases(loaded, fixtureIntrospector(defaultFixtureFlags()))

	if _, ok := loaded.Aliases["bad-arg"]; ok {
		test.Errorf("Aliases[bad-arg] kept after validate; want removed")
	}

	if len(loaded.AliasErrors) == 0 {
		test.Fatalf("AliasErrors empty; want at least one")
	}

	if !containsAny(loaded.AliasErrors, "bogus") {
		test.Errorf("AliasErrors %v: want one to mention 'bogus'", loaded.AliasErrors)
	}
}

func TestValidateAliases_RejectsWrongTypedArg(test *testing.T) {
	body := `[workspace]
name = "brain"

[alias.bad-type]
command = "node list"
args.take = "not-an-int"
`

	manifestPath := writeAliasManifest(test, body)
	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	manifest.ValidateAliases(loaded, fixtureIntrospector(defaultFixtureFlags()))

	if _, ok := loaded.Aliases["bad-type"]; ok {
		test.Errorf("Aliases[bad-type] kept after validate; want removed")
	}

	if len(loaded.AliasErrors) == 0 {
		test.Fatalf("AliasErrors empty; want one")
	}

	if !containsAny(loaded.AliasErrors, "take") {
		test.Errorf("AliasErrors %v: want one to mention 'take'", loaded.AliasErrors)
	}
}

func TestValidateAliases_LoadDoesNotFailOnBadAliases(test *testing.T) {
	body := `[workspace]
name = "brain"

[alias.good]
command = "status"

[alias.bad]
command = "no-such-verb"
`

	manifestPath := writeAliasManifest(test, body)
	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load failed on workspace with invalid alias: %v", loadErr)
	}

	manifest.ValidateAliases(loaded, fixtureIntrospector(defaultFixtureFlags()))

	if _, ok := loaded.Aliases["good"]; !ok {
		test.Errorf("Aliases[good] missing; valid alias should survive validation")
	}

	if _, ok := loaded.Aliases["bad"]; ok {
		test.Errorf("Aliases[bad] kept; invalid alias should be removed")
	}

	if len(loaded.AliasErrors) != 1 {
		test.Fatalf("AliasErrors len = %d, want 1: %v", len(loaded.AliasErrors), loaded.AliasErrors)
	}
}

func contains(haystack, needle string) bool {
	if needle == "" {
		return true
	}

	for index := 0; index+len(needle) <= len(haystack); index++ {
		if haystack[index:index+len(needle)] == needle {
			return true
		}
	}

	return false
}

func containsAny(errs []manifest.AliasError, needle string) bool {
	for _, item := range errs {
		if contains(item.Message, needle) {
			return true
		}
	}

	return false
}
