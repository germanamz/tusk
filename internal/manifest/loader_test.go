package manifest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/germanamz/tusk/internal/manifest"
)

func TestLoad_ParsesMinimalManifest(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "my-brain"
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	if loaded.Workspace.Name != "my-brain" {
		test.Errorf("Name = %q, want %q", loaded.Workspace.Name, "my-brain")
	}
}

func TestLoad_ParsesIgnorePatterns(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "my-brain"
ignore = ["build/", "*.tmp"]
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	if len(loaded.Workspace.Ignore) != 2 {
		test.Fatalf("Ignore len = %d, want 2", len(loaded.Workspace.Ignore))
	}

	if loaded.Workspace.Ignore[0] != "build/" {
		test.Errorf("Ignore[0] = %q", loaded.Workspace.Ignore[0])
	}
}

func TestLoad_ReturnsErrorOnMalformedTOML(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	if writeErr := os.WriteFile(manifestPath, []byte("not = valid = toml"), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	_, loadErr := manifest.Load(manifestPath)

	if loadErr == nil {
		test.Fatalf("expected error, got nil")
	}
}

func TestLoad_ParsesEdgeTypes(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "my-brain"

[edge-types.parent]
description = "Hierarchical parent"
from = ["ticket", "project"]
to = ["ticket", "project"]
cardinality = "many-to-one"
ordered = false

[edge-types.blocks]
description = "Blocks another"
from = ["ticket"]
to = ["ticket"]
cardinality = "many-to-many"
acyclic = true

[edge-types.references]
description = "Implicit references"
from = ["*"]
to = ["*"]
cardinality = "many-to-many"
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	if len(loaded.EdgeTypes) != 3 {
		test.Fatalf("EdgeTypes len = %d, want 3", len(loaded.EdgeTypes))
	}

	parentType, hasParent := loaded.EdgeTypes["parent"]

	if !hasParent {
		test.Fatalf("parent edge type missing")
	}

	if parentType.Cardinality != manifest.CardinalityManyToOne {
		test.Errorf("parent cardinality = %q", parentType.Cardinality)
	}

	if !parentType.AllowsSource("ticket") {
		test.Errorf("parent should allow ticket source")
	}

	blocksType := loaded.EdgeTypes["blocks"]

	if !blocksType.Acyclic {
		test.Errorf("blocks should be acyclic")
	}

	referencesType := loaded.EdgeTypes["references"]

	if !referencesType.AllowsSource("anything") {
		test.Errorf("references should allow any source via wildcard")
	}
}

func TestLoad_RejectsInvalidCardinality(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "x"

[edge-types.bad]
from = ["ticket"]
to = ["ticket"]
cardinality = "bogus"
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	_, loadErr := manifest.Load(manifestPath)

	if loadErr == nil {
		test.Fatalf("expected error for invalid cardinality")
	}
}

func TestLoad_RejectsEmptyFromOrTo(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "x"

[edge-types.bad]
from = []
to = ["ticket"]
cardinality = "many-to-many"
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	_, loadErr := manifest.Load(manifestPath)

	if loadErr == nil {
		test.Fatalf("expected error for empty from list")
	}
}

func TestLoad_ParsesEmbeddings(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "my-brain"

[embeddings]
provider = "ollama"
model = "nomic-embed-text"
endpoint = "http://localhost:11434"
dim = 768
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	if loaded.Embeddings.Provider != "ollama" {
		test.Errorf("Provider = %q", loaded.Embeddings.Provider)
	}

	if loaded.Embeddings.Dim != 768 {
		test.Errorf("Dim = %d", loaded.Embeddings.Dim)
	}
}

func TestLoad_ParsesEmbeddingsWorkers(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "x"

[embeddings]
provider = "ollama"
model    = "nomic-embed-text"
endpoint = "http://localhost:11434"
dim      = 768
workers  = 6
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	if loaded.Embeddings.Workers != 6 {
		test.Errorf("Workers = %d, want 6", loaded.Embeddings.Workers)
	}
}

func TestLoad_ParsesEmbeddingsTimeoutSeconds(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "x"

[embeddings]
provider        = "ollama"
model           = "nomic-embed-text"
endpoint        = "http://localhost:11434"
dim             = 768
timeout-seconds = 240
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	if loaded.Embeddings.TimeoutSeconds != 240 {
		test.Errorf("TimeoutSeconds = %d, want 240", loaded.Embeddings.TimeoutSeconds)
	}
}

func TestLoad_RejectsNegativeWorkers(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	// Use -1 (not 0) because go-toml decodes both absent and explicit-zero
	// as int(0); the validator can't distinguish those, so "absent" wins.
	body := `[workspace]
name = "x"

[embeddings]
provider = "ollama"
model    = "nomic-embed-text"
endpoint = "http://localhost:11434"
dim      = 768
workers  = -1
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	_, loadErr := manifest.Load(manifestPath)

	if loadErr == nil {
		test.Fatalf("Load: expected error for workers=-1")
	}

	if !strings.Contains(loadErr.Error(), "workers") {
		test.Errorf("Load error = %q, want it to mention 'workers'", loadErr.Error())
	}
}

func TestLoad_RejectsNegativeTimeoutSeconds(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "x"

[embeddings]
provider        = "ollama"
model           = "nomic-embed-text"
endpoint        = "http://localhost:11434"
dim             = 768
timeout-seconds = -5
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	_, loadErr := manifest.Load(manifestPath)

	if loadErr == nil {
		test.Fatalf("Load: expected error for timeout-seconds=-5")
	}

	if !strings.Contains(loadErr.Error(), "timeout-seconds") {
		test.Errorf("Load error = %q, want it to mention 'timeout-seconds'", loadErr.Error())
	}
}

func TestLoad_RejectsUnknownEmbeddingsProvider(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "x"

[embeddings]
provider = "bogus"
model = "x"
dim = 768
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	_, loadErr := manifest.Load(manifestPath)

	if loadErr == nil {
		test.Fatalf("expected error for unknown provider")
	}
}

func TestLoad_RejectsZeroDim(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "x"

[embeddings]
provider = "ollama"
model = "x"
dim = 0
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	_, loadErr := manifest.Load(manifestPath)

	if loadErr == nil {
		test.Fatalf("expected error for dim = 0")
	}
}

func TestLoad_AcceptsAbsentEmbeddings(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "x"
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	if loaded.Embeddings.Provider != "" {
		test.Errorf("Provider should be empty when [embeddings] absent: %q", loaded.Embeddings.Provider)
	}
}

func TestLoad_CapturesBehaviorsTomlMeta(test *testing.T) {
	dir := test.TempDir()
	manifestPath := filepath.Join(dir, "tusk.toml")

	content := `
[workspace]
name = "test"

[behaviors.workflow.tickets]
applies-to = ["ticket"]
states = [
  { name = "open", initial = true },
]
`
	if writeErr := os.WriteFile(manifestPath, []byte(content), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	if loaded.Meta == nil {
		test.Errorf("Meta: nil; expected captured TOML meta data")
	}

	subtable, found := loaded.Behaviors["workflow"]["tickets"]

	if !found {
		test.Fatalf("Behaviors[workflow][tickets]: missing")
	}

	// Sanity: PrimitiveDecode the subtable into a partial struct.
	var partial struct {
		AppliesTo []string `toml:"applies-to"`
	}

	if decodeErr := loaded.Meta.PrimitiveDecode(subtable, &partial); decodeErr != nil {
		test.Fatalf("PrimitiveDecode: %v", decodeErr)
	}

	if len(partial.AppliesTo) != 1 || partial.AppliesTo[0] != "ticket" {
		test.Errorf("AppliesTo = %v, want [ticket]", partial.AppliesTo)
	}
}

func TestLoad_RejectsBehaviorsWithEmptyInstanceName(test *testing.T) {
	dir := test.TempDir()
	manifestPath := filepath.Join(dir, "tusk.toml")

	// Empty TOML key isn't expressible directly; we test the validation by
	// constructing a Manifest in-memory through the validate function.
	loaded := &manifest.Manifest{
		Behaviors: map[string]map[string]toml.Primitive{
			"workflow": {"": toml.Primitive{}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr == nil {
		test.Errorf("Validate: expected empty-instance-name rejection")
	}

	_ = manifestPath
}

func TestLoad_RejectsBehaviorsWithEmptyKindName(test *testing.T) {
	loaded := &manifest.Manifest{
		Behaviors: map[string]map[string]toml.Primitive{
			"": {"any": toml.Primitive{}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr == nil {
		test.Errorf("Validate: expected empty-kind-name rejection")
	}
}

func TestLoad_DecodesNodeTypesSection(test *testing.T) {
	dir := test.TempDir()
	manifestPath := filepath.Join(dir, "tusk.toml")

	content := `
[workspace]
name = "test"

[node-types.ticket]
description = "A unit of trackable work"
properties = [
    { name = "summary",  type = "string", required = true },
    { name = "priority", type = "int" },
    { name = "due",      type = "date" },
    { name = "labels",   type = "list-of", item-type = "string" },
    { name = "stage",    type = "enum", values = ["pending", "active", "completed"] },
]

[node-types.note]
description = "Free-form note"
properties = [
    { name = "summary", type = "string", required = true },
]
`
	if writeErr := os.WriteFile(manifestPath, []byte(content), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	if len(loaded.NodeTypes) != 2 {
		test.Fatalf("NodeTypes count = %d, want 2", len(loaded.NodeTypes))
	}

	ticket, ok := loaded.NodeTypes["ticket"]

	if !ok {
		test.Fatalf("ticket type not decoded")
	}

	if ticket.Description != "A unit of trackable work" {
		test.Errorf("ticket.Description = %q", ticket.Description)
	}

	if len(ticket.Properties) != 5 {
		test.Fatalf("ticket.Properties count = %d, want 5", len(ticket.Properties))
	}

	summaryProp := ticket.Properties[0]

	if summaryProp.Name != "summary" || summaryProp.Type != "string" || !summaryProp.Required {
		test.Errorf("ticket.Properties[0] = %+v", summaryProp)
	}

	labelsProp := ticket.Properties[3]

	if labelsProp.Name != "labels" || labelsProp.Type != "list-of" || labelsProp.ItemType != "string" {
		test.Errorf("ticket.Properties[3] = %+v", labelsProp)
	}

	stageProp := ticket.Properties[4]

	if stageProp.Type != "enum" || len(stageProp.Values) != 3 {
		test.Errorf("ticket.Properties[4] = %+v", stageProp)
	}
}

func TestValidate_RejectsNodeTypeWithEmptyName(test *testing.T) {
	loaded := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{
			"": {Properties: []manifest.PropertyDecl{{Name: "title", Type: "string"}}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr == nil || !strings.Contains(validateErr.Error(), "empty type name") {
		test.Errorf("Validate: expected empty-type-name error, got %v", validateErr)
	}
}

func TestValidate_RejectsPropertyWithEmptyName(test *testing.T) {
	loaded := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{
			"ticket": {Properties: []manifest.PropertyDecl{{Name: "", Type: "string"}}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr == nil || !strings.Contains(validateErr.Error(), "empty property name") {
		test.Errorf("Validate: expected empty-property-name error, got %v", validateErr)
	}
}

func TestValidate_RejectsReservedPropertyName(test *testing.T) {
	cases := []string{"type", "title"}

	for _, reserved := range cases {
		loaded := &manifest.Manifest{
			NodeTypes: map[string]manifest.NodeType{
				"ticket": {Properties: []manifest.PropertyDecl{{Name: reserved, Type: "string"}}},
			},
		}

		validateErr := manifest.Validate(loaded)

		if validateErr == nil || !strings.Contains(validateErr.Error(), reserved) {
			test.Errorf("Validate: reserved %q expected error, got %v", reserved, validateErr)
		}
	}
}

func TestValidate_RejectsDuplicatePropertyName(test *testing.T) {
	loaded := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{
			"ticket": {Properties: []manifest.PropertyDecl{
				{Name: "x", Type: "string"},
				{Name: "x", Type: "int"},
			}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr == nil || !strings.Contains(validateErr.Error(), "duplicate") {
		test.Errorf("Validate: expected duplicate-property-name error, got %v", validateErr)
	}
}

func TestValidate_RejectsUnknownPropertyType(test *testing.T) {
	loaded := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{
			"ticket": {Properties: []manifest.PropertyDecl{{Name: "x", Type: "blob"}}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr == nil || !strings.Contains(validateErr.Error(), "blob") {
		test.Errorf("Validate: expected unknown-type error, got %v", validateErr)
	}
}

func TestValidate_AcceptsHappyPath(test *testing.T) {
	loaded := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{
			"ticket": {Properties: []manifest.PropertyDecl{
				{Name: "summary", Type: "string", Required: true},
				{Name: "n", Type: "int"},
				{Name: "f", Type: "float"},
				{Name: "b", Type: "bool"},
				{Name: "d", Type: "date"},
				{Name: "dt", Type: "datetime"},
				{Name: "md", Type: "markdown"},
			}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr != nil {
		test.Errorf("Validate: %v", validateErr)
	}
}

func TestValidate_RejectsEnumWithoutValues(test *testing.T) {
	loaded := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{
			"ticket": {Properties: []manifest.PropertyDecl{{Name: "stage", Type: "enum"}}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr == nil || !strings.Contains(validateErr.Error(), "values") {
		test.Errorf("Validate: expected enum-without-values error, got %v", validateErr)
	}
}

func TestValidate_RejectsEnumWithEmptyValueElement(test *testing.T) {
	loaded := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{
			"ticket": {Properties: []manifest.PropertyDecl{{Name: "stage", Type: "enum", Values: []string{"a", ""}}}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr == nil || !strings.Contains(validateErr.Error(), "empty") {
		test.Errorf("Validate: expected empty-enum-value error, got %v", validateErr)
	}
}

func TestValidate_RejectsEnumWithDuplicateValues(test *testing.T) {
	loaded := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{
			"ticket": {Properties: []manifest.PropertyDecl{{Name: "stage", Type: "enum", Values: []string{"a", "a"}}}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr == nil || !strings.Contains(validateErr.Error(), "duplicate") {
		test.Errorf("Validate: expected duplicate-enum-value error, got %v", validateErr)
	}
}

func TestValidate_RejectsListOfWithoutItemType(test *testing.T) {
	loaded := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{
			"ticket": {Properties: []manifest.PropertyDecl{{Name: "labels", Type: "list-of"}}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr == nil || !strings.Contains(validateErr.Error(), "item-type") {
		test.Errorf("Validate: expected list-of-without-item-type error, got %v", validateErr)
	}
}

func TestValidate_RejectsListOfWithNestedListItemType(test *testing.T) {
	loaded := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{
			"ticket": {Properties: []manifest.PropertyDecl{{Name: "labels", Type: "list-of", ItemType: "list-of"}}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr == nil || !strings.Contains(validateErr.Error(), "nest") {
		test.Errorf("Validate: expected nesting error, got %v", validateErr)
	}
}

func TestValidate_RejectsListOfEnumWithoutValues(test *testing.T) {
	loaded := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{
			"ticket": {Properties: []manifest.PropertyDecl{{Name: "stages", Type: "list-of", ItemType: "enum"}}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr == nil || !strings.Contains(validateErr.Error(), "values") {
		test.Errorf("Validate: expected list-of-enum-without-values error, got %v", validateErr)
	}
}

func TestValidate_RejectsValuesOnNonEnumScalar(test *testing.T) {
	loaded := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{
			"ticket": {Properties: []manifest.PropertyDecl{{Name: "x", Type: "string", Values: []string{"a"}}}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr == nil || !strings.Contains(validateErr.Error(), "values") {
		test.Errorf("Validate: expected misplaced-values error, got %v", validateErr)
	}
}

func TestValidate_RejectsItemTypeOnNonListScalar(test *testing.T) {
	loaded := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{
			"ticket": {Properties: []manifest.PropertyDecl{{Name: "x", Type: "string", ItemType: "int"}}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr == nil || !strings.Contains(validateErr.Error(), "item-type") {
		test.Errorf("Validate: expected misplaced-item-type error, got %v", validateErr)
	}
}

func TestValidate_AcceptsListOfEnum(test *testing.T) {
	loaded := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{
			"ticket": {Properties: []manifest.PropertyDecl{{
				Name:     "stages",
				Type:     "list-of",
				ItemType: "enum",
				Values:   []string{"draft", "review"},
			}}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr != nil {
		test.Errorf("Validate: %v", validateErr)
	}
}

func TestLoad_DecodesRefPropertyFields(test *testing.T) {
	dir := test.TempDir()
	manifestPath := filepath.Join(dir, "tusk.toml")

	content := `
[workspace]
name = "test"

[node-types.person]
properties = [
    { name = "name", type = "string", required = true },
]

[node-types.ticket]
properties = [
    { name = "assignee", type = "ref", to = "person" },
    { name = "watchers", type = "list-of", item-type = "ref", to = "person", inverse = "watching" },
    { name = "parent",   type = "ref", to = "ticket", acyclic = true },
]
`
	if writeErr := os.WriteFile(manifestPath, []byte(content), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	ticket := loaded.NodeTypes["ticket"]

	if len(ticket.Properties) != 3 {
		test.Fatalf("ticket.Properties count = %d, want 3", len(ticket.Properties))
	}

	assignee := ticket.Properties[0]

	if assignee.Type != "ref" || assignee.To != "person" {
		test.Errorf("assignee = %+v", assignee)
	}

	watchers := ticket.Properties[1]

	if watchers.Type != "list-of" || watchers.ItemType != "ref" || watchers.To != "person" || watchers.Inverse != "watching" {
		test.Errorf("watchers = %+v", watchers)
	}

	parent := ticket.Properties[2]

	if parent.Type != "ref" || parent.To != "ticket" || !parent.Acyclic {
		test.Errorf("parent = %+v", parent)
	}
}

func TestValidate_RejectsRefWithoutTo(test *testing.T) {
	loaded := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{
			"ticket": {Properties: []manifest.PropertyDecl{{Name: "assignee", Type: "ref"}}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr == nil || !strings.Contains(validateErr.Error(), "ref property requires `to`") {
		test.Errorf("Validate: expected ref-without-to error, got %v", validateErr)
	}
}

func TestValidate_RejectsListOfRefWithoutTo(test *testing.T) {
	loaded := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{
			"ticket": {Properties: []manifest.PropertyDecl{
				{Name: "watchers", Type: "list-of", ItemType: "ref"},
			}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr == nil || !strings.Contains(validateErr.Error(), "ref property requires `to`") {
		test.Errorf("Validate: expected list-of(ref)-without-to error, got %v", validateErr)
	}
}

func TestValidate_AcceptsRefWithToWildcard(test *testing.T) {
	loaded := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{
			"ticket": {Properties: []manifest.PropertyDecl{{Name: "linked", Type: "ref", To: "*"}}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr != nil {
		test.Errorf("Validate: ref with to=* unexpectedly rejected: %v", validateErr)
	}
}

func TestValidate_RejectsRefToUndeclaredType(test *testing.T) {
	loaded := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{
			"ticket": {Properties: []manifest.PropertyDecl{{Name: "assignee", Type: "ref", To: "person"}}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr == nil || !strings.Contains(validateErr.Error(), "person") {
		test.Errorf("Validate: expected ref-to-undeclared error, got %v", validateErr)
	}
}

func TestValidate_AcceptsRefToDeclaredType(test *testing.T) {
	loaded := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{
			"person": {},
			"ticket": {Properties: []manifest.PropertyDecl{{Name: "assignee", Type: "ref", To: "person"}}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr != nil {
		test.Errorf("Validate: ref to declared type unexpectedly rejected: %v", validateErr)
	}
}

func TestValidate_RejectsRefWithValues(test *testing.T) {
	loaded := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{
			"person": {},
			"ticket": {Properties: []manifest.PropertyDecl{
				{Name: "assignee", Type: "ref", To: "person", Values: []string{"a", "b"}},
			}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr == nil || !strings.Contains(validateErr.Error(), "values") {
		test.Errorf("Validate: expected ref-with-values error, got %v", validateErr)
	}
}

func TestValidate_RejectsRefWithItemType(test *testing.T) {
	loaded := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{
			"person": {},
			"ticket": {Properties: []manifest.PropertyDecl{
				{Name: "assignee", Type: "ref", To: "person", ItemType: "string"},
			}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr == nil || !strings.Contains(validateErr.Error(), "item-type") {
		test.Errorf("Validate: expected ref-with-item-type error, got %v", validateErr)
	}
}

func TestSynthesize_PlainRefProducesManyToOneEdge(test *testing.T) {
	loaded := &manifest.Manifest{
		EdgeTypes: manifest.EdgeTypes{},
		NodeTypes: map[string]manifest.NodeType{
			"person": {},
			"ticket": {Properties: []manifest.PropertyDecl{
				{Name: "assignee", Type: "ref", To: "person"},
			}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr != nil {
		test.Fatalf("Validate: %v", validateErr)
	}

	edge, ok := loaded.EdgeTypes["assignee"]

	if !ok {
		test.Fatalf("expected synthesized edge-type assignee")
	}

	if len(edge.From) != 1 || edge.From[0] != "ticket" {
		test.Errorf("From = %v, want [ticket]", edge.From)
	}

	if len(edge.To) != 1 || edge.To[0] != "person" {
		test.Errorf("To = %v, want [person]", edge.To)
	}

	if edge.Cardinality != manifest.CardinalityManyToOne {
		test.Errorf("Cardinality = %q, want many-to-one", edge.Cardinality)
	}

	if edge.Ordered {
		test.Errorf("Ordered = true, want false for plain ref")
	}
}

func TestSynthesize_ListOfRefProducesManyToManyUnordered(test *testing.T) {
	loaded := &manifest.Manifest{
		EdgeTypes: manifest.EdgeTypes{},
		NodeTypes: map[string]manifest.NodeType{
			"person": {},
			"ticket": {Properties: []manifest.PropertyDecl{
				{Name: "watchers", Type: "list-of", ItemType: "ref", To: "person"},
			}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr != nil {
		test.Fatalf("Validate: %v", validateErr)
	}

	edge := loaded.EdgeTypes["watchers"]

	if edge.Cardinality != manifest.CardinalityManyToMany {
		test.Errorf("Cardinality = %q, want many-to-many", edge.Cardinality)
	}

	// Ref-property syntax cannot express ordering; synthesized edges
	// are always unordered. Pack authors who need ordering must declare
	// an explicit [edge-types.X] with ordered = "<prop>".
	if edge.Ordered {
		test.Errorf("Ordered = true, want false for synthesized list-of(ref)")
	}

	if edge.OrderedBy != "" {
		test.Errorf("OrderedBy = %q, want empty for synthesized list-of(ref)", edge.OrderedBy)
	}
}

func TestSynthesize_RefWithInverseAndAcyclic(test *testing.T) {
	loaded := &manifest.Manifest{
		EdgeTypes: manifest.EdgeTypes{},
		NodeTypes: map[string]manifest.NodeType{
			"ticket": {Properties: []manifest.PropertyDecl{
				{Name: "parent", Type: "ref", To: "ticket", Acyclic: true, Inverse: "children"},
			}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr != nil {
		test.Fatalf("Validate: %v", validateErr)
	}

	edge := loaded.EdgeTypes["parent"]

	if edge.Inverse != "children" {
		test.Errorf("Inverse = %q, want children", edge.Inverse)
	}

	if !edge.Acyclic {
		test.Errorf("Acyclic = false, want true")
	}
}

func TestSynthesize_RefWithWildcardTo(test *testing.T) {
	loaded := &manifest.Manifest{
		EdgeTypes: manifest.EdgeTypes{},
		NodeTypes: map[string]manifest.NodeType{
			"ticket": {Properties: []manifest.PropertyDecl{
				{Name: "linked", Type: "ref", To: "*"},
			}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr != nil {
		test.Fatalf("Validate: %v", validateErr)
	}

	edge := loaded.EdgeTypes["linked"]

	if len(edge.To) != 1 || edge.To[0] != "*" {
		test.Errorf("To = %v, want [*]", edge.To)
	}
}

func TestSynthesize_SamePropertyAcrossTypesExtendsFrom(test *testing.T) {
	loaded := &manifest.Manifest{
		EdgeTypes: manifest.EdgeTypes{},
		NodeTypes: map[string]manifest.NodeType{
			"person": {},
			"story":  {Properties: []manifest.PropertyDecl{{Name: "assignee", Type: "ref", To: "person"}}},
			"ticket": {Properties: []manifest.PropertyDecl{{Name: "assignee", Type: "ref", To: "person"}}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr != nil {
		test.Fatalf("Validate: %v", validateErr)
	}

	edge := loaded.EdgeTypes["assignee"]

	// From is alpha-sorted because synthesizeRefEdgeTypes iterates sorted node-type keys.
	if len(edge.From) != 2 || edge.From[0] != "story" || edge.From[1] != "ticket" {
		test.Errorf("From = %v, want [story ticket]", edge.From)
	}

	if edge.Cardinality != manifest.CardinalityManyToOne {
		test.Errorf("Cardinality = %q, want many-to-one", edge.Cardinality)
	}
}

func TestSynthesize_RejectsConflictingCardinality(test *testing.T) {
	loaded := &manifest.Manifest{
		EdgeTypes: manifest.EdgeTypes{},
		NodeTypes: map[string]manifest.NodeType{
			"person": {},
			"story":  {Properties: []manifest.PropertyDecl{{Name: "assignee", Type: "ref", To: "person"}}},
			"ticket": {Properties: []manifest.PropertyDecl{
				{Name: "assignee", Type: "list-of", ItemType: "ref", To: "person"},
			}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr == nil || !strings.Contains(validateErr.Error(), "cardinality") {
		test.Errorf("Validate: expected conflicting-cardinality error, got %v", validateErr)
	}
}

func TestSynthesize_RejectsConflictingTo(test *testing.T) {
	loaded := &manifest.Manifest{
		EdgeTypes: manifest.EdgeTypes{},
		NodeTypes: map[string]manifest.NodeType{
			"person": {},
			"team":   {},
			"story":  {Properties: []manifest.PropertyDecl{{Name: "assignee", Type: "ref", To: "person"}}},
			"ticket": {Properties: []manifest.PropertyDecl{{Name: "assignee", Type: "ref", To: "team"}}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr == nil || !strings.Contains(validateErr.Error(), "assignee") {
		test.Errorf("Validate: expected conflicting-to error, got %v", validateErr)
	}
}

func TestSynthesize_RejectsConflictingInverse(test *testing.T) {
	loaded := &manifest.Manifest{
		EdgeTypes: manifest.EdgeTypes{},
		NodeTypes: map[string]manifest.NodeType{
			"person": {},
			"story":  {Properties: []manifest.PropertyDecl{{Name: "assignee", Type: "ref", To: "person", Inverse: "stories"}}},
			"ticket": {Properties: []manifest.PropertyDecl{{Name: "assignee", Type: "ref", To: "person", Inverse: "tickets"}}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr == nil || !strings.Contains(validateErr.Error(), "assignee") {
		test.Errorf("Validate: expected conflicting-inverse error, got %v", validateErr)
	}
}

func TestSynthesize_RejectsCollisionWithExplicitEdgeType(test *testing.T) {
	loaded := &manifest.Manifest{
		EdgeTypes: manifest.EdgeTypes{
			"assignee": {
				From:        []string{"ticket"},
				To:          []string{"person"},
				Cardinality: manifest.CardinalityManyToOne,
			},
		},
		NodeTypes: map[string]manifest.NodeType{
			"person": {},
			"ticket": {Properties: []manifest.PropertyDecl{{Name: "assignee", Type: "ref", To: "person"}}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr == nil || !strings.Contains(validateErr.Error(), "auto-generated by ref property") {
		test.Errorf("Validate: expected auto-generated-collision error, got %v", validateErr)
	}
}

func TestLoad_ParsesHierarchyFields(test *testing.T) {
	tempDir := test.TempDir()
	manifestPath := filepath.Join(tempDir, "tusk.toml")
	content := `[workspace]
name = "demo"

[node-types.task]
description = "A task"

[edge-types.wbs-parent]
description = "WBS parent"
from        = ["task"]
to          = ["task"]
cardinality = "many-to-one"
acyclic     = true
hierarchy   = "wbs"
hierarchy-default = true
`

	if err := os.WriteFile(manifestPath, []byte(content), 0o644); err != nil {
		test.Fatalf("write manifest: %v", err)
	}

	loaded, err := manifest.Load(manifestPath)

	if err != nil {
		test.Fatalf("Load: %v", err)
	}

	edge, ok := loaded.EdgeTypes["wbs-parent"]

	if !ok {
		test.Fatalf("expected wbs-parent edge")
	}

	if edge.Hierarchy != "wbs" {
		test.Errorf("Hierarchy = %q, want %q", edge.Hierarchy, "wbs")
	}

	if !edge.HierarchyDefault {
		test.Errorf("HierarchyDefault = false, want true")
	}
}

func TestLoad_RejectsHierarchyAliasCollidingWithKeyword(test *testing.T) {
	tempDir := test.TempDir()
	manifestPath := filepath.Join(tempDir, "tusk.toml")
	content := `[workspace]
name = "demo"

[node-types.task]
description = "A task"

[edge-types.wbs-parent]
from        = ["task"]
to          = ["task"]
cardinality = "many-to-one"
hierarchy   = "tree"
`

	if err := os.WriteFile(manifestPath, []byte(content), 0o644); err != nil {
		test.Fatalf("write manifest: %v", err)
	}

	_, err := manifest.Load(manifestPath)

	if err == nil {
		test.Fatalf("expected error for keyword-aliased hierarchy")
	}

	if !strings.Contains(err.Error(), "collides with shortcut keyword") {
		test.Errorf("unexpected error: %v", err)
	}
}

func TestLoad_RejectsDuplicateHierarchyAliases(test *testing.T) {
	tempDir := test.TempDir()
	manifestPath := filepath.Join(tempDir, "tusk.toml")
	content := `[workspace]
name = "demo"

[node-types.task]
description = "A task"

[edge-types.a-parent]
from        = ["task"]
to          = ["task"]
cardinality = "many-to-one"
hierarchy   = "wbs"

[edge-types.b-parent]
from        = ["task"]
to          = ["task"]
cardinality = "many-to-one"
hierarchy   = "wbs"
`

	if err := os.WriteFile(manifestPath, []byte(content), 0o644); err != nil {
		test.Fatalf("write manifest: %v", err)
	}

	_, err := manifest.Load(manifestPath)

	if err == nil {
		test.Fatalf("expected error for duplicate hierarchy alias")
	}

	if !strings.Contains(err.Error(), "duplicate hierarchy alias") {
		test.Errorf("unexpected error: %v", err)
	}
}

func TestLoad_RejectsMultipleHierarchyDefaults(test *testing.T) {
	tempDir := test.TempDir()
	manifestPath := filepath.Join(tempDir, "tusk.toml")
	content := `[workspace]
name = "demo"

[node-types.task]
description = "A task"

[edge-types.a-parent]
from        = ["task"]
to          = ["task"]
cardinality = "many-to-one"
hierarchy   = "a"
hierarchy-default = true

[edge-types.b-parent]
from        = ["task"]
to          = ["task"]
cardinality = "many-to-one"
hierarchy   = "b"
hierarchy-default = true
`

	if err := os.WriteFile(manifestPath, []byte(content), 0o644); err != nil {
		test.Fatalf("write manifest: %v", err)
	}

	_, err := manifest.Load(manifestPath)

	if err == nil {
		test.Fatalf("expected error for two hierarchy defaults")
	}

	if !strings.Contains(err.Error(), "multiple edges declare hierarchy-default") {
		test.Errorf("unexpected error: %v", err)
	}
}

func TestLoad_RejectsHierarchyDefaultWithoutAlias(test *testing.T) {
	tempDir := test.TempDir()
	manifestPath := filepath.Join(tempDir, "tusk.toml")
	content := `[workspace]
name = "demo"

[node-types.task]
description = "A task"

[edge-types.wbs-parent]
from        = ["task"]
to          = ["task"]
cardinality = "many-to-one"
hierarchy-default = true
`

	if err := os.WriteFile(manifestPath, []byte(content), 0o644); err != nil {
		test.Fatalf("write manifest: %v", err)
	}

	_, err := manifest.Load(manifestPath)

	if err == nil {
		test.Fatalf("expected error for hierarchy-default without hierarchy alias")
	}

	if !strings.Contains(err.Error(), "hierarchy-default requires hierarchy") {
		test.Errorf("unexpected error: %v", err)
	}
}

func TestLoad_SynthesizesHierarchyForBareParentEdge(test *testing.T) {
	tempDir := test.TempDir()
	manifestPath := filepath.Join(tempDir, "tusk.toml")
	content := `[workspace]
name = "demo"

[node-types.task]
description = "A task"

[edge-types.parent]
description = "Parent edge"
from        = ["task"]
to          = ["task"]
cardinality = "many-to-one"
acyclic     = true
`

	if err := os.WriteFile(manifestPath, []byte(content), 0o644); err != nil {
		test.Fatalf("write manifest: %v", err)
	}

	loaded, err := manifest.Load(manifestPath)

	if err != nil {
		test.Fatalf("Load: %v", err)
	}

	edge := loaded.EdgeTypes["parent"]

	if edge.Hierarchy != "parent" {
		test.Errorf("Hierarchy = %q, want %q", edge.Hierarchy, "parent")
	}

	if !edge.HierarchyDefault {
		test.Errorf("HierarchyDefault = false, want true (back-compat)")
	}
}

func TestLoad_BareParentDoesNotOverrideExplicitHierarchy(test *testing.T) {
	tempDir := test.TempDir()
	manifestPath := filepath.Join(tempDir, "tusk.toml")
	content := `[workspace]
name = "demo"

[node-types.task]
description = "A task"

[edge-types.parent]
description = "Parent edge"
from        = ["task"]
to          = ["task"]
cardinality = "many-to-one"
hierarchy   = "kanban"
`

	if err := os.WriteFile(manifestPath, []byte(content), 0o644); err != nil {
		test.Fatalf("write manifest: %v", err)
	}

	loaded, err := manifest.Load(manifestPath)

	if err != nil {
		test.Fatalf("Load: %v", err)
	}

	if loaded.EdgeTypes["parent"].Hierarchy != "kanban" {
		test.Errorf("explicit hierarchy %q was overwritten", "kanban")
	}
}

func TestLoad_BareParentDoesNotClaimDefaultWhenOtherDefaultExists(test *testing.T) {
	tempDir := test.TempDir()
	manifestPath := filepath.Join(tempDir, "tusk.toml")
	content := `[workspace]
name = "demo"

[node-types.task]
description = "A task"

[edge-types.parent]
description = "Bare parent edge (back-compat)"
from        = ["task"]
to          = ["task"]
cardinality = "many-to-one"

[edge-types.wbs-parent]
description = "WBS hierarchy"
from        = ["task"]
to          = ["task"]
cardinality = "many-to-one"
hierarchy   = "wbs"
hierarchy-default = true
`

	if err := os.WriteFile(manifestPath, []byte(content), 0o644); err != nil {
		test.Fatalf("write manifest: %v", err)
	}

	loaded, err := manifest.Load(manifestPath)

	if err != nil {
		test.Fatalf("Load: %v", err)
	}

	bareParent := loaded.EdgeTypes["parent"]

	if bareParent.Hierarchy != "parent" {
		test.Errorf("bare parent Hierarchy = %q, want %q", bareParent.Hierarchy, "parent")
	}

	if bareParent.HierarchyDefault {
		test.Errorf("bare parent should not claim default when another edge already does")
	}
}

// loadTOMLString writes body to a temp tusk.toml and returns the result of
// manifest.Load. Used by the polymorphic `ordered` decoding tests.
func loadTOMLString(test *testing.T, body string) (*manifest.Manifest, error) {
	test.Helper()

	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")

	if writeErr := os.WriteFile(path, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write tusk.toml: %v", writeErr)
	}

	return manifest.Load(path)
}

// loadTOMLFromString is the panicking variant for happy-path tests that
// expect the manifest to load cleanly.
func loadTOMLFromString(test *testing.T, body string) *manifest.Manifest {
	test.Helper()

	loaded, err := loadTOMLString(test, body)

	if err != nil {
		test.Fatalf("loadTOMLString: %v", err)
	}

	return loaded
}

func TestLoad_OrderedFalseDefault(test *testing.T) {
	manifest := loadTOMLFromString(test, `
[node-types.thing]
properties = []

[edge-types.relates-to]
from        = ["thing"]
to          = ["thing"]
cardinality = "many-to-many"
`)

	edge := manifest.EdgeTypes["relates-to"]

	if edge.Ordered {
		test.Errorf("Ordered should be false; got true")
	}

	if edge.OrderedBy != "" {
		test.Errorf("OrderedBy should be empty; got %q", edge.OrderedBy)
	}
}

func TestLoad_OrderedExplicitFalse(test *testing.T) {
	manifest := loadTOMLFromString(test, `
[node-types.thing]
properties = [
  { name = "order", type = "int" },
]

[edge-types.relates-to]
from        = ["thing"]
to          = ["thing"]
cardinality = "many-to-many"
ordered     = false
`)

	edge := manifest.EdgeTypes["relates-to"]

	if edge.Ordered {
		test.Errorf("Ordered should be false for explicit ordered = false; got true")
	}

	if edge.OrderedBy != "" {
		test.Errorf("OrderedBy should be empty for explicit ordered = false; got %q", edge.OrderedBy)
	}
}

func TestLoad_OrderedTrueAliasesToOrderProperty(test *testing.T) {
	manifest := loadTOMLFromString(test, `
[node-types.thing]
properties = [
  { name = "order", type = "int" },
]

[edge-types.relates-to]
from        = ["thing"]
to          = ["thing"]
cardinality = "many-to-many"
ordered     = true
`)

	edge := manifest.EdgeTypes["relates-to"]

	if !edge.Ordered {
		test.Errorf("Ordered should be true")
	}

	if edge.OrderedBy != "order" {
		test.Errorf("OrderedBy should default to %q; got %q", "order", edge.OrderedBy)
	}
}

func TestLoad_OrderedByExplicitProperty(test *testing.T) {
	manifest := loadTOMLFromString(test, `
[node-types.thing]
properties = [
  { name = "rank", type = "int" },
]

[edge-types.relates-to]
from        = ["thing"]
to          = ["thing"]
cardinality = "many-to-many"
ordered     = "rank"
`)

	edge := manifest.EdgeTypes["relates-to"]

	if !edge.Ordered {
		test.Errorf("Ordered should be true")
	}

	if edge.OrderedBy != "rank" {
		test.Errorf("OrderedBy should be %q; got %q", "rank", edge.OrderedBy)
	}
}

func TestLoad_OrderedByUnknownPropertyRejects(test *testing.T) {
	_, loadErr := loadTOMLString(test, `
[node-types.thing]
properties = [
  { name = "title-only", type = "string" },
]

[edge-types.relates-to]
from        = ["thing"]
to          = ["thing"]
cardinality = "many-to-many"
ordered     = "rank"
`)

	if loadErr == nil {
		test.Fatalf("expected error: ordered references unknown property")
	}
}

func TestLoad_OrderedByMissingOnOneOfFromRejects(test *testing.T) {
	_, loadErr := loadTOMLString(test, `
[node-types.thing-a]
properties = [
  { name = "rank", type = "int" },
]

[node-types.thing-b]
properties = []

[edge-types.relates-to]
from        = ["thing-a", "thing-b"]
to          = ["thing-a"]
cardinality = "many-to-many"
ordered     = "rank"
`)

	if loadErr == nil {
		test.Fatalf("expected error: ordered property missing on thing-b")
	}
}

func TestLoad_OrderedByOnWildcardFromRejects(test *testing.T) {
	_, loadErr := loadTOMLString(test, `
[node-types.thing]
properties = [
  { name = "rank", type = "int" },
]

[edge-types.relates-to]
from        = ["*"]
to          = ["thing"]
cardinality = "many-to-many"
ordered     = "rank"
`)

	if loadErr == nil {
		test.Fatalf("expected error: ordered=<prop> not allowed with from=[*]")
	}
}

func TestLoad_OrderedByNonSortablePropertyRejects(test *testing.T) {
	_, loadErr := loadTOMLString(test, `
[node-types.thing]
properties = [
  { name = "tags", type = "list-of", item-type = "string" },
]

[edge-types.relates-to]
from        = ["thing"]
to          = ["thing"]
cardinality = "many-to-many"
ordered     = "tags"
`)

	if loadErr == nil {
		test.Fatalf("expected error: ordered property must be sortable")
	}
}

func TestLoad_OrderedByWithSingleQuoteRejects(test *testing.T) {
	_, loadErr := loadTOMLString(test, `
[node-types.thing]
properties = [
  { name = "rank", type = "int" },
]

[edge-types.relates-to]
from        = ["thing"]
to          = ["thing"]
cardinality = "many-to-many"
ordered     = "it's-bad"
`)

	if loadErr == nil {
		test.Fatalf("expected error: ordered property name with single-quote should reject")
	}
}
