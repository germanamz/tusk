# Tusk v1 — Plan 7.b: Node-Types

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the `[node-types]` manifest section + property-type validation as the structural-schema layer described in v1 design spec §7.1 layer 2, plus extend the behavior-pack collision detector to cover declared properties versus behavior reservations. Tusk-owned writes reject on type/required/enum violations and on unsetting required properties; undeclared properties pass with stderr warning + drift surfaced by `tusk doctor`.

**Architecture:** A new pure validator `ValidateProperties` lives in `internal/node/types.go`, mirroring the structural-validation pattern that `internal/node/edges.go` already established for edge-type validation. The manifest grows a `NodeTypes` field and a typed property-declaration shape. `node.Service.Create/Modify` and `internal/reindex` invoke the validator at fixed insertion points; hard errors abort tusk-owned writes; drift entries surface stderr warnings + persist rows in a new `property_drift` SQLite table that `internal/doctor` reads as four new Issue kinds. The behavior package's existing collision detector grows a `DeclaredKey` parameter so engine startup also rejects overlap between declared properties and behavior-pack reservations.

**Tech Stack:** Go 1.26, the existing `internal/{manifest,node,reindex,doctor,index,behavior,mcp,workspace,lock}` packages, no new dependencies.

**Spec reference:** `docs/superpowers/specs/2026-05-07-tusk-v1-7b-node-types-design.md` (Plan 7.b sub-spec) and `docs/superpowers/specs/2026-05-05-tusk-v1-rebuild-design.md` §7.1 + §7.3.

**Style rules:** Code respects `STYLE.md` — minimum 2-character identifiers (`*testing.T` → `test *testing.T`), blank lines around `err` guards, named errors on shadow. Lefthook enforces pre-commit; never `--no-verify`.

**Plan-doc style:** Tests are shown in full (they are the precise behavioral spec). Production code is described as behavior + signature + key invariants, with type sketches and TOML/SQL/JSON examples where they add clarity. Implementer subagents write code from the failing test plus the behavior description plus the spec reference. This is intentional per the user's feedback after Plan 7.

---

## File Structure

**Created:**

```
internal/node/
  types.go                       # ValidateProperties + WhichRequiredWereUnset; pure validator;
                                 # PropertyValidationError typed wrapper used by Service.Create / Modify
                                 # so MCP can errors.As to access the structured []PropertyError slice
  types_test.go                  # all per-type acceptance/rejection cases

internal/index/
  property_drift_repo.go         # PropertyDriftRow + PropertyDriftRepo: Append, ListAll, ClearForNode, CountAll
  property_drift_repo_test.go    # mirror of workflow_drift_repo_test.go

# No new files in cmd/tusk/ or internal/mcp/ — Plan 7.b extends existing wiring.
```

**Modified:**

```
internal/manifest/manifest.go        # add NodeType + PropertyDecl types; NodeTypes map[string]NodeType field
internal/manifest/loader.go          # decode + structural validation rules from spec §2.3
internal/manifest/loader_test.go     # cover happy path + every structural rejection rule

internal/index/index.go              # add `property_drift` table + node_id index to schema
internal/index/index_test.go         # assert `property_drift` table is present

internal/node/service.go             # NewServiceWithBehaviors grows nodeTypes + propertyDrift parameters;
                                     # Create + Modify dispatch ValidateProperties + WhichRequiredWereUnset
internal/node/service_test.go        # cover create/modify rejection + drift + clean-pass clears

internal/reindex/reindex.go          # Config grows NodeTypes + PropertyDrift; per-file warn-mode validator;
                                     # Report.PropertyViolations; summary line update
internal/reindex/reindex_test.go     # cover off-schema property producing drift + summary count

internal/doctor/doctor.go            # IssueUndeclaredProperty + IssueTypeMismatch + IssueRequiredMissing +
                                     # IssueEnumViolation; Config.PropertyDrift; Run reads property drift
internal/doctor/doctor_test.go       # cover each new Issue kind

internal/behavior/engine.go          # NewEngine signature grows []DeclaredKey; collision detector extension
internal/behavior/registry.go        # BuildEngineWithDeclaredKeys path
internal/behavior/registry_test.go   # cover declared-key collision rejection + same-prop-different-type passes

cmd/tusk/behavior_registry.go        # newBehaviorEngine grows declaredKeysFrom + uses BuildEngineWithDeclaredKeys
cmd/tusk/cmd_node_create.go          # NewServiceWithBehaviors gets nodeTypes + propertyDrift
cmd/tusk/cmd_node_create_test.go     # workflow-style cases for required missing + type mismatch + drift
cmd/tusk/cmd_node_modify.go          # NewServiceWithBehaviors gets nodeTypes + propertyDrift
cmd/tusk/cmd_node_modify_test.go     # cases for required unset + drift visible to doctor
cmd/tusk/cmd_reindex.go              # reindex.Config gets NodeTypes + PropertyDrift; summary line
cmd/tusk/cmd_reindex_test.go         # cover summary count
cmd/tusk/cmd_doctor.go               # doctor.Config.PropertyDrift wired
cmd/tusk/cmd_doctor_test.go          # cover rendered Issue messages

internal/mcp/runtime.go              # Runtime gains PropertyDrift; Open + ReloadManifest thread NodeTypes
                                     # and PropertyDrift through to NodeService and the engine
internal/mcp/runtime_test.go         # cover ReloadManifest carries the new wiring
internal/mcp/tools.go                # tusk_node_modify + tusk_node_create return structured node-types-rejection;
                                     # success payload grows warnings on undeclared-property drift
internal/mcp/tools_test.go           # cover structured rejection shape + warnings shape
```

**Excluded for Plan 7.b** (per spec §1.2 / §10 ledger):

- `ref` property type and auto-generated edge types — Plan 7.c with built-in type packs.
- Nested `list-of(list-of(...))` — flat lists of scalars only.
- Built-in type packs (`kanban`, `vault`, `tags`) consuming `[node-types]` — Plan 7.c.
- `OnNodeRead*` firing site — still no v1 consumer.
- `tusk_doctor` MCP tool surfacing the new property Issue kinds — carries over from Plan 7.

---

## Module Conventions for Plan 7.b

**Validator purity.** `ValidateProperties` is pure — no I/O, no graph reads, no goroutines. The signature is `(parsed *node.Node, decls map[string]manifest.NodeType) PropertyValidationResult`. Tests construct `*node.Node` literals directly and pass synthetic `decls` maps. Race-free; trivially parallel-safe in tests.

**Result envelope.** The validator returns `PropertyValidationResult{HardErrors, Drift}`. Callers translate the two slices into action: hard errors → tusk-owned writes reject + reindex captures as drift rows; drift → tusk-owned writes warn + persist + continue, reindex persists silently. This split is the same shape Plan 7's recovery handling follows; the validator stays decision-free.

**Drift surface.** `property_drift` is a new SQLite table parallel to `workflow_drift`. Schema and idempotency rules mirror `workflow_drift` exactly. `PropertyDriftRepo` exposes `Append`, `ListAll`, `ClearForNode`, `CountAll` — same surface as `WorkflowDriftRepo`. Doctor reads from both and merges at the read side.

**Service constructor change.** `NewServiceWithBehaviors` (the Plan 7 production constructor) grows two parameters: `nodeTypes map[string]manifest.NodeType` and `propertyDrift *index.PropertyDriftRepo`. Pre-Plan-7.b constructors stay; their callers (test-only paths now) leave the new fields as zero values, which the validator treats as "untyped node, pass through".

**Engine signature change.** `behavior.NewEngine` grows a `[]behavior.DeclaredKey` parameter alongside the existing `[]Instance`. `Registry.BuildEngineWithDeclaredKeys(loaded, declaredKeys)` is the production path; the existing `BuildEngine(loaded)` (instances-only) stays for tests. The wiring layer (`cmd/tusk/behavior_registry.go` and `internal/mcp/runtime.go`'s `buildBehaviorEngine`) constructs the declared-keys slice from `loaded.NodeTypes`.

**Insertion point in `Service.Create / Modify`.** Property validation runs after edge validation and cycle check, before any behavior-pack hook firing. This is the same slot the workflow validator could have used if it weren't structured as a hook — symmetric with edge validation.

**Insertion point in `reindex.Run`.** Property validation runs alongside the workflow validation (warn mode) — after the per-file `Repo.Upsert` + `Edges.UpsertAll`, after the workflow validator's drift writes. Indexing never aborts on property issues.

**TDD discipline.** Each task: write the failing test → run to confirm fail → write the minimal implementation → run to confirm pass → commit. Lefthook pre-commit (gofmt, vet, lint, test) runs at each commit; never `--no-verify`. If lefthook blocks, fix the underlying issue.

---

## Task 0: Pre-flight verification

**Files:** none.

- [ ] **Step 1: Verify branch + clean state**

```bash
git rev-parse --abbrev-ref HEAD
git status --porcelain
```

Expected: `feat/plan-7b`; only the spec doc in `docs/superpowers/` (already committed) and possibly the local `tusk` binary as untracked.

- [ ] **Step 2: Verify pre-Plan-7.b is green**

```bash
make build && make test
```

Expected: build succeeds; all tests pass.

- [ ] **Step 3: Verify spec is in tree**

```bash
test -f docs/superpowers/specs/2026-05-07-tusk-v1-7b-node-types-design.md && echo OK
```

Expected: `OK`.

---

## Task 1: Manifest — `NodeType`, `PropertyDecl`, decode wiring

**Files:** Modify `internal/manifest/manifest.go`, `internal/manifest/loader_test.go`.

This task lands the manifest type definitions and asserts the section decodes. Structural validation comes in Tasks 2 and 3.

- [ ] **Step 1: Append failing test to `internal/manifest/loader_test.go`**

```go
func TestLoad_DecodesNodeTypesSection(test *testing.T) {
	dir := test.TempDir()
	manifestPath := filepath.Join(dir, "tusk.toml")

	content := `
[workspace]
name = "test"

[node-types.ticket]
description = "A unit of trackable work"
properties = [
    { name = "summary",    type = "string", required = true },
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

	titleProp := ticket.Properties[0]

	if titleProp.Name != "summary" || titleProp.Type != "string" || !titleProp.Required {
		test.Errorf("ticket.Properties[0] = %+v", titleProp)
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
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/manifest/... -run TestLoad_DecodesNodeTypesSection
```

Expected: FAIL — `manifest.Manifest` lacks the `NodeTypes` field.

- [ ] **Step 3: Add types and field to `internal/manifest/manifest.go`**

Add `NodeType` and `PropertyDecl` types matching the field shape used in the test. Add a `NodeTypes` field to `Manifest` with TOML tag `"node-types"`. Type sketches:

```go
type Manifest struct {
    // ... existing fields ...
    NodeTypes map[string]NodeType `toml:"node-types"`
}

type NodeType struct {
    Description string         `toml:"description"`
    Properties  []PropertyDecl `toml:"properties"`
}

type PropertyDecl struct {
    Name        string   `toml:"name"`
    Type        string   `toml:"type"`
    ItemType    string   `toml:"item-type"`
    Values      []string `toml:"values"`
    Required    bool     `toml:"required"`
    Description string   `toml:"description"`
}
```

No structural validation in this task — Task 2 adds it.

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/manifest/... -run TestLoad_DecodesNodeTypesSection -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/manifest/manifest.go internal/manifest/loader_test.go
git commit -m "feat(manifest): NodeType and PropertyDecl types; NodeTypes field"
```

---

## Task 2: Manifest — structural validation, basic rules

**Files:** Modify `internal/manifest/loader.go`, `internal/manifest/loader_test.go`.

This task lands the rules for type-name and property-name basics: empty checks, reserved names, duplicates within a type. The type-specific rules (enum, list-of) come in Task 3.

- [ ] **Step 1: Append failing tests covering the basic rules**

Add the following tests to `internal/manifest/loader_test.go`. Each test asserts that `manifest.Validate(loaded)` returns an error containing the named token (so the implementer's error messages just need to mention each rejection's subject).

```go
func TestValidate_RejectsNodeTypeWithEmptyName(test *testing.T) {
	loaded := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{
			"": {Properties: []manifest.PropertyDecl{{Name: "summary", Type: "string"}}},
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
				{Name: "n",     Type: "int"},
				{Name: "f",     Type: "float"},
				{Name: "b",     Type: "bool"},
				{Name: "d",     Type: "date"},
				{Name: "dt",    Type: "datetime"},
				{Name: "md",    Type: "markdown"},
			}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr != nil {
		test.Errorf("Validate: %v", validateErr)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/manifest/... -run "TestValidate_(RejectsNodeTypeWithEmptyName|RejectsPropertyWithEmptyName|RejectsReservedPropertyName|RejectsDuplicatePropertyName|RejectsUnknownPropertyType|AcceptsHappyPath)"
```

Expected: FAIL — current `Validate` doesn't traverse `NodeTypes`.

- [ ] **Step 3: Extend `validateBehaviors` (rename to `validate` extension or add `validateNodeTypes`) in `internal/manifest/loader.go`**

The structural rules to enforce, in any order:

- Empty type name → error mentioning "empty type name".
- Each property's name is non-empty (else "empty property name").
- Each property's name is not in the reserved set `{"type", "title"}` (error mentions the reserved name).
- Property names within the same type are unique (error mentions "duplicate" and the property name).
- Property type is in the supported set: `string`, `int`, `float`, `bool`, `date`, `datetime`, `enum`, `markdown`, `list-of` (error mentions the offending unknown type string).

Hook the new helper into the existing `Validate` function (which already calls `validateBehaviors`). Add `validateNodeTypes(loaded *Manifest) error` and call it from `Validate`.

The supported type set may live as a package-level var or const set; the implementer chooses the idiom. The error-message tokens listed above are what the tests check for — preserve them.

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/manifest/... -v
```

Expected: every existing test plus the six new ones PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/manifest/loader.go internal/manifest/loader_test.go
git commit -m "feat(manifest): structural validation for [node-types] basics"
```

---

## Task 3: Manifest — structural validation, enum and list-of rules

**Files:** Modify `internal/manifest/loader.go`, `internal/manifest/loader_test.go`.

- [ ] **Step 1: Append failing tests covering the type-specific rules**

```go
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
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/manifest/... -run "TestValidate_(RejectsEnum|RejectsListOf|RejectsValues|RejectsItemType|AcceptsListOfEnum)"
```

Expected: FAIL — these rules aren't enforced yet.

- [ ] **Step 3: Extend `validateNodeTypes` in `internal/manifest/loader.go`**

Per-property type-specific rules (in addition to the basic rules from Task 2):

- For `enum`:
  - `Values` is required and non-empty.
  - No element of `Values` is the empty string.
  - No duplicate elements in `Values`.
- For `list-of`:
  - `ItemType` is required and non-empty.
  - `ItemType` must not be `list-of` (no nesting); error message contains "nest".
  - When `ItemType = "enum"`, `Values` rules apply (required, non-empty, no empty elements, no duplicates).
- Misplaced constraint keys:
  - `Values` is non-empty on a property whose type is not `enum` and not (`list-of` with `item-type = "enum"`) → error with "values".
  - `ItemType` is non-empty on a property whose type is not `list-of` → error with "item-type".

The error tokens listed above are what the tests check for. The implementer chooses how to organize the helper (one big switch or per-type sub-helpers).

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/manifest/... -v
```

Expected: every existing test plus the nine new ones PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/manifest/loader.go internal/manifest/loader_test.go
git commit -m "feat(manifest): structural validation for enum and list-of rules"
```

---

## Task 4: Validator — `ValidateProperties` skeleton (untyped + required)

**Files:** Create `internal/node/types.go`, `internal/node/types_test.go`.

This task lands the validator's public surface and the two simplest branches (untyped pass-through + required-missing). Per-type rules come in Task 5; list-of and the modify-only helper in Task 6.

- [ ] **Step 1: Write the failing tests in `internal/node/types_test.go`**

```go
package node_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

func TestValidateProperties_UntypedNodePassThrough(test *testing.T) {
	parsed := &node.Node{Type: "unknown", Properties: map[string]any{"any-key": "any-value"}}

	result := node.ValidateProperties(parsed, map[string]manifest.NodeType{})

	if len(result.HardErrors) != 0 || len(result.Drift) != 0 {
		test.Errorf("untyped node should pass through, got %+v", result)
	}
}

func TestValidateProperties_RequiredPresent(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"summary": "ok"}}

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "summary", Type: "string", Required: true}}},
	}

	result := node.ValidateProperties(parsed, decls)

	if len(result.HardErrors) != 0 || len(result.Drift) != 0 {
		test.Errorf("present required should pass, got %+v", result)
	}
}

func TestValidateProperties_RequiredMissing(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{}}

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{
			{Name: "summary", Type: "string", Required: true},
			{Name: "due",   Type: "date",   Required: true},
		}},
	}

	result := node.ValidateProperties(parsed, decls)

	if len(result.HardErrors) != 2 {
		test.Fatalf("HardErrors count = %d, want 2; got %+v", len(result.HardErrors), result.HardErrors)
	}

	if result.HardErrors[0].Kind != node.ErrRequiredMissing || result.HardErrors[0].Property != "summary" {
		test.Errorf("HardErrors[0] = %+v", result.HardErrors[0])
	}

	if result.HardErrors[1].Property != "due" {
		test.Errorf("HardErrors[1] = %+v", result.HardErrors[1])
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/node/... -run TestValidateProperties
```

Expected: FAIL — `node.ValidateProperties`, `node.PropertyValidationResult`, `node.PropertyError`, `node.ErrRequiredMissing` undefined.

- [ ] **Step 3: Create `internal/node/types.go`**

Type sketches and behavior:

```go
type PropertyValidationResult struct {
    HardErrors []PropertyError
    Drift      []PropertyDrift
}

type PropertyError struct {
    Kind     PropertyErrorKind
    Property string
    Type     string  // declared type rendering, e.g. "int" or "list-of(string)"
    Value    any     // observed value (for type-mismatch / enum)
    Reason   string  // human-readable detail
}

type PropertyErrorKind int

const (
    ErrTypeMismatch PropertyErrorKind = iota
    ErrRequiredMissing
    ErrEnumViolation
    ErrCannotUnsetRequired
)

type PropertyDrift struct {
    Property string
    Reason   string
}
```

`ValidateProperties` algorithm in this task:

1. If `decls[parsed.Type]` is absent, return an empty result (untyped pass-through).
2. For each `PropertyDecl` with `Required = true` whose `Name` is absent from `parsed.Properties`, append `PropertyError{Kind: ErrRequiredMissing, Property: decl.Name, Type: <render of decl.Type>, Reason: "property \"<name>\" is required (declared in [node-types.<t>])"}`. Order: declaration order (Task 4's third test pins "summary before due" because the declaration order is summary, due).
3. Per-property loop is empty in this task — Task 5 fills it.
4. Return.

The "declared type rendering" helper (e.g., "list-of(string)" for list-of+string) is small but lives near the validator; tests will pin its output in later tasks.

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/node/... -run TestValidateProperties -v
```

Expected: 3 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/node/types.go internal/node/types_test.go
git commit -m "feat(node): ValidateProperties skeleton — untyped pass-through + required check"
```

---

## Task 5: Validator — per-scalar-type rules

**Files:** Modify `internal/node/types.go`, `internal/node/types_test.go`.

Add the per-scalar-type acceptance/rejection rules from spec §3.4. List-of and the modify-only helper come in Task 6.

- [ ] **Step 1: Append failing tests, one positive + one negative per scalar type**

```go
func TestValidateProperties_StringAccepts(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"name": "hi"}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "name", Type: "string"}}},
	}

	result := node.ValidateProperties(parsed, decls)

	if len(result.HardErrors) != 0 || len(result.Drift) != 0 {
		test.Errorf("expected pass; got %+v", result)
	}
}

func TestValidateProperties_StringRejectsNonString(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"name": 42}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "name", Type: "string"}}},
	}

	result := node.ValidateProperties(parsed, decls)

	if len(result.HardErrors) != 1 || result.HardErrors[0].Kind != node.ErrTypeMismatch {
		test.Errorf("expected ErrTypeMismatch; got %+v", result)
	}
}

func TestValidateProperties_IntAccepts(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"n": 7}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "n", Type: "int"}}},
	}

	if got := node.ValidateProperties(parsed, decls); len(got.HardErrors) != 0 {
		test.Errorf("expected pass; got %+v", got)
	}
}

func TestValidateProperties_IntRejectsFloat(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"n": 1.5}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "n", Type: "int"}}},
	}

	if got := node.ValidateProperties(parsed, decls); len(got.HardErrors) != 1 || got.HardErrors[0].Kind != node.ErrTypeMismatch {
		test.Errorf("expected ErrTypeMismatch; got %+v", got)
	}
}

func TestValidateProperties_FloatAcceptsInt(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"f": 7}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "f", Type: "float"}}},
	}

	if got := node.ValidateProperties(parsed, decls); len(got.HardErrors) != 0 {
		test.Errorf("expected pass (int promotes to float); got %+v", got)
	}
}

func TestValidateProperties_FloatRejectsString(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"f": "1.5"}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "f", Type: "float"}}},
	}

	if got := node.ValidateProperties(parsed, decls); len(got.HardErrors) != 1 {
		test.Errorf("expected error; got %+v", got)
	}
}

func TestValidateProperties_BoolAcceptsBool(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"b": true}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "b", Type: "bool"}}},
	}

	if got := node.ValidateProperties(parsed, decls); len(got.HardErrors) != 0 {
		test.Errorf("expected pass; got %+v", got)
	}
}

func TestValidateProperties_BoolRejectsStringTrue(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"b": "true"}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "b", Type: "bool"}}},
	}

	if got := node.ValidateProperties(parsed, decls); len(got.HardErrors) != 1 {
		test.Errorf("expected error; got %+v", got)
	}
}

func TestValidateProperties_DateAcceptsISO(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"d": "2026-05-15"}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "d", Type: "date"}}},
	}

	if got := node.ValidateProperties(parsed, decls); len(got.HardErrors) != 0 {
		test.Errorf("expected pass; got %+v", got)
	}
}

func TestValidateProperties_DateRejectsBadString(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"d": "tomorrow"}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "d", Type: "date"}}},
	}

	if got := node.ValidateProperties(parsed, decls); len(got.HardErrors) != 1 {
		test.Errorf("expected error; got %+v", got)
	}
}

func TestValidateProperties_DatetimeAcceptsRFC3339(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"dt": "2026-05-15T10:00:00Z"}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "dt", Type: "datetime"}}},
	}

	if got := node.ValidateProperties(parsed, decls); len(got.HardErrors) != 0 {
		test.Errorf("expected pass; got %+v", got)
	}
}

func TestValidateProperties_EnumInValuesAccepts(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"stage": "active"}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{
			Name: "stage", Type: "enum", Values: []string{"pending", "active", "completed"},
		}}},
	}

	if got := node.ValidateProperties(parsed, decls); len(got.HardErrors) != 0 {
		test.Errorf("expected pass; got %+v", got)
	}
}

func TestValidateProperties_EnumOutOfValuesRejects(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"stage": "shipping"}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{
			Name: "stage", Type: "enum", Values: []string{"pending", "active", "completed"},
		}}},
	}

	if got := node.ValidateProperties(parsed, decls); len(got.HardErrors) != 1 || got.HardErrors[0].Kind != node.ErrEnumViolation {
		test.Errorf("expected ErrEnumViolation; got %+v", got)
	}
}

func TestValidateProperties_MarkdownAcceptsString(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"m": "# heading"}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "m", Type: "markdown"}}},
	}

	if got := node.ValidateProperties(parsed, decls); len(got.HardErrors) != 0 {
		test.Errorf("expected pass; got %+v", got)
	}
}

func TestValidateProperties_UndeclaredPropertyAppearsInDrift(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"assignee": "bob"}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "summary", Type: "string"}}},
	}

	got := node.ValidateProperties(parsed, decls)

	if len(got.HardErrors) != 0 {
		test.Errorf("undeclared should not be HardError; got %+v", got.HardErrors)
	}

	if len(got.Drift) != 1 || got.Drift[0].Property != "assignee" {
		test.Errorf("expected one Drift entry for assignee; got %+v", got.Drift)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/node/... -run TestValidateProperties
```

Expected: FAIL — the per-type loop in `ValidateProperties` is empty.

- [ ] **Step 3: Add the per-property loop in `internal/node/types.go`**

Behavior to implement, per spec §3.4:

For each `(name, value)` in `parsed.Properties`:

1. Look up `decl` by name in the type's properties slice. If absent: append `PropertyDrift{Property: name, Reason: "not declared on type \"" + parsed.Type + "\""}`. Continue.
2. Validate `value` against `decl.Type`:
   - `string` / `markdown`: accept any Go `string`. Else `ErrTypeMismatch`.
   - `int`: accept `int` and `int64`. Reject `float64`, `string`, `bool`. (BurntSushi YAML gives `int64`; the validator should also accept `int` for tests' literal values.)
   - `float`: accept `float64`, `int`, `int64`. Reject other types.
   - `bool`: accept `bool`. Reject everything else.
   - `date`: accept a `string` that `time.Parse(time.DateOnly, value)` accepts. Else `ErrTypeMismatch`.
   - `datetime`: accept a `string` that `time.Parse(time.RFC3339, value)` accepts.
   - `enum`: accept a `string` whose value is in `decl.Values`. Else `ErrEnumViolation` (note the distinct kind).
   - `list-of`: handled in Task 6.

Each `PropertyError` carries `Property: name`, `Type: <render of decl>`, `Value: value`, and a one-line `Reason` describing the mismatch (e.g., "expected type \"int\" but value \"high\" is not an integer", "value \"shipping\" not in declared enum [pending, active, completed]").

The "render of decl" helper returns:
- The bare type name for scalars (`"int"`, `"date"`, etc.).
- `"enum(<comma-joined values>)"` for `enum`.
- `"list-of(<inner>)"` for `list-of`.

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/node/... -run TestValidateProperties -v
```

Expected: every TestValidateProperties_* PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/node/types.go internal/node/types_test.go
git commit -m "feat(node): per-scalar-type rules for ValidateProperties"
```

---

## Task 6: Validator — list-of validation + WhichRequiredWereUnset

**Files:** Modify `internal/node/types.go`, `internal/node/types_test.go`.

- [ ] **Step 1: Append failing tests for list-of and the modify-helper**

```go
func TestValidateProperties_ListOfStringAccepts(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"labels": []any{"auth", "security"}}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "labels", Type: "list-of", ItemType: "string"}}},
	}

	if got := node.ValidateProperties(parsed, decls); len(got.HardErrors) != 0 {
		test.Errorf("expected pass; got %+v", got)
	}
}

func TestValidateProperties_ListOfStringRejectsMixedElement(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"labels": []any{"auth", 42}}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "labels", Type: "list-of", ItemType: "string"}}},
	}

	got := node.ValidateProperties(parsed, decls)

	if len(got.HardErrors) != 1 {
		test.Errorf("expected one HardError for the int element; got %+v", got)
	}
}

func TestValidateProperties_ListOfEnumAccepts(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"stages": []any{"draft", "review"}}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{
			Name: "stages", Type: "list-of", ItemType: "enum", Values: []string{"draft", "review", "shipped"},
		}}},
	}

	if got := node.ValidateProperties(parsed, decls); len(got.HardErrors) != 0 {
		test.Errorf("expected pass; got %+v", got)
	}
}

func TestValidateProperties_ListOfEnumRejectsOutOfValues(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"stages": []any{"draft", "shipping"}}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{
			Name: "stages", Type: "list-of", ItemType: "enum", Values: []string{"draft", "review", "shipped"},
		}}},
	}

	got := node.ValidateProperties(parsed, decls)

	if len(got.HardErrors) != 1 || got.HardErrors[0].Kind != node.ErrEnumViolation {
		test.Errorf("expected one ErrEnumViolation; got %+v", got)
	}
}

func TestValidateProperties_ListOfRejectsNonList(test *testing.T) {
	parsed := &node.Node{Type: "ticket", Properties: map[string]any{"labels": "auth"}}
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "labels", Type: "list-of", ItemType: "string"}}},
	}

	if got := node.ValidateProperties(parsed, decls); len(got.HardErrors) != 1 || got.HardErrors[0].Kind != node.ErrTypeMismatch {
		test.Errorf("expected ErrTypeMismatch; got %+v", got)
	}
}

func TestWhichRequiredWereUnset_NotRequired(test *testing.T) {
	before := &node.Node{Type: "ticket", Properties: map[string]any{"x": "v"}}
	after := &node.Node{Type: "ticket", Properties: map[string]any{}}

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "x", Type: "string"}}},
	}

	if got := node.WhichRequiredWereUnset(before, after, decls); len(got) != 0 {
		test.Errorf("expected empty; got %v", got)
	}
}

func TestWhichRequiredWereUnset_RequiredUnsetReturnsName(test *testing.T) {
	before := &node.Node{Type: "ticket", Properties: map[string]any{"summary": "v"}}
	after := &node.Node{Type: "ticket", Properties: map[string]any{}}

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "summary", Type: "string", Required: true}}},
	}

	got := node.WhichRequiredWereUnset(before, after, decls)

	if len(got) != 1 || got[0] != "summary" {
		test.Errorf("expected [summary]; got %v", got)
	}
}

func TestWhichRequiredWereUnset_RequiredStillPresentReturnsEmpty(test *testing.T) {
	before := &node.Node{Type: "ticket", Properties: map[string]any{"summary": "v"}}
	after := &node.Node{Type: "ticket", Properties: map[string]any{"summary": "v2"}}

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "summary", Type: "string", Required: true}}},
	}

	if got := node.WhichRequiredWereUnset(before, after, decls); len(got) != 0 {
		test.Errorf("expected empty; got %v", got)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/node/... -run "TestValidateProperties_ListOf|TestWhichRequiredWereUnset"
```

Expected: FAIL — list-of branch absent; `WhichRequiredWereUnset` undefined.

- [ ] **Step 3: Add the list-of branch to the per-property loop**

Behavior:

- Reject if `value` is not a Go `[]any` → one `ErrTypeMismatch` for the property.
- Otherwise, for each element in the list, run the same per-type validation (recursive single level) using `decl.ItemType` as the type and (when `ItemType = "enum"`) `decl.Values` for the enum list. Each violating element produces one `ErrTypeMismatch` (or `ErrEnumViolation` for list-of-enum).

The render of `decl` for `list-of` is `"list-of(<inner-render>)"`, e.g. `"list-of(string)"` or `"list-of(enum(draft|review|shipped))"`.

- [ ] **Step 4: Add `WhichRequiredWereUnset` to `internal/node/types.go`**

Signature:

```go
func WhichRequiredWereUnset(before, after *node.Node, decls map[string]manifest.NodeType) []string
```

Behavior:

- If `decls[after.Type]` is absent, return an empty slice.
- For each `PropertyDecl` with `Required = true`:
  - If the property is present on `before.Properties` AND absent from `after.Properties` → append `decl.Name` to the result.
- Result order matches declaration order.

- [ ] **Step 5: Run, verify pass**

```bash
go test ./internal/node/... -v
```

Expected: every TestValidateProperties_* and TestWhichRequiredWereUnset_* PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/node/types.go internal/node/types_test.go
git commit -m "feat(node): list-of validation + WhichRequiredWereUnset helper"
```

---

## Task 7: Index — `property_drift` table + `PropertyDriftRepo`

**Files:** Modify `internal/index/index.go`, `internal/index/index_test.go`. Create `internal/index/property_drift_repo.go`, `internal/index/property_drift_repo_test.go`.

This task mirrors `workflow_drift_repo.go` exactly. Same shape, different field names.

- [ ] **Step 1: Append failing schema test to `internal/index/index_test.go`**

```go
func TestOpen_CreatesPropertyDriftTable(test *testing.T) {
	dbPath := filepath.Join(test.TempDir(), "index.db")

	store, openErr := index.Open(dbPath)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer store.Close()

	tables, listErr := store.ListTables()

	if listErr != nil {
		test.Fatalf("ListTables: %v", listErr)
	}

	if !contains(tables, "property_drift") {
		test.Errorf("missing table %q in %v", "property_drift", tables)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/index/... -run TestOpen_CreatesPropertyDriftTable
```

Expected: FAIL — schema lacks the `property_drift` table.

- [ ] **Step 3: Append the table to the `schema` const in `internal/index/index.go`**

Insert just before the closing backtick:

```sql
CREATE TABLE IF NOT EXISTS property_drift (
    node_id      TEXT NOT NULL,
    node_type    TEXT NOT NULL,
    kind         TEXT NOT NULL,
    property     TEXT NOT NULL,
    details      TEXT,
    observed_at  INTEGER NOT NULL,
    PRIMARY KEY (node_id, kind, property)
);

CREATE INDEX IF NOT EXISTS property_drift_node_idx ON property_drift(node_id);
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/index/... -run TestOpen_CreatesPropertyDriftTable
```

Expected: PASS.

- [ ] **Step 5: Write the failing repo tests**

`internal/index/property_drift_repo_test.go`:

```go
package index_test

import (
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func TestPropertyDriftRepo_AppendThenList(test *testing.T) {
	store, closer := newTestIndexForPropertyDrift(test)
	defer closer()

	repo := index.NewPropertyDriftRepo(store)

	row := index.PropertyDriftRow{
		NodeID:     "tickets/foo",
		NodeType:   "ticket",
		Kind:       "undeclared-property",
		Property:   "assignee",
		Details:    "not declared on type \"ticket\"",
		ObservedAt: 1700_000_000,
	}

	if appendErr := repo.Append(row); appendErr != nil {
		test.Fatalf("Append: %v", appendErr)
	}

	rows, listErr := repo.ListAll()

	if listErr != nil {
		test.Fatalf("ListAll: %v", listErr)
	}

	if len(rows) != 1 || rows[0].NodeID != "tickets/foo" || rows[0].Property != "assignee" {
		test.Errorf("ListAll = %+v", rows)
	}
}

func TestPropertyDriftRepo_AppendIdempotentOnPK(test *testing.T) {
	store, closer := newTestIndexForPropertyDrift(test)
	defer closer()

	repo := index.NewPropertyDriftRepo(store)

	row := index.PropertyDriftRow{
		NodeID:     "tickets/foo",
		NodeType:   "ticket",
		Kind:       "type-mismatch",
		Property:   "priority",
		Details:    "value \"high\"",
		ObservedAt: 100,
	}

	for _, observedAt := range []int64{100, 200, 300} {
		row.ObservedAt = observedAt

		if appendErr := repo.Append(row); appendErr != nil {
			test.Fatalf("Append: %v", appendErr)
		}
	}

	rows, _ := repo.ListAll()

	if len(rows) != 1 {
		test.Errorf("ListAll: want 1 row (PK collapsed), got %d", len(rows))
	}

	if rows[0].ObservedAt != 300 {
		test.Errorf("ObservedAt = %d, want most recent (300)", rows[0].ObservedAt)
	}
}

func TestPropertyDriftRepo_ClearForNode(test *testing.T) {
	store, closer := newTestIndexForPropertyDrift(test)
	defer closer()

	repo := index.NewPropertyDriftRepo(store)

	rows := []index.PropertyDriftRow{
		{NodeID: "tickets/foo", NodeType: "ticket", Kind: "undeclared-property", Property: "x", ObservedAt: 1},
		{NodeID: "tickets/foo", NodeType: "ticket", Kind: "type-mismatch",       Property: "y", ObservedAt: 2},
		{NodeID: "tickets/bar", NodeType: "ticket", Kind: "undeclared-property", Property: "z", ObservedAt: 3},
	}

	for _, row := range rows {
		if appendErr := repo.Append(row); appendErr != nil {
			test.Fatalf("Append: %v", appendErr)
		}
	}

	if clearErr := repo.ClearForNode("tickets/foo"); clearErr != nil {
		test.Fatalf("ClearForNode: %v", clearErr)
	}

	remaining, _ := repo.ListAll()

	if len(remaining) != 1 || remaining[0].NodeID != "tickets/bar" {
		test.Errorf("after Clear: remaining = %+v, want only tickets/bar", remaining)
	}
}

func TestPropertyDriftRepo_CountAll(test *testing.T) {
	store, closer := newTestIndexForPropertyDrift(test)
	defer closer()

	repo := index.NewPropertyDriftRepo(store)

	rows := []index.PropertyDriftRow{
		{NodeID: "a", NodeType: "ticket", Kind: "type-mismatch", Property: "x", ObservedAt: 1},
		{NodeID: "b", NodeType: "ticket", Kind: "type-mismatch", Property: "y", ObservedAt: 2},
	}

	for _, row := range rows {
		_ = repo.Append(row)
	}

	count, countErr := repo.CountAll()

	if countErr != nil {
		test.Fatalf("CountAll: %v", countErr)
	}

	if count != 2 {
		test.Errorf("CountAll = %d, want 2", count)
	}
}

func newTestIndexForPropertyDrift(test *testing.T) (*index.Index, func()) {
	test.Helper()

	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	return store, func() { store.Close() }
}
```

- [ ] **Step 6: Run, verify fail**

```bash
go test ./internal/index/... -run TestPropertyDriftRepo
```

Expected: FAIL — `index.PropertyDriftRow`, `index.NewPropertyDriftRepo` undefined.

- [ ] **Step 7: Create `internal/index/property_drift_repo.go`**

Type sketch:

```go
type PropertyDriftRow struct {
    NodeID     string
    NodeType   string
    Kind       string  // "undeclared-property" | "type-mismatch" | "required-missing" | "enum-violation"
    Property   string
    Details    string
    ObservedAt int64
}

type PropertyDriftRepo struct{ /* db handle */ }

func NewPropertyDriftRepo(idx *Index) *PropertyDriftRepo
func (repo *PropertyDriftRepo) Append(row PropertyDriftRow) error
func (repo *PropertyDriftRepo) ListAll() ([]PropertyDriftRow, error)
func (repo *PropertyDriftRepo) ClearForNode(nodeID string) error
func (repo *PropertyDriftRepo) CountAll() (int, error)
```

Behavior is identical to `WorkflowDriftRepo`:

- `Append`: SQL `INSERT … ON CONFLICT (node_id, kind, property) DO UPDATE SET node_type=excluded.node_type, details=excluded.details, observed_at=excluded.observed_at`. Returns the wrapped error on failure.
- `ListAll`: SQL `SELECT … ORDER BY node_id, kind, property`. Returns the slice in deterministic order for stable doctor rendering.
- `ClearForNode`: SQL `DELETE FROM property_drift WHERE node_id = ?`.
- `CountAll`: SQL `SELECT COUNT(*) FROM property_drift`.

The implementer's reference is `internal/index/workflow_drift_repo.go` — same shape, different table.

- [ ] **Step 8: Run, verify pass**

```bash
go test ./internal/index/... -v
```

Expected: every existing test plus `TestPropertyDriftRepo_*` PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/index/index.go internal/index/index_test.go internal/index/property_drift_repo.go internal/index/property_drift_repo_test.go
git commit -m "feat(index): property_drift table + PropertyDriftRepo"
```

---

## Task 8: NodeService — constructor extension + struct fields

**Files:** Modify `internal/node/service.go`.

The Plan 7 production constructor `NewServiceWithBehaviors` grows two parameters; the struct grows two fields. Pre-Plan-7.b constructors stay unchanged. No test changes in this task — Tasks 9 and 10 exercise the new fields.

- [ ] **Step 1: Add fields to `Service` struct**

The two new fields (named consistently with Plan 7's naming style):

```go
nodeTypes      map[string]manifest.NodeType
propertyDrift  *index.PropertyDriftRepo
```

Inserted alongside Plan 7's `behaviors`, `drift`, `warnings`. No new imports needed beyond what's already in `service.go`.

- [ ] **Step 2: Update `NewServiceWithBehaviors` signature and body**

The new signature inserts two parameters between `embedQueue` and `behaviors`:

```go
func NewServiceWithBehaviors(
    workspaceRoot string,
    repo *index.NodeRepo,
    edges *index.EdgeRepo,
    edgeTypes manifest.EdgeTypes,
    embedQueue *index.EmbedQueueRepo,
    nodeTypes map[string]manifest.NodeType,        // + 7.b
    propertyDrift *index.PropertyDriftRepo,        // + 7.b
    behaviors Behaviors,
    drift *index.WorkflowDriftRepo,
    warnings io.Writer,
) *Service
```

Body assigns the new fields onto the returned `*Service`. No defaulting beyond what already exists (the struct's zero value for `nodeTypes` is `nil`, which the validator treats as "no declarations"; same for `propertyDrift`).

- [ ] **Step 3: Update every existing in-tree caller of `NewServiceWithBehaviors`**

Three callers in the tree — `cmd/tusk/cmd_node_create.go`, `cmd/tusk/cmd_node_modify.go`, `internal/mcp/runtime.go` — currently call with the Plan 7 argument list. Each grows two arguments.

For now, pass `nil` for both `nodeTypes` and `propertyDrift` at each call site. Tasks 14–17 wire the real values through. This minimizes the diff in Task 8 and keeps the build green between commits.

- [ ] **Step 4: Verify it compiles + existing tests still pass**

```bash
go build ./...
make test
```

Expected: build clean; every existing test still passes (the new fields are optional from the validator's perspective).

- [ ] **Step 5: Commit**

```bash
git add internal/node/service.go cmd/tusk/cmd_node_create.go cmd/tusk/cmd_node_modify.go internal/mcp/runtime.go
git commit -m "feat(node): NewServiceWithBehaviors grows nodeTypes + propertyDrift parameters"
```

---

## Task 9: NodeService.Create — dispatch property validation

**Files:** Modify `internal/node/service.go`, `internal/node/service_test.go`.

`Service.Create` runs the validator after edge validation and before any hook firing. Hard errors abort before file write. Drift fires after the index commits — Plan 7's recovery-handling tail.

- [ ] **Step 1: Append failing tests to `internal/node/service_test.go`**

```go
func TestCreate_PropertyRequiredMissingRejects(test *testing.T) {
	root := test.TempDir()
	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))

	defer store.Close()

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "summary", Type: "string", Required: true}}},
	}

	service := node.NewServiceWithBehaviors(
		root,
		index.NewNodeRepo(store),
		index.NewEdgeRepo(store),
		manifest.EdgeTypes{},
		index.NewEmbedQueueRepo(store),
		decls,
		index.NewPropertyDriftRepo(store),
		nil, nil, io.Discard,
	)

	_, createErr := service.Create(node.CreateInput{
		RelPath: "tickets/foo.md",
		Type:    "ticket",
	})

	if createErr == nil || !strings.Contains(createErr.Error(), "summary") {
		test.Errorf("Create: expected required-missing error, got %v", createErr)
	}

	// File must NOT exist (hard error aborts before write).
	if _, statErr := os.Stat(filepath.Join(root, "tickets/foo.md")); !os.IsNotExist(statErr) {
		test.Errorf("file present after rejection; statErr = %v", statErr)
	}
}

func TestCreate_PropertyTypeMismatchRejects(test *testing.T) {
	root := test.TempDir()
	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))

	defer store.Close()

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "priority", Type: "int"}}},
	}

	service := node.NewServiceWithBehaviors(
		root,
		index.NewNodeRepo(store),
		index.NewEdgeRepo(store),
		manifest.EdgeTypes{},
		index.NewEmbedQueueRepo(store),
		decls,
		index.NewPropertyDriftRepo(store),
		nil, nil, io.Discard,
	)

	_, createErr := service.Create(node.CreateInput{
		RelPath:    "tickets/foo.md",
		Type:       "ticket",
		Properties: map[string]any{"priority": "high"},
	})

	if createErr == nil || !strings.Contains(createErr.Error(), "priority") {
		test.Errorf("Create: expected type-mismatch error, got %v", createErr)
	}
}

func TestCreate_PropertyUndeclaredWritesAndDrifts(test *testing.T) {
	root := test.TempDir()
	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))

	defer store.Close()

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "summary", Type: "string"}}},
	}

	driftRepo := index.NewPropertyDriftRepo(store)

	var warnings bytes.Buffer

	service := node.NewServiceWithBehaviors(
		root,
		index.NewNodeRepo(store),
		index.NewEdgeRepo(store),
		manifest.EdgeTypes{},
		index.NewEmbedQueueRepo(store),
		decls,
		driftRepo,
		nil, nil, &warnings,
	)

	if _, createErr := service.Create(node.CreateInput{
		RelPath:    "tickets/foo.md",
		Type:       "ticket",
		Properties: map[string]any{"summary": "hi", "assignee": "bob"},
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	if !strings.Contains(warnings.String(), "assignee") {
		test.Errorf("warnings = %q, want mention of assignee", warnings.String())
	}

	rows, _ := driftRepo.ListAll()

	if len(rows) != 1 || rows[0].Property != "assignee" || rows[0].Kind != "undeclared-property" {
		test.Errorf("drift rows = %+v", rows)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/node/... -run "TestCreate_Property"
```

Expected: FAIL — Create doesn't yet dispatch property validation.

- [ ] **Step 3: Add `PropertyValidationError` to `internal/node/types.go`**

Type sketch:

```go
// PropertyValidationError wraps the validator's HardErrors slice so callers
// (CLI, MCP) can either pass the human message through (CLI) or type-assert
// to access the structured slice (MCP).
type PropertyValidationError struct {
    Op       string  // "create" | "modify"
    NodeID   string
    NodeType string
    Errors   []PropertyError
}

func (err *PropertyValidationError) Error() string  // returns the joined human message
```

`Error()` returns the joined message format from spec §4.4:

```
node-types: rejected create: ticket "tickets/foo" has 1 error:
  - property "summary" is required (declared in [node-types.ticket])
```

The header line names the op, node-type, and node-id; each error is rendered as `- property "<name>" <reason>` on an indented line.

- [ ] **Step 4: Wire dispatch into `Service.Create`**

Insertion points (per spec §4.2's order):

- After the existing edge-validation + cycle-check block, BEFORE Plan 7's hook validate-phase: call `node.ValidateProperties(parsed, service.nodeTypes)`. If `len(result.HardErrors) > 0`, return a `*node.PropertyValidationError{Op: "create", NodeID: parsed.ID, NodeType: parsed.Type, Errors: result.HardErrors}` to the caller. No file write.
- After the existing index commits (the `embedQueue.Enqueue` block) and Plan 7's after-phase hook fires, but before returning the parsed node: for each `result.Drift` entry, write a line to `service.warnings` of the form `warning: node-types: property "<P>" is not declared on type "<T>"; surfaces as a property-drift in tusk doctor`, AND if `service.propertyDrift != nil`, append a `PropertyDriftRow{Kind: "undeclared-property", NodeID: parsed.ID, NodeType: parsed.Type, Property: drift.Property, Details: drift.Reason, ObservedAt: time.Now().UnixNano()}`. On clean pass (`len(result.HardErrors) == 0 && len(result.Drift) == 0`), call `service.propertyDrift.ClearForNode(parsed.ID)` if non-nil.

The joined hard-error message format (used by both Create and Modify): a header line, then one indented bullet per error. Example for the test cases:

```
node-types: rejected create: ticket "tickets/foo" has 1 error:
  - property "summary" is required (declared in [node-types.ticket])
```

The implementer formats the bullet text from each `PropertyError.Reason` field, prefixed with `- property "<name>" `.

- [ ] **Step 5: Run, verify pass**

```bash
go test ./internal/node/... -v
```

Expected: every existing TestCreate_* and the three new TestCreate_Property* PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/node/types.go internal/node/service.go internal/node/service_test.go
git commit -m "feat(node): Create dispatches ValidateProperties; PropertyValidationError typed wrapper"
```

---

## Task 10: NodeService.Modify — dispatch property validation + WhichRequiredWereUnset

**Files:** Modify `internal/node/service.go`, `internal/node/service_test.go`.

Modify dispatches the validator + the modify-only `WhichRequiredWereUnset` helper. Hard errors abort before file write; drift surfaces after the commit.

- [ ] **Step 1: Append failing tests**

```go
func TestModify_PropertyTypeMismatchRejects(test *testing.T) {
	root := test.TempDir()
	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))

	defer store.Close()

	// Seed without validation.
	seed := node.NewServiceWithBehaviors(
		root, index.NewNodeRepo(store), index.NewEdgeRepo(store),
		manifest.EdgeTypes{}, index.NewEmbedQueueRepo(store),
		nil, nil, nil, nil, io.Discard,
	)

	if _, createErr := seed.Create(node.CreateInput{
		RelPath: "tickets/foo.md",
		Type:    "ticket",
	}); createErr != nil {
		test.Fatalf("seed Create: %v", createErr)
	}

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "priority", Type: "int"}}},
	}

	service := node.NewServiceWithBehaviors(
		root, index.NewNodeRepo(store), index.NewEdgeRepo(store),
		manifest.EdgeTypes{}, index.NewEmbedQueueRepo(store),
		decls, index.NewPropertyDriftRepo(store),
		nil, nil, io.Discard,
	)

	_, modifyErr := service.Modify(node.ModifyInput{
		ID:       "tickets/foo",
		SetProps: map[string]any{"priority": "high"},
	})

	if modifyErr == nil || !strings.Contains(modifyErr.Error(), "priority") {
		test.Errorf("Modify: expected type-mismatch error, got %v", modifyErr)
	}
}

func TestModify_UnsetRequiredRejects(test *testing.T) {
	root := test.TempDir()
	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))

	defer store.Close()

	seed := node.NewServiceWithBehaviors(
		root, index.NewNodeRepo(store), index.NewEdgeRepo(store),
		manifest.EdgeTypes{}, index.NewEmbedQueueRepo(store),
		nil, nil, nil, nil, io.Discard,
	)

	if _, createErr := seed.Create(node.CreateInput{
		RelPath: "tickets/foo.md",
		Type:    "ticket",
		Title:   "hello",
	}); createErr != nil {
		test.Fatalf("seed Create: %v", createErr)
	}

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "summary", Type: "string", Required: true}}},
	}

	service := node.NewServiceWithBehaviors(
		root, index.NewNodeRepo(store), index.NewEdgeRepo(store),
		manifest.EdgeTypes{}, index.NewEmbedQueueRepo(store),
		decls, index.NewPropertyDriftRepo(store),
		nil, nil, io.Discard,
	)

	_, modifyErr := service.Modify(node.ModifyInput{
		ID:        "tickets/foo",
		UnsetKeys: []string{"summary"},
	})

	if modifyErr == nil || !strings.Contains(modifyErr.Error(), "cannot unset required") {
		test.Errorf("Modify: expected required-unset error, got %v", modifyErr)
	}
}

func TestModify_UndeclaredPropertyDriftsAndClearsOnCleanPass(test *testing.T) {
	root := test.TempDir()
	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))

	defer store.Close()

	seed := node.NewServiceWithBehaviors(
		root, index.NewNodeRepo(store), index.NewEdgeRepo(store),
		manifest.EdgeTypes{}, index.NewEmbedQueueRepo(store),
		nil, nil, nil, nil, io.Discard,
	)

	if _, createErr := seed.Create(node.CreateInput{
		RelPath: "tickets/foo.md",
		Type:    "ticket",
		Title:   "hello",
	}); createErr != nil {
		test.Fatalf("seed Create: %v", createErr)
	}

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "summary", Type: "string", Required: true}}},
	}

	driftRepo := index.NewPropertyDriftRepo(store)

	var warnings bytes.Buffer

	service := node.NewServiceWithBehaviors(
		root, index.NewNodeRepo(store), index.NewEdgeRepo(store),
		manifest.EdgeTypes{}, index.NewEmbedQueueRepo(store),
		decls, driftRepo,
		nil, nil, &warnings,
	)

	// First Modify: add an undeclared property → drift.
	if _, modifyErr := service.Modify(node.ModifyInput{
		ID:       "tickets/foo",
		SetProps: map[string]any{"assignee": "bob"},
	}); modifyErr != nil {
		test.Fatalf("Modify (drift): %v", modifyErr)
	}

	rows, _ := driftRepo.ListAll()

	if len(rows) != 1 {
		test.Fatalf("drift after first Modify = %+v, want 1 row", rows)
	}

	if !strings.Contains(warnings.String(), "assignee") {
		test.Errorf("warnings = %q", warnings.String())
	}

	// Second Modify: remove the undeclared property → clean pass clears drift.
	if _, modifyErr := service.Modify(node.ModifyInput{
		ID:        "tickets/foo",
		UnsetKeys: []string{"assignee"},
	}); modifyErr != nil {
		test.Fatalf("Modify (clean): %v", modifyErr)
	}

	rows, _ = driftRepo.ListAll()

	if len(rows) != 0 {
		test.Errorf("drift after clean Modify = %+v, want empty", rows)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/node/... -run "TestModify_(PropertyTypeMismatch|UnsetRequired|UndeclaredProperty)"
```

Expected: FAIL — Modify doesn't yet dispatch property validation.

- [ ] **Step 3: Wire dispatch into `Service.Modify`**

Insertion points (per spec §4.3):

- After the existing parse + apply Set/Unset/Body + render + reparse + edge-validate + cycle-check block, BEFORE Plan 7's recovery-aware Validate fire: call `result := node.ValidateProperties(reparsed, service.nodeTypes)`. Then call `unsetRequired := node.WhichRequiredWereUnset(beforeNode, reparsed, service.nodeTypes)`. For each name in `unsetRequired`, append a `PropertyError{Kind: ErrCannotUnsetRequired, Property: name, Reason: "cannot unset required property \"" + name + "\" on type \"" + reparsed.Type + "\""}` to `result.HardErrors`. If `len(result.HardErrors) > 0`, return `*node.PropertyValidationError{Op: "modify", NodeID: reparsed.ID, NodeType: reparsed.Type, Errors: result.HardErrors}`.
- After the existing index commits and Plan 7's after-phase hook fires + recovered-event drift writes, mirror Task 9's drift handling: per drift entry, write a stderr warning + append a `PropertyDriftRow`. On clean pass (no hard errors AND no drift), call `service.propertyDrift.ClearForNode(reparsed.ID)`.

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/node/... -v
```

Expected: every existing TestModify_* and the three new TestModify_Property* PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/node/service.go internal/node/service_test.go
git commit -m "feat(node): Modify dispatches ValidateProperties + WhichRequiredWereUnset"
```

---

## Task 11: Reindex — warn-mode property validation + summary count

**Files:** Modify `internal/reindex/reindex.go`, `internal/reindex/reindex_test.go`.

`reindex.Run` runs property validation in warn mode alongside Plan 7's workflow validation. Hard errors and drift entries both become drift rows; reindex never aborts indexing. The summary line gains a property-violation count.

- [ ] **Step 1: Append failing tests**

```go
func TestRun_OffSchemaPropertyProducesDriftRow(test *testing.T) {
	root := test.TempDir()
	dbPath := filepath.Join(root, "index.db")

	store, openErr := index.Open(dbPath)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer store.Close()

	if writeErr := os.WriteFile(filepath.Join(root, "ticket.md"), []byte(`---
type: ticket
priority: high
---
body
`), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "priority", Type: "int"}}},
	}

	driftRepo := index.NewPropertyDriftRepo(store)

	report, runErr := reindex.Run(reindex.Config{
		Root:          root,
		Repo:          index.NewNodeRepo(store),
		NodeTypes:     decls,
		PropertyDrift: driftRepo,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if report.PropertyViolations != 1 {
		test.Errorf("PropertyViolations = %d, want 1", report.PropertyViolations)
	}

	rows, _ := driftRepo.ListAll()

	if len(rows) != 1 || rows[0].Kind != "type-mismatch" {
		test.Errorf("drift rows = %+v", rows)
	}

	// Indexing still upserted the row.
	if _, getErr := index.NewNodeRepo(store).Get("ticket"); getErr != nil {
		test.Errorf("Get: %v (reindex should still upsert despite drift)", getErr)
	}
}

func TestRun_CleanPassClearsPropertyDrift(test *testing.T) {
	root := test.TempDir()
	dbPath := filepath.Join(root, "index.db")

	store, _ := index.Open(dbPath)

	defer store.Close()

	driftRepo := index.NewPropertyDriftRepo(store)

	if appendErr := driftRepo.Append(index.PropertyDriftRow{
		NodeID: "ticket", NodeType: "ticket", Kind: "type-mismatch", Property: "priority", ObservedAt: 1,
	}); appendErr != nil {
		test.Fatalf("seed Append: %v", appendErr)
	}

	if writeErr := os.WriteFile(filepath.Join(root, "ticket.md"), []byte(`---
type: ticket
priority: 3
---
`), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "priority", Type: "int"}}},
	}

	if _, runErr := reindex.Run(reindex.Config{
		Root:          root,
		Repo:          index.NewNodeRepo(store),
		NodeTypes:     decls,
		PropertyDrift: driftRepo,
	}); runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	rows, _ := driftRepo.ListAll()

	if len(rows) != 0 {
		test.Errorf("drift after clean reindex = %+v, want empty", rows)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/reindex/... -run "TestRun_(OffSchemaPropertyProducesDriftRow|CleanPassClearsPropertyDrift)"
```

Expected: FAIL — `Config.NodeTypes`, `Config.PropertyDrift`, `Report.PropertyViolations` undefined.

- [ ] **Step 3: Modify `internal/reindex/reindex.go`**

Add fields to `Config` and `Report`:

```go
type Config struct {
    // ... existing fields including Plan 7's Behaviors and DriftLog ...
    NodeTypes     map[string]manifest.NodeType
    PropertyDrift *index.PropertyDriftRepo
}

type Report struct {
    // ... existing fields including Plan 7's WorkflowViolations ...
    PropertyViolations int
}
```

Per-file walk extension (insert after Plan 7's workflow-validation block, before tracking `seenPaths` or whatever follows):

- If `config.NodeTypes` is nil or `parsed.Type` is not in the map, skip (untyped pass-through).
- Otherwise, call `node.ValidateProperties(parsed, config.NodeTypes)`.
- Per `HardError`: increment `report.PropertyViolations`; append a `PropertyDriftRow` with the matching `Kind`. The `Kind` mapping is:
  - `ErrTypeMismatch` → `"type-mismatch"`
  - `ErrRequiredMissing` → `"required-missing"`
  - `ErrEnumViolation` → `"enum-violation"`
  - `ErrCannotUnsetRequired` cannot fire from reindex (no `before`); guard with a defensive skip if needed.
- Per `Drift`: increment `report.PropertyViolations`; append row with `Kind: "undeclared-property"`.
- On clean pass (both empty), call `config.PropertyDrift.ClearForNode(parsed.ID)` if `PropertyDrift` is non-nil.

The `Details` field for hard-error drift rows carries the `Reason` from the `PropertyError` (e.g., `"value \"high\" is not an integer"`). For drift entries, `Details` carries the `Reason` from the `PropertyDrift` (e.g., `"not declared on type \"ticket\""`).

Add `time` import for `time.Now().UnixNano()` if not already imported in this file.

Update the summary line emission (the existing reindex CLI prints — but that's in cmd_reindex.go; reindex.Run doesn't print directly. Leave the print to Task 16). For now, just expose `PropertyViolations` on the Report.

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/reindex/... -v
```

Expected: every existing test plus the two new ones PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/reindex/
git commit -m "feat(reindex): warn-mode property validation + drift writes + violations count"
```

---

## Task 12: Doctor — new Issue kinds + read property drift

**Files:** Modify `internal/doctor/doctor.go`, `internal/doctor/doctor_test.go`.

Doctor adds four new Issue kind constants and reads `property_drift` rows when `Config.PropertyDrift` is supplied. Each row materializes one `Issue` with a kind-specific rendered message.

- [ ] **Step 1: Append failing test**

```go
func TestRun_SurfacesPropertyDrift(test *testing.T) {
	store, closer := newTempIndexForDoctor(test)
	defer closer()

	driftRepo := index.NewPropertyDriftRepo(store)

	rows := []index.PropertyDriftRow{
		{NodeID: "tickets/foo", NodeType: "ticket", Kind: "undeclared-property", Property: "assignee", Details: "not declared on type \"ticket\"", ObservedAt: 100},
		{NodeID: "tickets/bar", NodeType: "ticket", Kind: "type-mismatch",       Property: "priority", Details: "value \"high\" is not an integer", ObservedAt: 200},
		{NodeID: "tickets/baz", NodeType: "ticket", Kind: "required-missing",    Property: "summary",    Details: "required",                            ObservedAt: 300},
		{NodeID: "tickets/qux", NodeType: "ticket", Kind: "enum-violation",      Property: "stage",    Details: "value \"shipping\" not in [pending, active, completed]", ObservedAt: 400},
	}

	for _, row := range rows {
		if appendErr := driftRepo.Append(row); appendErr != nil {
			test.Fatalf("Append: %v", appendErr)
		}
	}

	report, runErr := doctor.Run(doctor.Config{
		Nodes:         index.NewNodeRepo(store),
		PropertyDrift: driftRepo,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	wantKinds := []string{
		doctor.IssueUndeclaredProperty,
		doctor.IssueTypeMismatch,
		doctor.IssueRequiredMissing,
		doctor.IssueEnumViolation,
	}

	for _, want := range wantKinds {
		var found bool

		for _, issue := range report.Issues {
			if issue.Kind == want {
				found = true

				if !strings.Contains(issue.Message, "node-types") {
					test.Errorf("Issue %s: message %q missing 'node-types' prefix", want, issue.Message)
				}
			}
		}

		if !found {
			test.Errorf("kind %q not surfaced", want)
		}
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/doctor/... -run TestRun_SurfacesPropertyDrift
```

Expected: FAIL — new Issue constants and `Config.PropertyDrift` undefined.

- [ ] **Step 3: Modify `internal/doctor/doctor.go`**

Add four new constants:

```go
const (
    // existing IssueDanglingEdge, IssueEmbedRetry, IssueWorkflowViolation ...

    IssueUndeclaredProperty = "undeclared-property"
    IssueTypeMismatch       = "type-mismatch"
    IssueRequiredMissing    = "required-missing"
    IssueEnumViolation      = "enum-violation"
)
```

Add a field to `Config`:

```go
PropertyDrift *index.PropertyDriftRepo  // optional
```

In `Run`, after the existing `WorkflowDrift` read block:

- If `config.PropertyDrift` is non-nil, read `ListAll()`. For each row, append `Issue{Kind: row.Kind, NodeID: row.NodeID, Message: <rendered>}`.
- The rendered message is kind-specific (per spec §7.3):
  - `undeclared-property`: `"node-types: property \"<P>\" not declared on type \"<T>\""`
  - `type-mismatch`: `"node-types: property \"<P>\" — <details>"` (where `<details>` is `row.Details`)
  - `required-missing`: `"node-types: required property \"<P>\" missing on type \"<T>\""`
  - `enum-violation`: `"node-types: property \"<P>\" — <details>"`

Each rendering uses `row.NodeType` for `<T>`, `row.Property` for `<P>`. The `row.Details` field is the validator's `Reason` text, suitable for inline interpolation.

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/doctor/... -v
```

Expected: every existing test plus the new one PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/doctor/
git commit -m "feat(doctor): four new Issue kinds sourced from property_drift"
```

---

## Task 13: Behavior — `DeclaredKey` + collision detector extension

**Files:** Modify `internal/behavior/engine.go`, `internal/behavior/registry.go`, `internal/behavior/registry_test.go`.

`behavior.NewEngine` grows a `[]DeclaredKey` parameter; the existing collision detector unifies behavior reservations with declared keys. `Registry.BuildEngineWithDeclaredKeys` is the production path; `BuildEngine` (instances-only) stays for tests.

- [ ] **Step 1: Append failing tests to `internal/behavior/registry_test.go`**

```go
func TestBuildEngineWithDeclaredKeys_CollidesWithBehaviorReservation(test *testing.T) {
	reg := behavior.NewRegistry()

	// Workflow-style pack reserving status on ticket.
	colliding := func(instanceName string) *fakePack {
		return &fakePack{
			name: instanceName,
			kind: "workflow",
			reserved: []behavior.ReservedKey{
				{NodeType: "ticket", Property: "status"},
			},
		}
	}

	if registerErr := reg.Register(&fakeKind{name: "workflow", produced: colliding}); registerErr != nil {
		test.Fatalf("Register: %v", registerErr)
	}

	loaded := &manifest.Manifest{
		Behaviors: map[string]map[string]toml.Primitive{
			"workflow": {"tickets": toml.Primitive{}},
		},
	}

	declared := []behavior.DeclaredKey{
		{NodeType: "ticket", Property: "status", Source: "node-types.ticket.properties[status]"},
	}

	_, buildErr := reg.BuildEngineWithDeclaredKeys(loaded, declared)

	if buildErr == nil {
		test.Fatalf("BuildEngineWithDeclaredKeys: expected collision error")
	}

	if !strings.Contains(buildErr.Error(), "ticket") || !strings.Contains(buildErr.Error(), "status") {
		test.Errorf("error missing ticket/status: %v", buildErr)
	}

	if !strings.Contains(buildErr.Error(), "node-types") {
		test.Errorf("error missing node-types source mention: %v", buildErr)
	}
}

func TestBuildEngineWithDeclaredKeys_SamePropertyDifferentTypePasses(test *testing.T) {
	reg := behavior.NewRegistry()

	colliding := func(instanceName string) *fakePack {
		return &fakePack{
			name: instanceName,
			kind: "workflow",
			reserved: []behavior.ReservedKey{
				{NodeType: "ticket", Property: "status"},
			},
		}
	}

	if registerErr := reg.Register(&fakeKind{name: "workflow", produced: colliding}); registerErr != nil {
		test.Fatalf("Register: %v", registerErr)
	}

	loaded := &manifest.Manifest{
		Behaviors: map[string]map[string]toml.Primitive{
			"workflow": {"tickets": toml.Primitive{}},
		},
	}

	declared := []behavior.DeclaredKey{
		// Same property name but on a different node-type — no collision.
		{NodeType: "decision", Property: "status", Source: "node-types.decision.properties[status]"},
	}

	if _, buildErr := reg.BuildEngineWithDeclaredKeys(loaded, declared); buildErr != nil {
		test.Errorf("BuildEngineWithDeclaredKeys: %v", buildErr)
	}
}

func TestBuildEngineWithDeclaredKeys_DifferentPropertySameTypePasses(test *testing.T) {
	reg := behavior.NewRegistry()

	colliding := func(instanceName string) *fakePack {
		return &fakePack{
			name: instanceName,
			kind: "workflow",
			reserved: []behavior.ReservedKey{
				{NodeType: "ticket", Property: "status"},
			},
		}
	}

	if registerErr := reg.Register(&fakeKind{name: "workflow", produced: colliding}); registerErr != nil {
		test.Fatalf("Register: %v", registerErr)
	}

	loaded := &manifest.Manifest{
		Behaviors: map[string]map[string]toml.Primitive{
			"workflow": {"tickets": toml.Primitive{}},
		},
	}

	declared := []behavior.DeclaredKey{
		// Same type but different property — no collision.
		{NodeType: "ticket", Property: "priority", Source: "node-types.ticket.properties[priority]"},
	}

	if _, buildErr := reg.BuildEngineWithDeclaredKeys(loaded, declared); buildErr != nil {
		test.Errorf("BuildEngineWithDeclaredKeys: %v", buildErr)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/behavior/... -run TestBuildEngineWithDeclaredKeys
```

Expected: FAIL — `behavior.DeclaredKey`, `Registry.BuildEngineWithDeclaredKeys` undefined.

- [ ] **Step 3: Add `DeclaredKey` type to `internal/behavior/pack.go` (or a new file in `internal/behavior`)**

```go
type DeclaredKey struct {
    NodeType string
    Property string
    Source   string  // e.g. "node-types.ticket.properties[priority]"
}
```

- [ ] **Step 4: Extend `internal/behavior/engine.go`'s `NewEngine`**

New signature:

```go
func NewEngine(instances []Instance, declaredKeys []DeclaredKey) (*Engine, error)
```

Body changes: extend `detectCollisions` to walk `declaredKeys` first, building the owner map keyed by `(NodeType, Property)` with the `Source` string as the owner identifier. Then walk `instances` as before; on a `ReservedKey` collision, the error names both colliding sources — for instance vs. declared, format as:

```
behavior: behaviors.<kind>.<instance> reserves property "<P>" on type "<T>"
but it is also declared in <declared.Source>
```

Existing instance-vs-instance collision behavior (Plan 7) is preserved unchanged.

- [ ] **Step 5: Update existing `NewEngine` callers**

Two callers exist today (Plan 7): the `Registry.BuildEngine` method and the existing `engine_test.go`. Pass an empty `[]DeclaredKey{}` slice from both. The `BuildEngine` signature stays the same (instances-only); it now calls `NewEngine(instances, nil)`.

Also update `engine_test.go` and the Bundle 4 fake-engine constructions in `internal/node/service_test.go` to pass the new parameter (nil slice).

- [ ] **Step 6: Add `Registry.BuildEngineWithDeclaredKeys` to `internal/behavior/registry.go`**

```go
func (registry *Registry) BuildEngineWithDeclaredKeys(loaded *manifest.Manifest, declaredKeys []DeclaredKey) (*Engine, error)
```

Behavior: identical to `BuildEngine` except it passes `declaredKeys` through to `NewEngine`.

- [ ] **Step 7: Run, verify pass**

```bash
go test ./internal/behavior/... -v
```

Expected: every existing test plus the three new ones PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/behavior/ internal/node/service_test.go
git commit -m "feat(behavior): DeclaredKey + collision detector extension; BuildEngineWithDeclaredKeys"
```

---

## Task 14: cmd/tusk — `behavior_registry` builds DeclaredKey slice

**Files:** Modify `cmd/tusk/behavior_registry.go`.

The CLI's `newBehaviorEngine` helper now constructs the `[]behavior.DeclaredKey` slice from `loaded.NodeTypes` and calls the new `Registry.BuildEngineWithDeclaredKeys` path.

- [ ] **Step 1: Modify `cmd/tusk/behavior_registry.go`**

Add a helper `declaredKeysFrom(loaded *manifest.Manifest) []behavior.DeclaredKey` that iterates `loaded.NodeTypes`; for each declared `(typeName, prop)`, appends `behavior.DeclaredKey{NodeType: typeName, Property: prop.Name, Source: fmt.Sprintf("node-types.%s.properties[%s]", typeName, prop.Name)}`.

Update `newBehaviorEngine` to call `declaredKeysFrom(loaded)` and pass the result to `registry.BuildEngineWithDeclaredKeys(loaded, declaredKeys)` instead of `BuildEngine(loaded)`.

- [ ] **Step 2: Verify it compiles + existing tests still pass**

```bash
go build ./cmd/tusk/...
make test
```

Expected: build clean; tests still pass (no behavior change for workspaces without `[node-types]`).

- [ ] **Step 3: Commit**

```bash
git add cmd/tusk/behavior_registry.go
git commit -m "feat(cli): behavior_registry threads DeclaredKey slice from manifest"
```

---

## Task 15: cmd/tusk — `node create` and `node modify` thread NodeTypes + PropertyDrift

**Files:** Modify `cmd/tusk/cmd_node_create.go`, `cmd/tusk/cmd_node_create_test.go`, `cmd/tusk/cmd_node_modify.go`, `cmd/tusk/cmd_node_modify_test.go`.

Both commands' Service constructors get the real `loaded.NodeTypes` and a fresh `index.NewPropertyDriftRepo(store)`. Tests cover required-missing, type-mismatch, undeclared-drift visible to doctor, and unset-required.

- [ ] **Step 1: Append failing tests to `cmd/tusk/cmd_node_create_test.go`**

```go
func TestNodeCreate_PropertyRequiredMissingRejected(test *testing.T) {
	root := newWorkspaceWithNodeTypes(test)

	_, stderr, exit := runCLISplit(test, root, "node", "create", "--type", "ticket", "--", "tickets/foo")

	if exit == 0 {
		test.Errorf("exit = 0, want non-zero")
	}

	if !strings.Contains(stderr.String(), "summary") {
		test.Errorf("stderr = %q, want mention of summary", stderr.String())
	}
}

func TestNodeCreate_PropertyTypeMismatchRejected(test *testing.T) {
	root := newWorkspaceWithNodeTypes(test)

	_, stderr, exit := runCLISplit(test, root, "node", "create",
		"--type", "ticket",
		"--prop", "title=hi",
		"--prop", "priority=high",
		"--", "tickets/foo")

	if exit == 0 {
		test.Errorf("exit = 0, want non-zero")
	}

	if !strings.Contains(stderr.String(), "priority") {
		test.Errorf("stderr = %q, want mention of priority", stderr.String())
	}
}

// newWorkspaceWithNodeTypes seeds a workspace with a tusk.toml that declares
// node-types.ticket. Returns the workspace root.
func newWorkspaceWithNodeTypes(test *testing.T) string {
	test.Helper()

	root := test.TempDir()

	manifestBody := `
[workspace]
name = "test"

[node-types.ticket]
properties = [
    { name = "summary",    type = "string", required = true },
    { name = "priority", type = "int" },
]
`

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(manifestBody), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	return root
}
```

- [ ] **Step 2: Append failing tests to `cmd/tusk/cmd_node_modify_test.go`**

```go
func TestNodeModify_PropertyUndeclaredDriftsAndDoctorSurfaces(test *testing.T) {
	root := newWorkspaceWithNodeTypes(test)

	mustCreateNode(test, root, "tickets/foo", "ticket", map[string]string{"summary": "hi"})

	stdout, stderr, exit := runCLISplit(test, root, "node", "modify", "tickets/foo", "--prop", "assignee=bob")

	if exit != 0 {
		test.Errorf("exit = %d, want 0", exit)
	}

	if !strings.Contains(stderr.String(), "assignee") {
		test.Errorf("stderr = %q, want drift warning", stderr.String())
	}

	if !strings.Contains(stdout.String(), "Modified tickets/foo") {
		test.Errorf("stdout = %q, want success line", stdout.String())
	}

	doctorStdout, _, doctorExit := runCLISplit(test, root, "doctor")

	if doctorExit != 0 {
		test.Errorf("doctor exit = %d, want 0", doctorExit)
	}

	if !strings.Contains(doctorStdout.String(), "undeclared-property") {
		test.Errorf("doctor stdout = %q", doctorStdout.String())
	}
}

func TestNodeModify_UnsetRequiredRejected(test *testing.T) {
	root := newWorkspaceWithNodeTypes(test)

	mustCreateNode(test, root, "tickets/foo", "ticket", map[string]string{"summary": "hi"})

	_, stderr, exit := runCLISplit(test, root, "node", "modify", "tickets/foo", "--unset", "summary")

	if exit == 0 {
		test.Errorf("exit = 0, want non-zero")
	}

	if !strings.Contains(stderr.String(), "cannot unset required") {
		test.Errorf("stderr = %q, want mention of cannot-unset-required", stderr.String())
	}
}
```

The helpers `runCLISplit` and `mustCreateNode` were introduced in Plan 7's Bundle 6. Reuse.

- [ ] **Step 3: Run, verify fail**

```bash
go test ./cmd/tusk/... -run "TestNodeCreate_Property|TestNodeModify_PropertyUndeclared|TestNodeModify_UnsetRequired"
```

Expected: FAIL — the cmd handlers still pass `nil` for `nodeTypes` and `propertyDrift` (Task 8's stub).

- [ ] **Step 4: Update `cmd/tusk/cmd_node_create.go`**

In the `RunE` handler, replace the `nil, nil` stubs in the `node.NewServiceWithBehaviors` call with `loaded.NodeTypes` and `index.NewPropertyDriftRepo(store)` respectively.

- [ ] **Step 5: Update `cmd/tusk/cmd_node_modify.go`**

Same replacement in its `RunE`.

- [ ] **Step 6: Run, verify pass**

```bash
go test ./cmd/tusk/... -run "TestNodeCreate|TestNodeModify" -v
```

Expected: every existing test plus the new ones PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd/tusk/cmd_node_create.go cmd/tusk/cmd_node_modify.go cmd/tusk/cmd_node_create_test.go cmd/tusk/cmd_node_modify_test.go
git commit -m "feat(cli): node create + modify thread NodeTypes + PropertyDrift"
```

---

## Task 16: cmd/tusk — `reindex` and `doctor` thread NodeTypes + PropertyDrift; summary line

**Files:** Modify `cmd/tusk/cmd_reindex.go`, `cmd/tusk/cmd_reindex_test.go`, `cmd/tusk/cmd_doctor.go`, `cmd/tusk/cmd_doctor_test.go`.

`tusk reindex` passes the new fields into `reindex.Config` and grows its summary line to include the property-violation count. `tusk doctor` passes `PropertyDrift: index.NewPropertyDriftRepo(store)` to `doctor.Run`.

- [ ] **Step 1: Append failing tests**

`cmd/tusk/cmd_reindex_test.go`:

```go
func TestReindex_OffSchemaPropertyReportedInSummary(test *testing.T) {
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

	stdout, _, exit := runCLISplit(test, root, "reindex")

	if exit != 0 {
		test.Errorf("exit = %d, want 0", exit)
	}

	if !strings.Contains(stdout.String(), "property-violation") {
		test.Errorf("stdout = %q, want mention of property-violation", stdout.String())
	}
}
```

`cmd/tusk/cmd_doctor_test.go`:

```go
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

	if _, _, reindexExit := runCLISplit(test, root, "reindex"); reindexExit != 0 {
		test.Fatalf("reindex exit = %d", reindexExit)
	}

	stdout, _, exit := runCLISplit(test, root, "doctor")

	if exit != 0 {
		test.Errorf("exit = %d, want 0", exit)
	}

	if !strings.Contains(stdout.String(), "type-mismatch") {
		test.Errorf("stdout = %q, want mention of type-mismatch", stdout.String())
	}

	if !strings.Contains(stdout.String(), "tickets/bar") {
		test.Errorf("stdout = %q, want mention of tickets/bar", stdout.String())
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./cmd/tusk/... -run "TestReindex_OffSchemaPropertyReportedInSummary|TestDoctor_RendersPropertyTypeMismatch"
```

Expected: FAIL — `cmd_reindex.go` and `cmd_doctor.go` don't yet wire the new fields.

- [ ] **Step 3: Modify `cmd/tusk/cmd_reindex.go`**

In the `reindex.Run(reindex.Config{...})` call, add:

```go
NodeTypes:     loaded.NodeTypes,
PropertyDrift: index.NewPropertyDriftRepo(store),
```

Update the summary-line emission. Today (Plan 7) the conditional is roughly:

```go
if report.WorkflowViolations > 0 {
    fmt.Fprintf(cmd.OutOrStdout(), "Indexed %d nodes (%d workflow-violation%s) in %s\n...", ...)
}
```

Plan 7.b grows it to surface both counts when either is non-zero:

```
Indexed 142 nodes (3 workflow-violations, 5 property-violations) in 240ms
Run `tusk doctor` to inspect violations
```

When both are zero, omit the parenthetical and the hint (Plan 7's existing zero-violations branch). When only one is non-zero, emit only that count (e.g., `(5 property-violations)`).

The implementer chooses the exact formatting helper, but the tests check for the substring `property-violation` in stdout.

- [ ] **Step 4: Modify `cmd/tusk/cmd_doctor.go`**

In the `doctor.Run(doctor.Config{...})` call, add:

```go
PropertyDrift: index.NewPropertyDriftRepo(store),
```

The existing rendering loop already handles arbitrary `Issue.Kind` strings; no further changes needed.

- [ ] **Step 5: Run, verify pass**

```bash
go test ./cmd/tusk/... -run "TestReindex|TestDoctor" -v
```

Expected: every existing test plus the new ones PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/tusk/cmd_reindex.go cmd/tusk/cmd_doctor.go cmd/tusk/cmd_reindex_test.go cmd/tusk/cmd_doctor_test.go
git commit -m "feat(cli): reindex + doctor thread NodeTypes + PropertyDrift; summary count"
```

---

## Task 17: MCP — runtime + tools structured rejection + warnings

**Files:** Modify `internal/mcp/runtime.go`, `internal/mcp/runtime_test.go`, `internal/mcp/tools.go`, `internal/mcp/tools_test.go`.

`Runtime` gains `PropertyDrift *index.PropertyDriftRepo` and threads `NodeTypes` + `PropertyDrift` through both the engine builder and the NodeService constructor. `tusk_node_modify` and `tusk_node_create` return the structured `node-types-rejection` envelope on hard errors and grow a `warnings` array entry on undeclared-property drift. `tusk_doctor` is not in scope (per spec §1.2 / Plan 7's residual ledger).

- [ ] **Step 1: Append failing test to `internal/mcp/runtime_test.go`**

```go
func TestRuntime_OpenWithNodeTypesWiresPropertyDrift(test *testing.T) {
	root := test.TempDir()

	manifestBody := []byte(`
[workspace]
name = "test"

[node-types.ticket]
properties = [
    { name = "summary", type = "string", required = true },
]
`)

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), manifestBody, 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	rt, openErr := mcp.Open(root)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer rt.Close()

	if rt.PropertyDrift == nil {
		test.Errorf("PropertyDrift is nil after Open")
	}

	if _, ok := rt.Manifest.NodeTypes["ticket"]; !ok {
		test.Errorf("Manifest.NodeTypes lacks ticket")
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/mcp/... -run TestRuntime_OpenWithNodeTypesWiresPropertyDrift
```

Expected: FAIL — `Runtime.PropertyDrift` undefined.

- [ ] **Step 3: Modify `internal/mcp/runtime.go`**

Add a field:

```go
PropertyDrift *index.PropertyDriftRepo
```

In `Open`, after `WorkflowDrift` is constructed, also construct `propertyDriftRepo := index.NewPropertyDriftRepo(store)` and assign to the runtime.

In the `node.NewServiceWithBehaviors(...)` call, pass `loaded.NodeTypes` for `nodeTypes` and `propertyDriftRepo` for `propertyDrift`. (The Plan 7 `WorkflowDrift` argument stays as-is.)

In `buildBehaviorEngine` (the helper introduced in Plan 7), construct the declared-keys slice the same way `cmd/tusk/behavior_registry.go` does, and call `Registry.BuildEngineWithDeclaredKeys(loaded, declaredKeys)`.

In `ReloadManifest`, mirror these changes — rebuild `NodeService` with the (possibly updated) `loaded.NodeTypes` and the same `rt.PropertyDrift`.

- [ ] **Step 4: Append failing tests to `internal/mcp/tools_test.go`**

```go
func TestTools_NodeModify_PropertyTypeMismatchStructuredRejection(test *testing.T) {
	rt, harness := newRuntimeWithNodeTypes(test)
	defer harness.Close()

	mustCreateNodeViaRuntime(test, rt, "tickets/foo", "ticket", map[string]any{"summary": "hi"})

	result := callTool(test, harness, "tusk_node_modify", map[string]any{
		"id":  "tickets/foo",
		"set": map[string]any{"priority": "high"},
	})

	if !result.IsError {
		test.Fatalf("expected IsError=true")
	}

	body := decodeJSONContent(test, result)

	if body["error"] != "node-types-rejection" {
		test.Errorf("body.error = %v, want node-types-rejection", body["error"])
	}

	errors, ok := body["errors"].([]any)

	if !ok || len(errors) == 0 {
		test.Fatalf("body.errors absent; body = %v", body)
	}

	first, _ := errors[0].(map[string]any)

	if first["kind"] != "type-mismatch" || first["property"] != "priority" {
		test.Errorf("errors[0] = %v", first)
	}
}

func TestTools_NodeModify_UndeclaredPropertyWarnsOnSuccess(test *testing.T) {
	rt, harness := newRuntimeWithNodeTypes(test)
	defer harness.Close()

	mustCreateNodeViaRuntime(test, rt, "tickets/foo", "ticket", map[string]any{"summary": "hi"})

	result := callTool(test, harness, "tusk_node_modify", map[string]any{
		"id":  "tickets/foo",
		"set": map[string]any{"assignee": "bob"},
	})

	if result.IsError {
		test.Errorf("expected success result, got error: %v", result)
	}

	body := decodeJSONContent(test, result)

	warnings, ok := body["warnings"].([]any)

	if !ok || len(warnings) == 0 {
		test.Fatalf("warnings absent; body = %v", body)
	}

	first, _ := warnings[0].(map[string]any)

	if first["kind"] != "property-drift" || first["property"] != "assignee" {
		test.Errorf("warnings[0] = %v", first)
	}
}

// newRuntimeWithNodeTypes seeds an mcp.Runtime backed by a workspace with a
// node-types declaration on `ticket`. Mirror of Plan 7's newRuntimeWithWorkflow
// helper.
```

The `newRuntimeWithNodeTypes` helper follows the same pattern as Plan 7's `newRuntimeWithWorkflow` — write a `tusk.toml` with the `[node-types.ticket]` section, call `mcp.Open(root)`. The harness, `callTool`, `decodeJSONContent`, and `mustCreateNodeViaRuntime` helpers were introduced in Plan 7's Bundle 6. Reuse.

- [ ] **Step 5: Run, verify fail**

```bash
go test ./internal/mcp/... -run "TestTools_NodeModify_(PropertyTypeMismatch|UndeclaredProperty)"
```

Expected: FAIL — tools.go doesn't yet emit structured property-rejection or property-drift warnings.

- [ ] **Step 6: Modify `internal/mcp/tools.go`**

In `registerNodeModifyTool` (and `registerNodeCreateTool`), the existing handler today does:

- `errors.As(modifyErr, &workflowErr)` → returns workflow rejection envelope.
- Otherwise → `toolError(modifyErr)`.

Plan 7.b adds a check between these two paths:

- If the error matches a property-validation error (the validator returns a joined error of the form `node-types: rejected modify: ticket "tickets/foo" has N errors:\n  - ...`), translate it into a structured envelope. The `errors` array carries one entry per validator-`PropertyError`; entries have keys `kind`, `property`, `type`, `value`, `reason` (per spec §7.2).

The detection uses the `*node.PropertyValidationError` type already introduced in Task 9 — `errors.As(err, &propErr)` extracts the structured `[]PropertyError` slice. This matches the workflow-error pattern (`*workflow.Error`).

The handler builds the structured payload by walking `propErr.Errors`. For each entry, the JSON object carries the validator's structured fields:

| JSON field | Source |
|---|---|
| `kind` | one of `type-mismatch` / `required-missing` / `enum-violation` / `cannot-unset-required` (mapped from `PropertyErrorKind`) |
| `property` | `entry.Property` |
| `type` | `entry.Type` (declared type rendering) |
| `value` | `entry.Value` (omitted when nil for required-missing) |
| `reason` | `entry.Reason` |

Top-level envelope per spec §7.2: `error: "node-types-rejection"`, `node_id`, `node_type` (from `propErr.NodeID` / `propErr.NodeType`), `op` (`propErr.Op`), `errors` (the array).

For drift on the success path: the per-call `bytes.Buffer` warnings writer (introduced in Plan 7's Bundle 6) currently captures workflow-recovery warnings as text. Plan 7.b takes a cleaner direct path: the Service exposes a method `LastDrift() []PropertyDrift` (cleared per call) OR the per-call Service constructed in tools.go is given a structured drift sink (a small interface) that captures the drift entries directly. Either approach works; the implementer picks.

The simpler path — and what the spec §7.2 last paragraph endorses — is for tools.go to construct a per-call Service whose `propertyDrift` field is a small in-memory wrapper that records drift entries during the call AND forwards them to the runtime's real `PropertyDrift` repo afterwards. The handler reads the wrapper's recorded entries to populate the `warnings` array on the success payload. This avoids text re-parsing entirely.

Concrete shape for the in-memory wrapper:

```go
type recordingPropertyDrift struct {
    inner   *index.PropertyDriftRepo
    entries []index.PropertyDriftRow
}

func (r *recordingPropertyDrift) Append(row index.PropertyDriftRow) error {
    r.entries = append(r.entries, row)
    return r.inner.Append(row)
}
// other methods (ListAll, ClearForNode, CountAll) delegate to inner unchanged.
```

This requires `node.Service` to accept `propertyDrift` via an interface rather than a concrete `*index.PropertyDriftRepo`. The simpler path: introduce a small `node.PropertyDriftSink` interface in `internal/node/types.go` matching the four `PropertyDriftRepo` methods, and have the Service hold the interface. `index.PropertyDriftRepo` satisfies the interface naturally; tools.go's `recordingPropertyDrift` also satisfies it.

The implementer chooses whether this cleanup is worth it for v1.b. If not, fall back to text-based parsing of the warnings buffer (Plan 7's pattern, with a new parser line for property warnings). Either approach satisfies the tests.

- [ ] **Step 7: Run, verify pass**

```bash
go test ./internal/mcp/... -v
```

Expected: every existing test plus the new ones PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/mcp/runtime.go internal/mcp/runtime_test.go internal/mcp/tools.go internal/mcp/tools_test.go internal/node/types.go internal/node/service.go
git commit -m "feat(mcp): runtime threads NodeTypes + PropertyDrift; tools surface structured property rejection + drift warnings"
```

(Note: the `node` files are included if the implementer chose the interface path or added a `PropertyValidationError` type.)

---

## Task 18: End-to-end smoke + plan doc commit

**Files:** none new.

- [ ] **Step 1: Run the full unit + race test suite**

```bash
make test
make test-race
```

Expected: every test PASS in both runs.

- [ ] **Step 2: Run lint + vet + format**

```bash
make lint
make vet
make fmt
```

Expected: 0 issues; `make fmt` produces no diff.

- [ ] **Step 3: Build the binary**

```bash
make build
```

Expected: success.

- [ ] **Step 4: End-to-end CLI smoke**

```bash
mkdir -p /tmp/plan-7b-smoke
cd /tmp/plan-7b-smoke
cat > tusk.toml <<'EOF'
[workspace]
name = "smoke"

[node-types.ticket]
description = "Trackable work item"
properties = [
    { name = "summary",    type = "string", required = true },
    { name = "priority", type = "int" },
    { name = "stage",    type = "enum", values = ["pending", "active", "completed"] },
    { name = "labels",   type = "list-of", item-type = "string" },
]
EOF

# Initial: required missing
/workspaces/tusk/bin/tusk node create --type ticket --prop priority=3 --prop stage=pending tickets/incomplete && echo UNEXPECTED-SUCCESS
# Initial: type mismatch
/workspaces/tusk/bin/tusk node create --type ticket --prop summary=hi --prop priority=high --prop stage=pending tickets/badtype && echo UNEXPECTED-SUCCESS
# Initial: enum violation
/workspaces/tusk/bin/tusk node create --type ticket --prop summary=hi --prop stage=shipping tickets/badenum && echo UNEXPECTED-SUCCESS
# Happy path
/workspaces/tusk/bin/tusk node create --type ticket --prop summary="Fix bug" --prop priority=3 --prop stage=pending tickets/foo
# Undeclared property (drift)
/workspaces/tusk/bin/tusk node modify tickets/foo --prop assignee=bob
/workspaces/tusk/bin/tusk doctor
# Required unset
/workspaces/tusk/bin/tusk node modify tickets/foo --unset summary && echo UNEXPECTED-SUCCESS
# Off-schema reindex
cat > tickets/badreindex.md <<'EOF'
---
type: ticket
summary: bad
priority: high
---
EOF
/workspaces/tusk/bin/tusk reindex
/workspaces/tusk/bin/tusk doctor
```

Expected:

- The first three `node create` commands fail with stderr messages naming `title`, `priority`, and `shipping` respectively; no `UNEXPECTED-SUCCESS` printed; no files created.
- The happy-path `node create` succeeds.
- The `node modify --prop assignee=bob` succeeds, prints a stderr warning mentioning `assignee`, and a follow-up `doctor` shows `undeclared-property` for `tickets/foo`.
- `node modify --unset summary` fails with stderr `cannot unset required`.
- The reindex of `tickets/badreindex.md` succeeds (exit 0); summary line includes `property-violation`; doctor shows `type-mismatch` for `tickets/badreindex`.

- [ ] **Step 5: Commit the plan doc**

```bash
git add docs/superpowers/plans/2026-05-07-tusk-v1-7b-node-types.md
git commit -m "docs(plan): plan 7.b — node-types"
```

(May be a no-op if the plan doc was committed earlier on the branch.)

- [ ] **Step 6: Verify branch state and open a draft PR**

```bash
git status
git log --oneline v1..feat/plan-7b
gh pr create --base v1 --head feat/plan-7b --draft \
  --title "feat(v1): plan 7.b — node-types" \
  --body "$(cat <<'EOF'
## Summary
- New [node-types.<t>] manifest section + property-type validation.
- Pure ValidateProperties in internal/node, mirroring edge-type validation.
- Tusk-owned writes reject on type/required/enum violations and on unsetting required properties; undeclared properties allowed with stderr warning + drift surfaced by tusk doctor.
- Reindex runs the validator in warn mode; never aborts indexing.
- New property_drift SQLite table; doctor surfaces four new Issue kinds.
- behavior.NewEngine grows DeclaredKey parameter; engine startup rejects collisions between declared properties and behavior reservations.

## Spec
- docs/superpowers/specs/2026-05-07-tusk-v1-7b-node-types-design.md

## Test plan
- [ ] make test and make test-race green
- [ ] tusk node create rejects required-missing and type-mismatch with stderr message
- [ ] tusk node modify with undeclared property emits stderr warning and persists drift visible to tusk doctor
- [ ] tusk node modify --unset of a required property rejects
- [ ] tusk reindex summary includes property-violation count
- [ ] MCP tusk_node_modify returns structured node-types-rejection JSON on hard errors and warnings array on undeclared-property drift
EOF
)"
```

---

## Self-Review Notes

**1. Spec coverage.** Every spec section maps to at least one task:

| Spec § | Task(s) |
|---|---|
| §1 Goal & Scope | All — Plan 7.b implements the full in-scope list |
| §2 Manifest schema | T1 (decode + types), T2 (basic structural rules), T3 (enum + list-of rules) |
| §3 Validator | T4 (skeleton), T5 (per-scalar-type), T6 (list-of + WhichRequiredWereUnset) |
| §4.1 Constructor change | T8 |
| §4.2 Create order | T9 |
| §4.3 Modify order | T10 |
| §4.4 Hard-error rendering | T9 + T10 |
| §5 Reindex integration | T11 (engine), T16 (CLI summary line) |
| §6 Engine collision detection | T13 |
| §7.1 CLI rendering | T15 (node create + modify), T16 (reindex + doctor) |
| §7.2 MCP rendering | T17 |
| §7.3 Doctor rendering | T12 |
| §7.4 Reindex summary | T16 |
| §8 Testing strategy | tests in every task |
| §9 Open questions | documentation, not tasks |
| §10 Plan 7.c ledger | documentation, not tasks |

**2. Placeholder scan.** No `TBD` / `TODO` / `FIXME` markers. Every task either shows test code in full or describes the production code as behavior + signatures + invariants per the user's code-light feedback. Tests are the precise behavioral spec; the implementer reads the test plus the description plus the spec section reference and writes the implementation.

**3. Type / method consistency:**

- `node.PropertyValidationResult{HardErrors, Drift}` — used identically in T4 (declaration), T5 / T6 (test assertions), T9 / T10 (Service consumers), T11 (reindex consumer).
- `node.PropertyError{Kind, Property, Type, Value, Reason}` — declared T4; consumed T9 / T10 / T11; rendered to MCP envelope T17.
- `node.PropertyErrorKind` constants (`ErrTypeMismatch`, `ErrRequiredMissing`, `ErrEnumViolation`, `ErrCannotUnsetRequired`) — declared T4; consumed everywhere.
- `index.PropertyDriftRow{NodeID, NodeType, Kind, Property, Details, ObservedAt}` — declared T7; consumed T9 / T10 / T11 / T12.
- `behavior.DeclaredKey{NodeType, Property, Source}` — declared T13; consumed T13 + T14.
- `manifest.NodeType` and `manifest.PropertyDecl` — declared T1; used everywhere.
- `Service.NewServiceWithBehaviors` — signature change in T8; existing call sites updated in T8 (with `nil` stubs); real values threaded in T15 + T17.
- `behavior.NewEngine([]Instance, []DeclaredKey)` — signature change in T13; existing callers updated in T13.

**4. Bundle assignment** (one implementer subagent per bundle, per the handoff convention):

| Bundle | Tasks | Theme |
|---|---|---|
| 1 | T1–T3 | Manifest schema for `[node-types]` |
| 2 | T4–T7 | Validator + drift surface |
| 3 | T8–T10 | NodeService integration |
| 4 | T11–T13 | Reindex + doctor + engine collision |
| 5 | T14–T18 | CLI + MCP + e2e + final smoke |

Bundle 5 is the widest, like Plan 7's Bundle 6 — the implementer should expect a larger PR review burden than Bundles 1–4.
