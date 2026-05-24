package manifest_test

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
)

// writeContextManifest is a thin alias over writeAliasManifest so the context
// tests read naturally; it exists only for readability.
func writeContextManifest(test *testing.T, body string) string {
	test.Helper()

	return writeAliasManifest(test, body)
}

func TestLoad_ParsesContextReferenceForm(test *testing.T) {
	body := `[workspace]
name = "brain"

[alias.recent-activity]
command = "node list"
args.filter = "modified-since:7d type=note"

[context]
pinned  = ["docs/agent-charter", "docs/style"]
recent  = "recent-activity"
include = []
`

	manifestPath := writeContextManifest(test, body)
	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	manifest.ValidateAliases(loaded, fixtureIntrospector(defaultFixtureFlags()))
	manifest.ValidateContext(loaded, fixtureIntrospector(defaultFixtureFlags()))

	if loaded.Context == nil {
		test.Fatalf("Context is nil")
	}

	if got, want := len(loaded.Context.Pinned), 2; got != want {
		test.Errorf("Pinned len = %d, want %d", got, want)
	}

	if loaded.Context.Pinned[0] != "docs/agent-charter" {
		test.Errorf("Pinned[0] = %q", loaded.Context.Pinned[0])
	}

	if loaded.Context.Recent == nil {
		test.Fatalf("Recent is nil")
	}

	if loaded.Context.Recent.Name != "recent-activity" {
		test.Errorf("Recent.Name = %q, want recent-activity", loaded.Context.Recent.Name)
	}

	if loaded.Context.Recent.Command != "node list" {
		test.Errorf("Recent.Command = %q, want node list", loaded.Context.Recent.Command)
	}

	if len(loaded.ContextErrors) != 0 {
		test.Errorf("ContextErrors = %v, want empty", loaded.ContextErrors)
	}
}

func TestLoad_ParsesContextInlineForm(test *testing.T) {
	body := `[workspace]
name = "brain"

[context]
pinned  = ["docs/agent-charter"]

[context.recent]
command     = "node list"
args.filter = "modified-since:7d type=note"
args.sort   = "modified-desc"
args.take   = 20
`

	manifestPath := writeContextManifest(test, body)
	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	manifest.ValidateAliases(loaded, fixtureIntrospector(defaultFixtureFlags()))
	manifest.ValidateContext(loaded, fixtureIntrospector(defaultFixtureFlags()))

	if loaded.Context == nil {
		test.Fatalf("Context is nil")
	}

	if loaded.Context.Recent == nil {
		test.Fatalf("Recent is nil; ContextErrors = %v", loaded.ContextErrors)
	}

	if loaded.Context.Recent.Command != "node list" {
		test.Errorf("Recent.Command = %q, want node list", loaded.Context.Recent.Command)
	}

	if loaded.Context.Recent.Name == "" {
		test.Errorf("Recent.Name is empty; want synthetic name")
	}

	if loaded.Context.Recent.Args["filter"] != "modified-since:7d type=note" {
		test.Errorf("Recent.Args[filter] = %v", loaded.Context.Recent.Args["filter"])
	}
}

func TestLoad_RejectsBothRecentFormsSet(test *testing.T) {
	body := `[workspace]
name = "brain"

[alias.recent-activity]
command = "node list"

[context]
pinned = ["docs/style"]
recent = "recent-activity"

[context.recent]
command = "node list"
`

	manifestPath := writeContextManifest(test, body)
	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v (expected success; collision should be a ContextError)", loadErr)
	}

	manifest.ValidateAliases(loaded, fixtureIntrospector(defaultFixtureFlags()))
	manifest.ValidateContext(loaded, fixtureIntrospector(defaultFixtureFlags()))

	if loaded.Context == nil {
		test.Fatalf("Context is nil; want declared with Recent unset")
	}

	if loaded.Context.Recent != nil {
		test.Errorf("Recent = %v, want nil (both forms collide)", loaded.Context.Recent)
	}

	if len(loaded.ContextErrors) == 0 {
		test.Fatalf("ContextErrors empty; want one mentioning both forms")
	}

	if !contextErrorsContain(loaded.ContextErrors, "both") {
		test.Errorf("ContextErrors = %v, want one mentioning 'both'", loaded.ContextErrors)
	}
}

func TestValidateContext_RejectsUnknownReferenceAlias(test *testing.T) {
	body := `[workspace]
name = "brain"

[context]
recent = "does-not-exist"
`

	manifestPath := writeContextManifest(test, body)
	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	manifest.ValidateAliases(loaded, fixtureIntrospector(defaultFixtureFlags()))
	manifest.ValidateContext(loaded, fixtureIntrospector(defaultFixtureFlags()))

	if loaded.Context == nil {
		test.Fatalf("Context is nil")
	}

	if loaded.Context.Recent != nil {
		test.Errorf("Recent = %v, want nil (alias missing)", loaded.Context.Recent)
	}

	if !contextErrorsContain(loaded.ContextErrors, "does-not-exist") {
		test.Errorf("ContextErrors = %v, want one mentioning 'does-not-exist'", loaded.ContextErrors)
	}
}

func TestValidateContext_RejectsWriteVerbInline(test *testing.T) {
	body := `[workspace]
name = "brain"

[context]

[context.recent]
command = "node create"
`

	manifestPath := writeContextManifest(test, body)
	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	manifest.ValidateAliases(loaded, fixtureIntrospector(defaultFixtureFlags()))
	manifest.ValidateContext(loaded, fixtureIntrospector(defaultFixtureFlags()))

	if loaded.Context.Recent != nil {
		test.Errorf("Recent should be nil for write verb; got %v", loaded.Context.Recent)
	}

	if !contextErrorsContain(loaded.ContextErrors, "write verb") {
		test.Errorf("ContextErrors = %v, want one mentioning 'write verb'", loaded.ContextErrors)
	}
}

func TestValidateContext_DropsUnknownIncludeAlias(test *testing.T) {
	body := `[workspace]
name = "brain"

[alias.snap]
command = "status"

[context]
include = ["snap", "bogus"]
`

	manifestPath := writeContextManifest(test, body)
	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	manifest.ValidateAliases(loaded, fixtureIntrospector(defaultFixtureFlags()))
	manifest.ValidateContext(loaded, fixtureIntrospector(defaultFixtureFlags()))

	if loaded.Context == nil {
		test.Fatalf("Context is nil")
	}

	if got, want := len(loaded.Context.Include), 1; got != want {
		test.Fatalf("Include len = %d, want %d (%v)", got, want, loaded.Context.Include)
	}

	if loaded.Context.Include[0] != "snap" {
		test.Errorf("Include[0] = %q, want snap", loaded.Context.Include[0])
	}

	if !contextErrorsContain(loaded.ContextErrors, "bogus") {
		test.Errorf("ContextErrors = %v, want one mentioning 'bogus'", loaded.ContextErrors)
	}
}

func TestValidateContext_NoContextBlockIsNoOp(test *testing.T) {
	body := `[workspace]
name = "brain"
`

	manifestPath := writeContextManifest(test, body)
	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	manifest.ValidateAliases(loaded, fixtureIntrospector(defaultFixtureFlags()))
	manifest.ValidateContext(loaded, fixtureIntrospector(defaultFixtureFlags()))

	if loaded.Context != nil {
		test.Errorf("Context = %v, want nil when [context] not declared", loaded.Context)
	}

	if len(loaded.ContextErrors) != 0 {
		test.Errorf("ContextErrors = %v, want empty", loaded.ContextErrors)
	}
}

func TestLoad_DoesNotFailOnInvalidContext(test *testing.T) {
	body := `[workspace]
name = "brain"

[context]
recent  = "no-such-alias"
include = ["also-bogus"]
`

	manifestPath := writeContextManifest(test, body)
	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v (must not fail on bad context)", loadErr)
	}

	manifest.ValidateAliases(loaded, fixtureIntrospector(defaultFixtureFlags()))
	manifest.ValidateContext(loaded, fixtureIntrospector(defaultFixtureFlags()))

	if loaded.Context == nil {
		test.Fatalf("Context nil")
	}

	if loaded.Context.Recent != nil {
		test.Errorf("Recent = %v, want nil", loaded.Context.Recent)
	}

	if len(loaded.Context.Include) != 0 {
		test.Errorf("Include = %v, want empty after pruning", loaded.Context.Include)
	}

	if len(loaded.ContextErrors) < 2 {
		test.Errorf("ContextErrors = %v, want at least two", loaded.ContextErrors)
	}
}

func contextErrorsContain(errs []manifest.ContextError, needle string) bool {
	for _, item := range errs {
		if strings.Contains(item.Message, needle) {
			return true
		}
	}

	return false
}
