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
    { name = "ordered_list", type = "list-of", item-type = "ref", to = "person", ordered = true },
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

	if len(ticket.Properties) != 4 {
		test.Fatalf("ticket.Properties count = %d, want 4", len(ticket.Properties))
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

	ordered := ticket.Properties[3]

	if ordered.Type != "list-of" || ordered.ItemType != "ref" || !ordered.Ordered {
		test.Errorf("ordered_list = %+v", ordered)
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

func TestSynthesize_ListOfRefProducesManyToManyOrdered(test *testing.T) {
	loaded := &manifest.Manifest{
		EdgeTypes: manifest.EdgeTypes{},
		NodeTypes: map[string]manifest.NodeType{
			"person": {},
			"ticket": {Properties: []manifest.PropertyDecl{
				{Name: "watchers", Type: "list-of", ItemType: "ref", To: "person", Ordered: true},
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

	if !edge.Ordered {
		test.Errorf("Ordered = false, want true for list-of(ref) with Ordered=true")
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
