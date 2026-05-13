---
type: plan
title: Plan 7.c.1
status: shipped
pr: 360
shipped-at: "2026-05-07"
implements:
  - Plan 7.c.1 — Pack Platform Spec
  - Tusk v1 Rebuild
---

# Tusk v1 — Plan 7.c.1: Pack Platform and `ref` Property Type

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship two foundations under Plan 7.c: (1) a `tusk pack add <name|url>` CLI command that fetches community-shared TOML pack content and merges it into the user's `tusk.toml` under standard sections, and (2) a `ref` property type that desugars frontmatter strings/wikilinks into typed edges with auto-generated edge types. The engine has no notion of packs as a runtime concept; packs are pure CLI/templating.

**Architecture:** Pack platform lives in `internal/typepacks` as a small set of focused files (alias map, fetch, validate, merge, orchestrator) and a CLI command at `cmd/tusk/cmd_pack_add.go` that acquires the workspace write lock and writes atomically. Ref support extends Plan 7.b's `PropertyDecl` with four optional fields (`To`, `Inverse`, `Acyclic`, `Ordered`); a new manifest-loader pass synthesizes one `EdgeType` per ref property and rejects collisions with explicit `[edge-types.X]`. Runtime resolution lives in `internal/node/refs.go` and is invoked by `Service.Create`/`Service.Modify`/`reindex` after property validation; ref edges piggyback on the existing `edges` table from Plan 2. Doctor surfaces four new issue kinds (`ref_dangling`, `ref_ambiguous`, `ref_type_mismatch`, `ref_cycle`) by reusing the `property_drift` table with new `Kind` values.

**Tech Stack:** Go 1.26, the existing `internal/{manifest,node,reindex,doctor,index,lock,workspace}` packages, plus a new `internal/typepacks` package. Standard library `net/http`, `io.LimitReader`, `os.Rename` for the pack platform; no new external dependencies.

**Spec reference:** `docs/superpowers/specs/2026-05-07-tusk-v1-7c1-pack-platform-and-ref-design.md` (Plan 7.c.1 sub-spec). Plan 7.b conventions carry forward — `docs/superpowers/specs/2026-05-07-tusk-v1-7b-node-types-design.md` is the prerequisite reading.

**Style rules:** Code respects `STYLE.md` — minimum 2-character identifiers (`*testing.T` → `test *testing.T`), blank lines around `err` guards, named errors on shadow. Lefthook enforces pre-commit; never `--no-verify`.

**Plan-doc style:** Tests are shown in full (they are the precise behavioral spec). Production code is described as behavior + signature + key invariants, with type sketches and TOML/JSON examples where they add clarity. Implementer subagents write code from the failing test plus the behavior description plus the spec reference. This matches the convention established in Plans 7 and 7.b.

---

## File Structure

**Created:**

```
internal/typepacks/
  aliases.go               # BuiltinAliases name-to-URL map; Resolve(arg) helper
  aliases_test.go
  fetch.go                 # Fetch(ctx, url) -> []byte; HTTP/file scheme; timeout; size cap; redirect cap
  fetch_test.go            # httptest server + file:// scenarios
  validate.go              # Validate(packBytes) -> *manifest.Manifest; allowed-section check + manifest schema
  validate_test.go
  merge.go                 # FindCollisions + StripSections; --force splice over user manifest text
  merge_test.go
  pack.go                  # AddPack(ctx, source, force, workspaceRoot) orchestrator
  pack_test.go
  testdata/
    sample.toml
    invalid-section.toml
    bad-toml.toml

cmd/tusk/
  cmd_pack.go              # `tusk pack` parent command
  cmd_pack_add.go          # `tusk pack add` subcommand
  cmd_pack_add_test.go

internal/node/
  refs.go                  # ResolveRefs: parse frontmatter values + lookup; ResolveResult
  refs_test.go
```

**Modified:**

```
internal/manifest/manifest.go         # PropertyDecl gains To, Inverse, Acyclic, Ordered fields
internal/manifest/loader.go           # ref-shape validation + auto-edge-type synthesis pass
internal/manifest/loader_test.go      # cover ref schema rules + auto-edge synthesis + collision

internal/node/service.go              # Service.Create + Service.Modify invoke ResolveRefs after property validation
internal/node/service_test.go         # cover Create/Modify ref happy + each rejection kind

internal/reindex/reindex.go           # Config + Report ref-counter fields; per-file walk invokes resolver
internal/reindex/reindex_test.go      # cover ref drift + clean-pass + summary line

internal/doctor/doctor.go             # IssueRefDangling, IssueRefAmbiguous, IssueRefTypeMismatch, IssueRefCycle constants;
                                       # renderPropertyDriftMessage extension

cmd/tusk/cmd_node_create.go           # NewServiceWithBehaviors threads RefResolver via existing wiring; no signature change
cmd/tusk/cmd_node_create_test.go      # ref happy + rejection cases
cmd/tusk/cmd_node_modify.go           # same
cmd/tusk/cmd_node_modify_test.go      # ref happy + rejection cases
cmd/tusk/cmd_reindex.go               # surface ref counters in summary line
cmd/tusk/cmd_reindex_test.go          # cover summary

internal/mcp/tools.go                 # tusk_node_create + tusk_node_modify carry ref kind in structured rejection
internal/mcp/tools_test.go            # cover ref kind shape
```

**Excluded for Plan 7.c.1** (per spec §1.2 / §10 ledger):

- Tags pack content — Plan 7.c.2.
- Kanban pack content — Plan 7.c.3.
- Vault pack content — Plan 7.c.4.
- Subcommand shortcuts (`tusk ticket open`, `tusk note new`, etc.) — deferred from v1.c entirely.
- Filter-grammar shortcuts (`+tag` / `-tag`) — dropped from v1.c entirely.
- `[workspace] type-packs = [...]` activation list — dropped.
- `[type-packs.<name>.<sub>]` override sections — dropped.
- Bare path-string ref values without wikilink brackets — wikilink syntax required.
- `tusk pack list` / `tusk pack remove` — additive; revisit when needed.
- MCP `tusk_pack_add` tool — workspace-config commands stay CLI-only.

---

## Module Conventions for Plan 7.c.1

**`ref` storage in property_drift.** Plan 7.c.1 reuses Plan 7.b's `property_drift` SQLite table for ref drift events. The row shape (`node_id`, `node_type`, `kind`, `property`, `details`, `observed_at`) accommodates the four new `Kind` values without schema migration. `details` carries JSON-encoded per-kind extra fields (`value`, `to`, `candidates`, `actual_type`, `path`).

**Ref resolver shape.** `ResolveRefs(parsed *node.Node, decls map[string]manifest.NodeType, lookup RefLookup) RefResolutionResult` is pure relative to its `RefLookup` interface — the interface is the only I/O, all SQL lives behind it. Tests construct fake `RefLookup` implementations; production wires it to the SQLite-backed lookup. Result envelope mirrors `PropertyValidationResult{HardErrors, Drift}`.

**Service ref invocation.** Ref resolution runs *after* `ValidateProperties` (Plan 7.b) and *before* the behavior `Validate` hooks (Plan 7). On hard error, the create/modify rejects before any file write. On clean pass, the resolver returns a list of `(edgeType, targetID, ordinal)` tuples that the Service merges into `parsed.Edges` so the existing edge-write path persists them.

**Auto-edge synthesis is loader-internal.** A new private function `synthesizeRefEdgeTypes(loaded *Manifest) error` runs after `validateNodeTypes` inside `manifest.Validate`. It mutates `loaded.EdgeTypes` in place. Production code never calls it directly.

**Pack orchestrator shape.** `typepacks.AddPack(ctx context.Context, source string, force bool, workspaceRoot string) error` is the single entry point. It composes alias resolution → fetch → validate → collision detection → atomic write. Returns typed errors so the CLI can map to the documented exit codes.

**Pack TOML format.** A pack file is valid TOML containing only the top-level keys `node-types`, `edge-types`, `behaviors`. Any other top-level section (`workspace`, `embeddings`, custom keys) rejects at validate time.

**Atomic write under workspace lock.** `AddPack` acquires `lock.WorkspaceLock` for the duration of validate/merge/write. Temp-file write + fsync + rename pattern matches the existing `internal/node/service.go` `atomicWrite` helper.

**TDD discipline.** Each task: write the failing test → run to confirm fail → write the minimal implementation → run to confirm pass → commit. Lefthook pre-commit (gofmt, vet, lint, test) runs at each commit; never `--no-verify`. If lefthook blocks, fix the underlying issue.

---

## Task 0: Pre-flight verification

**Files:** none.

- [ ] **Step 1: Verify branch + clean state**

```bash
git rev-parse --abbrev-ref HEAD
git status --porcelain
```

Expected: `feat/plan-7c1`; only the spec doc in `docs/superpowers/` (already committed) and possibly the local `tusk` binary as untracked.

- [ ] **Step 2: Verify pre-Plan-7.c.1 is green**

```bash
make build && make test
```

Expected: build succeeds; all tests pass.

- [ ] **Step 3: Verify spec is in tree**

```bash
test -f docs/superpowers/specs/2026-05-07-tusk-v1-7c1-pack-platform-and-ref-design.md && echo OK
```

Expected: `OK`.

---

## Task 1: Manifest — `PropertyDecl` extension for ref

**Files:** Modify `internal/manifest/manifest.go`, `internal/manifest/loader_test.go`.

Extends the Plan 7.b `PropertyDecl` struct with four optional fields. No validation rules in this task — Task 2 adds them.

- [ ] **Step 1: Append failing test to `internal/manifest/loader_test.go`**

```go
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
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/manifest/... -run TestLoad_DecodesRefPropertyFields
```

Expected: FAIL — `PropertyDecl` lacks `To`, `Inverse`, `Acyclic`, `Ordered`.

- [ ] **Step 3: Extend `PropertyDecl` in `internal/manifest/manifest.go`**

Add four optional fields to the existing `PropertyDecl` struct. Behavior: TOML-decoded only; meaningful only when `Type == "ref"` or (`Type == "list-of"` and `ItemType == "ref"`). Type sketch:

```go
type PropertyDecl struct {
    Name        string   `toml:"name"`
    Type        string   `toml:"type"`
    ItemType    string   `toml:"item-type"`
    Values      []string `toml:"values"`
    Required    bool     `toml:"required"`
    Description string   `toml:"description"`

    // ref-only fields:
    To      string `toml:"to"`
    Inverse string `toml:"inverse"`
    Acyclic bool   `toml:"acyclic"`
    Ordered bool   `toml:"ordered"`
}
```

No new validation in this task. Existing validation paths still apply.

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/manifest/... -run TestLoad_DecodesRefPropertyFields -v
```

Expected: PASS. Run the broader suite to confirm no regression:

```bash
go test ./internal/manifest/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/manifest/manifest.go internal/manifest/loader_test.go
git commit -m "feat(manifest): extend PropertyDecl with ref-only fields"
```

---

## Task 2: Manifest — ref-shape validation rules

**Files:** Modify `internal/manifest/loader.go`, `internal/manifest/loader_test.go`.

Adds the manifest-load-time validation rules from spec §3.2: `to` required when type is `ref`, `to` must reference a declared node-type (or `*`), inapplicable fields rejected.

- [ ] **Step 1: Append failing tests**

```go
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
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/manifest/... -run "TestValidate_(RejectsRef|AcceptsRef|RejectsListOfRef)"
```

Expected: FAIL — ref-shape rules not enforced.

- [ ] **Step 3: Add ref-shape validation in `internal/manifest/loader.go`**

Behavior:
- Extend `validatePropertyTypeConstraints` (or add a sibling) with rules for `Type == "ref"` and `Type == "list-of" && ItemType == "ref"`.
- For both: require `To != ""`. Reject otherwise with message containing `"ref property requires \`to\`"`.
- For `Type == "ref"`: reject if `Values` is non-empty (message contains `"values"`); reject if `ItemType != ""` (message contains `"item-type"`). Existing `validatePropertyTypeConstraints` `default` arm already catches misplaced `ItemType` and `Values` for non-list-of/non-enum types — extend or reorder so ref hits its own arm before the default.
- For `To` non-empty and not `*`: validate against `loaded.NodeTypes` membership. The check needs the full `loaded` reference, so add a top-level walk in `validateNodeTypes` (or a new helper called from `Validate`) that runs after the per-type per-property structural pass. Reject with message containing the offending `to` value.

Pseudocode for the new ref-arm in the type-switch (sketch only — implementer fleshes out):

```go
case "ref":
    if prop.To == "" {
        return fmt.Errorf("manifest: node-types.%s.%s: ref property requires `to`", typeName, prop.Name)
    }

    if len(prop.Values) > 0 {
        return fmt.Errorf("manifest: node-types.%s.%s: ref property cannot declare values", typeName, prop.Name)
    }

    if prop.ItemType != "" {
        return fmt.Errorf("manifest: node-types.%s.%s: ref property cannot declare item-type", typeName, prop.Name)
    }
```

The `to`-target-exists check needs `loaded` in scope, so it lives in a new `validateRefTargets(loaded)` helper called from `Validate` after `validateNodeTypes`.

The `list-of` arm of the existing `validatePropertyTypeConstraints` also needs a sub-case: when `prop.ItemType == "ref"`, require `prop.To != ""` (empty `Values` is fine since enum is a separate ItemType).

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/manifest/... -run "TestValidate_(RejectsRef|AcceptsRef|RejectsListOfRef)" -v
go test ./internal/manifest/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/manifest/loader.go internal/manifest/loader_test.go
git commit -m "feat(manifest): validate ref-property shape and to-target existence"
```

---

## Task 3: Manifest — auto-edge-type synthesis (single-source)

**Files:** Modify `internal/manifest/loader.go`, `internal/manifest/loader_test.go`.

Adds `synthesizeRefEdgeTypes` — a manifest-loader pass that walks every ref-shaped property and produces one `EdgeType` in `loaded.EdgeTypes`. Multi-source merge and explicit-edge-type collision come in Tasks 4 and 5.

- [ ] **Step 1: Append failing tests**

```go
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
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/manifest/... -run TestSynthesize
```

Expected: FAIL — synthesizer not implemented.

- [ ] **Step 3: Implement `synthesizeRefEdgeTypes` in `internal/manifest/loader.go`**

Behavior:
- New private function `synthesizeRefEdgeTypes(loaded *Manifest) error`.
- Called from `Validate` *after* `validateNodeTypes` returns nil and *after* `validateRefTargets` (added in Task 2). Order: structural rules first, then synthesis.
- Initialize `loaded.EdgeTypes` to an empty map if nil.
- Walk `loaded.NodeTypes` in deterministic order — sort the map keys for reproducible `From` slice ordering across runs.
- For each `PropertyDecl` with `Type == "ref"` or (`Type == "list-of" && ItemType == "ref"`):
  - Synthesize an `EdgeType` per the table below.
  - Store under `loaded.EdgeTypes[<property.Name>]`.
- For collisions with already-synthesized entries (same property name on a different node type), Task 4 adds merging — Task 3's implementation may use simple "later writer wins" semantics; Task 4 replaces it with the merge-or-reject logic.

| EdgeType field | Source for plain `ref` | Source for `list-of(ref)` |
|---|---|---|
| `Description` | `"auto-generated from <owning-type>.<prop>"` | same |
| `From` | `[<owning-type>]` | `[<owning-type>]` |
| `To` | `[<prop.To>]` | `[<prop.To>]` |
| `Cardinality` | `manifest.CardinalityManyToOne` | `manifest.CardinalityManyToMany` |
| `Ordered` | `false` | `prop.Ordered` |
| `Inverse` | `prop.Inverse` | same |
| `Acyclic` | `prop.Acyclic` | same |

Signature sketch:

```go
func synthesizeRefEdgeTypes(loaded *Manifest) error {
    if loaded.EdgeTypes == nil {
        loaded.EdgeTypes = EdgeTypes{}
    }

    // sorted iteration over loaded.NodeTypes for deterministic From ordering
    typeNames := make([]string, 0, len(loaded.NodeTypes))

    for typeName := range loaded.NodeTypes {
        typeNames = append(typeNames, typeName)
    }

    sort.Strings(typeNames)

    for _, typeName := range typeNames {
        nodeType := loaded.NodeTypes[typeName]

        for _, prop := range nodeType.Properties {
            if !isRefProperty(prop) {
                continue
            }

            // synthesize and write to loaded.EdgeTypes[prop.Name]
        }
    }

    return nil
}
```

`isRefProperty` returns true when `prop.Type == "ref"` or (`prop.Type == "list-of" && prop.ItemType == "ref"`).

Add `import "sort"` to the loader file.

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/manifest/... -run TestSynthesize -v
go test ./internal/manifest/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/manifest/loader.go internal/manifest/loader_test.go
git commit -m "feat(manifest): synthesize edge-types from ref properties (single-source)"
```

---

## Task 4: Manifest — auto-edge synthesis multi-source merge

**Files:** Modify `internal/manifest/loader.go`, `internal/manifest/loader_test.go`.

Two ref properties with the same name across different node types must merge into one synthesized edge-type by extending `From`. Conflicting attributes reject.

- [ ] **Step 1: Append failing tests**

```go
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
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/manifest/... -run "TestSynthesize_(SameProperty|RejectsConflicting)"
```

Expected: FAIL — naive synthesis doesn't merge or detect conflicts.

- [ ] **Step 3: Implement merge logic in `synthesizeRefEdgeTypes`**

Behavior:
- When iterating ref properties, if `loaded.EdgeTypes[<property.Name>]` already exists *and was synthesized in this pass* (track via a parallel `map[string]bool` `synthesized` set inside the pass), check that the new attributes match the existing entry: `To`, `Cardinality`, `Ordered`, `Inverse`, `Acyclic` must all be equal.
- If they match: append the owning type name to `From` (deduplicated). Because iteration is sorted, the resulting `From` slice is alpha-sorted.
- If any field mismatches: return an error of the form `manifest: ref property %q is declared on both %q and %q with conflicting attributes (%s); align the declarations or use distinct property names` where `%s` names the first mismatching field (e.g., `cardinality: many-to-one vs many-to-many`).
- The existing edge-type collision rule (with explicit `[edge-types.X]`) is Task 5 — leave it for now.

Sketch (helper inside the synthesis pass):

```go
type refSynthesisState struct {
    edgeTypes  EdgeTypes
    synthesized map[string]string // edge-name → first owning-type that produced it
}

func (state *refSynthesisState) addOrMerge(owningType string, prop PropertyDecl) error {
    candidate := buildEdgeTypeFromRef(owningType, prop)

    existing, present := state.edgeTypes[prop.Name]

    if !present {
        state.edgeTypes[prop.Name] = candidate
        state.synthesized[prop.Name] = owningType

        return nil
    }

    // already synthesized? compare attribute-wise.
    if mismatchErr := assertCompatibleSynthesis(existing, candidate, prop.Name, state.synthesized[prop.Name], owningType); mismatchErr != nil {
        return mismatchErr
    }

    existing.From = appendUniqueString(existing.From, owningType)
    state.edgeTypes[prop.Name] = existing

    return nil
}
```

The implementer can either inline this state into the existing `synthesizeRefEdgeTypes` function or create the helper struct shown above — both work.

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/manifest/... -run "TestSynthesize" -v
go test ./internal/manifest/...
```

Expected: PASS for all `TestSynthesize_*` cases.

- [ ] **Step 5: Commit**

```bash
git add internal/manifest/loader.go internal/manifest/loader_test.go
git commit -m "feat(manifest): merge or reject ref-edge synthesis across node types"
```

---

## Task 5: Manifest — auto-edge synthesis collision with explicit `[edge-types.X]`

**Files:** Modify `internal/manifest/loader.go`, `internal/manifest/loader_test.go`.

Spec §3.4: if a synthesized edge-type collides with an explicit user-declared `[edge-types.<same-name>]`, the loader rejects.

- [ ] **Step 1: Append failing tests**

```go
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
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/manifest/... -run TestSynthesize_RejectsCollisionWithExplicitEdgeType
```

Expected: FAIL — Task 4's merge logic happily appends the new owning type to the existing explicit entry.

- [ ] **Step 3: Track explicit-vs-synthesized in `synthesizeRefEdgeTypes`**

Behavior:
- Before iterating ref properties, snapshot the set of edge-type names already in `loaded.EdgeTypes`. These are explicit (user-declared) entries.
- When synthesizing, if the candidate name appears in the explicit-snapshot set → reject:

```
manifest: edge type %q is auto-generated by ref property %q.%q;
remove the explicit [edge-types.%q] declaration or rename the property
```

- The merge-with-existing logic from Task 4 only applies when the existing entry was itself synthesized in this pass. The explicit-snapshot check disambiguates.

Sketch:

```go
func synthesizeRefEdgeTypes(loaded *Manifest) error {
    if loaded.EdgeTypes == nil {
        loaded.EdgeTypes = EdgeTypes{}
    }

    explicit := make(map[string]struct{}, len(loaded.EdgeTypes))

    for name := range loaded.EdgeTypes {
        explicit[name] = struct{}{}
    }

    // ... existing iteration ...

    // when about to write loaded.EdgeTypes[prop.Name]:
    if _, isExplicit := explicit[prop.Name]; isExplicit {
        return fmt.Errorf("manifest: edge type %q is auto-generated by ref property %q.%q; remove the explicit [edge-types.%s] declaration or rename the property", prop.Name, owningType, prop.Name, prop.Name)
    }
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/manifest/... -run TestSynthesize -v
go test ./internal/manifest/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/manifest/loader.go internal/manifest/loader_test.go
git commit -m "feat(manifest): reject ref synthesis colliding with explicit edge-types"
```

---

## Task 6: Node — `RefLookup` interface + `ResolveRefs` parser

**Files:** Create `internal/node/refs.go`, `internal/node/refs_test.go`.

Pure ref-resolution algorithm. Tests use a fake `RefLookup`; production wires it to the SQLite-backed `index.NodeRepo` in Task 7.

- [ ] **Step 1: Write the failing test in `internal/node/refs_test.go`**

```go
package node_test

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

// fakeRefLookup is the test-only RefLookup. Title lookups consult the
// titles map; node-ID lookups consult the ids map.
type fakeRefLookup struct {
	titles map[string]map[string][]string // type → title → []nodeID
	ids    map[string]string              // nodeID → type (presence indicates existence)
}

func (lookup *fakeRefLookup) FindByID(nodeID string) (foundType string, found bool) {
	foundType, found = lookup.ids[nodeID]
	return
}

func (lookup *fakeRefLookup) FindByTitle(targetType, title string) ([]string, error) {
	if targetType == "*" {
		var all []string
		for _, byTitle := range lookup.titles {
			all = append(all, byTitle[title]...)
		}
		return all, nil
	}
	return lookup.titles[targetType][title], nil
}

func newParsedNode(nodeID, nodeType string, props map[string]any) *node.Node {
	return &node.Node{
		ID:         nodeID,
		Type:       nodeType,
		Path:       nodeID + ".md",
		Properties: props,
		Edges:      map[string][]string{},
	}
}

func TestResolveRefs_BareTitleResolves(test *testing.T) {
	parsed := newParsedNode("tickets/auth", "ticket", map[string]any{"assignee": "alice"})

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "assignee", Type: "ref", To: "person"}}},
	}

	lookup := &fakeRefLookup{
		titles: map[string]map[string][]string{"person": {"alice": {"people/alice"}}},
		ids:    map[string]string{"people/alice": "person"},
	}

	result := node.ResolveRefs(parsed, decls, lookup)

	if len(result.HardErrors) > 0 {
		test.Fatalf("HardErrors = %v", result.HardErrors)
	}

	if len(result.Edges) != 1 {
		test.Fatalf("Edges = %v, want 1", result.Edges)
	}

	edge := result.Edges[0]

	if edge.EdgeType != "assignee" || edge.TargetID != "people/alice" || edge.Ordinal != 0 {
		test.Errorf("Edge = %+v", edge)
	}
}

func TestResolveRefs_WikilinkResolvesToNodeID(test *testing.T) {
	parsed := newParsedNode("tickets/auth", "ticket", map[string]any{"assignee": "[[people/alice]]"})

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "assignee", Type: "ref", To: "person"}}},
	}

	lookup := &fakeRefLookup{
		titles: map[string]map[string][]string{},
		ids:    map[string]string{"people/alice": "person"},
	}

	result := node.ResolveRefs(parsed, decls, lookup)

	if len(result.HardErrors) > 0 {
		test.Fatalf("HardErrors = %v", result.HardErrors)
	}

	if len(result.Edges) != 1 || result.Edges[0].TargetID != "people/alice" {
		test.Fatalf("Edges = %+v", result.Edges)
	}
}

func TestResolveRefs_DanglingTitleHardErrors(test *testing.T) {
	parsed := newParsedNode("tickets/auth", "ticket", map[string]any{"assignee": "missing"})

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "assignee", Type: "ref", To: "person"}}},
	}

	lookup := &fakeRefLookup{titles: map[string]map[string][]string{}, ids: map[string]string{}}

	result := node.ResolveRefs(parsed, decls, lookup)

	if len(result.HardErrors) != 1 {
		test.Fatalf("HardErrors = %+v, want 1", result.HardErrors)
	}

	if result.HardErrors[0].Kind != node.RefErrDangling || result.HardErrors[0].Property != "assignee" {
		test.Errorf("HardErrors[0] = %+v", result.HardErrors[0])
	}

	if len(result.Edges) != 0 {
		test.Errorf("Edges = %+v, want none", result.Edges)
	}
}

func TestResolveRefs_AmbiguousTitleHardErrors(test *testing.T) {
	parsed := newParsedNode("tickets/auth", "ticket", map[string]any{"assignee": "alice"})

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "assignee", Type: "ref", To: "person"}}},
	}

	lookup := &fakeRefLookup{
		titles: map[string]map[string][]string{"person": {"alice": {"people/alice-1", "people/alice-2"}}},
		ids:    map[string]string{},
	}

	result := node.ResolveRefs(parsed, decls, lookup)

	if len(result.HardErrors) != 1 || result.HardErrors[0].Kind != node.RefErrAmbiguous {
		test.Fatalf("HardErrors = %+v, want one ambiguous", result.HardErrors)
	}

	if !strings.Contains(result.HardErrors[0].Reason, "people/alice-1") {
		test.Errorf("Reason = %q, expected to mention candidate", result.HardErrors[0].Reason)
	}
}

func TestResolveRefs_TypeMismatchOnWikilink(test *testing.T) {
	parsed := newParsedNode("tickets/auth", "ticket", map[string]any{"assignee": "[[people/bob]]"})

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "assignee", Type: "ref", To: "person"}}},
	}

	lookup := &fakeRefLookup{
		titles: map[string]map[string][]string{},
		ids:    map[string]string{"people/bob": "user"}, // exists, but type=user not person
	}

	result := node.ResolveRefs(parsed, decls, lookup)

	if len(result.HardErrors) != 1 || result.HardErrors[0].Kind != node.RefErrTypeMismatch {
		test.Fatalf("HardErrors = %+v, want one type-mismatch", result.HardErrors)
	}
}

func TestResolveRefs_ListOfRefPreservesOrdinals(test *testing.T) {
	parsed := newParsedNode("tickets/auth", "ticket", map[string]any{
		"watchers": []any{"alice", "[[people/bob]]"},
	})

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{
			{Name: "watchers", Type: "list-of", ItemType: "ref", To: "person"},
		}},
	}

	lookup := &fakeRefLookup{
		titles: map[string]map[string][]string{"person": {"alice": {"people/alice"}}},
		ids:    map[string]string{"people/bob": "person"},
	}

	result := node.ResolveRefs(parsed, decls, lookup)

	if len(result.HardErrors) > 0 {
		test.Fatalf("HardErrors = %+v", result.HardErrors)
	}

	if len(result.Edges) != 2 {
		test.Fatalf("Edges = %+v, want 2", result.Edges)
	}

	if result.Edges[0].TargetID != "people/alice" || result.Edges[0].Ordinal != 0 {
		test.Errorf("Edges[0] = %+v", result.Edges[0])
	}

	if result.Edges[1].TargetID != "people/bob" || result.Edges[1].Ordinal != 1 {
		test.Errorf("Edges[1] = %+v", result.Edges[1])
	}
}

func TestResolveRefs_EmptyValueSkipped(test *testing.T) {
	parsed := newParsedNode("tickets/auth", "ticket", map[string]any{"assignee": ""})

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "assignee", Type: "ref", To: "person"}}},
	}

	lookup := &fakeRefLookup{}

	result := node.ResolveRefs(parsed, decls, lookup)

	if len(result.HardErrors) != 0 || len(result.Edges) != 0 {
		test.Errorf("expected empty result; got %+v", result)
	}
}

func TestResolveRefs_NilValueSkipped(test *testing.T) {
	parsed := newParsedNode("tickets/auth", "ticket", map[string]any{"assignee": nil})

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "assignee", Type: "ref", To: "person"}}},
	}

	lookup := &fakeRefLookup{}

	result := node.ResolveRefs(parsed, decls, lookup)

	if len(result.HardErrors) != 0 || len(result.Edges) != 0 {
		test.Errorf("expected empty result; got %+v", result)
	}
}

func TestResolveRefs_WildcardToAcceptsAnyType(test *testing.T) {
	parsed := newParsedNode("tickets/auth", "ticket", map[string]any{"linked": "[[anything/x]]"})

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "linked", Type: "ref", To: "*"}}},
	}

	lookup := &fakeRefLookup{ids: map[string]string{"anything/x": "whatever"}}

	result := node.ResolveRefs(parsed, decls, lookup)

	if len(result.HardErrors) != 0 || len(result.Edges) != 1 {
		test.Errorf("expected one edge; got %+v", result)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/node/... -run TestResolveRefs
```

Expected: FAIL — `node.ResolveRefs`, `node.RefLookup`, `node.RefErr*` undefined.

- [ ] **Step 3: Implement `internal/node/refs.go`**

Behavior:

```go
package node

import (
    "fmt"
    "regexp"
    "strings"

    "github.com/germanamz/tusk/internal/manifest"
)

// RefLookup is the I/O surface ResolveRefs needs. Production wires it
// to *index.NodeRepo; tests pass synthetic fakes.
type RefLookup interface {
    FindByID(nodeID string) (nodeType string, found bool)
    FindByTitle(targetType, title string) ([]string, error)
}

// RefErrKind classifies a ref resolution failure for doctor and MCP.
type RefErrKind string

const (
    RefErrDangling     RefErrKind = "ref_dangling"
    RefErrAmbiguous    RefErrKind = "ref_ambiguous"
    RefErrTypeMismatch RefErrKind = "ref_type_mismatch"
    // RefErrCycle is emitted by the cycle detector layer, not by
    // ResolveRefs. Declared here so callers reference one constant set.
    RefErrCycle RefErrKind = "ref_cycle"
)

// RefError is one ref-resolution failure for one (property, value).
type RefError struct {
    Kind       RefErrKind
    Property   string
    Value      string
    To         string
    Candidates []string // populated for Ambiguous
    ActualType string   // populated for TypeMismatch
    Reason     string   // human-readable; built from the structured fields
}

// RefEdge is one resolved (edgeType, targetID, ordinal) tuple.
type RefEdge struct {
    EdgeType string
    TargetID string
    Ordinal  int
}

// RefResolutionResult is the resolver's verdict.
type RefResolutionResult struct {
    Edges      []RefEdge
    HardErrors []RefError
}

// wikilinkPattern matches an entire string of the form "[[X]]"; the
// captured group is X.
var wikilinkPattern = regexp.MustCompile(`^\[\[(.+?)\]\]$`)

// ResolveRefs walks every ref-shaped property declared on parsed.Type,
// reads the corresponding value(s) from parsed.Properties, and resolves
// each into a RefEdge or a RefError. Pure relative to lookup.
func ResolveRefs(parsed *Node, decls map[string]manifest.NodeType, lookup RefLookup) RefResolutionResult { /* ... */ }
```

Algorithm body (matches spec §4.2):

1. Read `decls[parsed.Type]`. If not declared (untyped node), return empty result.
2. For each `PropertyDecl` where `isRefProperty(prop)`:
   - Read `parsed.Properties[prop.Name]`. If absent or nil, skip.
   - If the property type is plain `ref`: treat the value as a single string (per the resolution loop below). If the value isn't a string (e.g., it's an array), record a `RefErrDangling` with `Reason: "ref property expects a single string value"`.
   - If the property type is `list-of(ref)`: the value must be `[]any` (post-frontmatter parsing). Iterate, applying the resolution loop to each element with `Ordinal = index`.
3. Resolution loop for one string value at position `ordinal`:
   - Trim whitespace.
   - If empty after trim → skip.
   - If matches `wikilinkPattern`: extract the inner ID. Call `lookup.FindByID(id)`.
     - Not found → `RefError{Kind: Dangling, Property, Value: original, To: prop.To, Reason: ...}`.
     - Found, type == prop.To (or prop.To == "*") → `RefEdge{prop.Name, id, ordinal}`.
     - Found, type mismatch → `RefError{Kind: TypeMismatch, ActualType, ...}`.
   - Otherwise (bare title): call `lookup.FindByTitle(prop.To, value)`.
     - Empty → `RefError{Kind: Dangling, ...}`.
     - One candidate → `RefEdge{...}`.
     - Multiple → `RefError{Kind: Ambiguous, Candidates: result, Reason includes joined IDs, ...}`.

`isRefProperty` is the same predicate as in Task 3; either re-export it from `manifest` or keep a local copy.

`Reason` for each `RefError` is a human-readable string built from the structured fields, e.g.:

```
ref property "assignee" — value "alice" did not match any node of type "person"
ref property "assignee" — value "alice" matched 2 nodes: people/alice-1, people/alice-2
ref property "assignee" — value "[[people/bob]]" target type "user" does not match required "person"
```

Cycle detection (`RefErrCycle`) is *not* implemented in this task; Task 7's Service layer handles cycle detection because it has the in-progress edge set in scope. The constant is declared here so consumers reference one set.

`Node.Properties` already exists from Plan 7.b; `Node.Edges` is `map[string][]string` from Plan 2 — we don't write to it directly here. The Service layer in Task 7 takes `result.Edges` and merges them into `parsed.Edges`.

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/node/... -run TestResolveRefs -v
go test ./internal/node/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/node/refs.go internal/node/refs_test.go
git commit -m "feat(node): ResolveRefs parses ref values and resolves edges"
```

---

## Task 7: Node — Service.Create + Service.Modify ref integration

**Files:** Modify `internal/node/service.go`, `internal/node/service_test.go`.

`Service.Create` and `Service.Modify` invoke `ResolveRefs` after `ValidateProperties`. On hard error, reject before writing the file. On clean pass, merge `result.Edges` into `parsed.Edges` so the existing edge-write path persists them. Cycle detection via the existing `detectCyclesForAcyclicEdges` already handles the resulting edges since synthesized edge-types carry `Acyclic: true` per the property declaration.

This task also threads a `RefLookup` into `Service` via the existing `NewServiceWithBehaviors` constructor — adding an optional argument is backward-compatible because the constructor is the only caller surface and it has callers in `cmd/tusk/cmd_node_create.go`, `cmd/tusk/cmd_node_modify.go`, and the test helpers; all are updated in this task.

- [ ] **Step 1: Append failing tests to `internal/node/service_test.go`**

```go
func TestServiceCreate_RefResolutionDangling(test *testing.T) {
	dir := test.TempDir()
	idx, _ := index.Open(filepath.Join(dir, "idx.db"))
	defer idx.Close()

	repo := index.NewNodeRepo(idx)
	edges := index.NewEdgeRepo(idx)

	nodeTypes := map[string]manifest.NodeType{
		"person": {Properties: []manifest.PropertyDecl{{Name: "name", Type: "string", Required: true}}},
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "assignee", Type: "ref", To: "person"}}},
	}
	edgeTypes := manifest.EdgeTypes{
		"assignee": {From: []string{"ticket"}, To: []string{"person"}, Cardinality: manifest.CardinalityManyToOne},
	}

	service := node.NewServiceWithBehaviors(
		dir, repo, edges, edgeTypes, nil,
		nodeTypes, nil, nil, nil, nil,
		node.NewIndexRefLookup(repo), // helper from this task
	)

	_, createErr := service.Create(node.CreateInput{
		RelPath:    "tickets/auth.md",
		Type:       "ticket",
		Title:      "Auth cleanup",
		Properties: map[string]any{"assignee": "missing"},
	})

	if createErr == nil {
		test.Fatal("expected ref-dangling rejection")
	}

	var refErr *node.RefValidationError

	if !errors.As(createErr, &refErr) {
		test.Fatalf("error is not RefValidationError: %T %v", createErr, createErr)
	}

	if len(refErr.Errors) != 1 || refErr.Errors[0].Kind != node.RefErrDangling {
		test.Errorf("RefValidationError = %+v", refErr)
	}

	// File must not exist (write rejected).
	if _, statErr := os.Stat(filepath.Join(dir, "tickets/auth.md")); !os.IsNotExist(statErr) {
		test.Errorf("file unexpectedly created: stat err = %v", statErr)
	}
}

func TestServiceCreate_RefResolutionHappy(test *testing.T) {
	dir := test.TempDir()
	idx, _ := index.Open(filepath.Join(dir, "idx.db"))
	defer idx.Close()

	repo := index.NewNodeRepo(idx)
	edges := index.NewEdgeRepo(idx)

	nodeTypes := map[string]manifest.NodeType{
		"person": {Properties: []manifest.PropertyDecl{{Name: "name", Type: "string", Required: true}}},
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "assignee", Type: "ref", To: "person"}}},
	}
	edgeTypes := manifest.EdgeTypes{
		"assignee": {From: []string{"ticket"}, To: []string{"person"}, Cardinality: manifest.CardinalityManyToOne},
	}

	service := node.NewServiceWithBehaviors(
		dir, repo, edges, edgeTypes, nil,
		nodeTypes, nil, nil, nil, nil,
		node.NewIndexRefLookup(repo),
	)

	if _, createErr := service.Create(node.CreateInput{
		RelPath:    "people/alice.md",
		Type:       "person",
		Title:      "alice",
		Properties: map[string]any{"name": "Alice"},
	}); createErr != nil {
		test.Fatalf("create person: %v", createErr)
	}

	if _, createErr := service.Create(node.CreateInput{
		RelPath:    "tickets/auth.md",
		Type:       "ticket",
		Title:      "Auth",
		Properties: map[string]any{"assignee": "alice"},
	}); createErr != nil {
		test.Fatalf("create ticket: %v", createErr)
	}

	// Edge must exist.
	rows, _ := edges.ListBySource("tickets/auth")

	if len(rows) != 1 || rows[0].EdgeType != "assignee" || rows[0].TargetID != "people/alice" {
		test.Errorf("edges = %+v", rows)
	}
}

func TestServiceModify_RefRemovedDeletesEdge(test *testing.T) {
	// Setup as above; create alice and the ticket with assignee=alice.
	// Then Modify with --unset assignee. Verify the edge row is gone.

	// (Implementer: copy the boilerplate from the happy-path test, then add:)
	//
	// _, modifyErr := service.Modify(node.ModifyInput{
	//     RelPath: "tickets/auth.md",
	//     UnsetKeys: []string{"assignee"},
	// })
	//
	// rows, _ := edges.ListBySource("tickets/auth")
	// expect len(rows) == 0
}

func TestServiceCreate_RefAcyclicCycleRejected(test *testing.T) {
	// nodeTypes declares "ticket" with parent: ref to ticket, acyclic = true.
	// Create three tickets a, b, c with parents:
	//   a.parent = b
	//   b.parent = c
	// Then attempt:
	//   c.parent = a
	// Expect the create to reject (existing detectCyclesForAcyclicEdges
	// surfaces the cycle through the synthesized edge-type's Acyclic flag).
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/node/... -run "TestServiceCreate_RefResolution|TestServiceModify_RefRemoved|TestServiceCreate_RefAcyclicCycle"
```

Expected: FAIL — `RefLookup` not threaded through Service; `RefValidationError` and `NewIndexRefLookup` undefined.

- [ ] **Step 3: Implement Service ref wiring**

Files affected: `internal/node/service.go` and a new helper `NewIndexRefLookup` (could live alongside `RefLookup` in `internal/node/refs.go` or in `internal/node/refs_repo.go` — implementer's choice; suggested name: keep it in `refs.go`).

Behavior:

1. Add `refs RefLookup` field to `Service`.

2. Extend `NewServiceWithBehaviors` with one new trailing argument `refs RefLookup`. The argument is optional — when nil, ref resolution is a no-op (matches Plan 7.b's pattern of nil-tolerant constructor params for forward-compat). All in-tree callers pass a non-nil value when ref support is wanted.

3. Inside `Service.Create`, after the property-validation step (Plan 7.b's `propResult := ValidateProperties(...)`) and before the behavior-validate dispatch:

   ```go
   if service.refs != nil {
       refResult := ResolveRefs(parsed, service.nodeTypes, service.refs)

       if len(refResult.HardErrors) > 0 {
           return nil, &RefValidationError{
               Op:       "create",
               NodeID:   parsed.ID,
               NodeType: parsed.Type,
               Errors:   refResult.HardErrors,
           }
       }

       for _, edge := range refResult.Edges {
           parsed.Edges[edge.EdgeType] = appendUnique(parsed.Edges[edge.EdgeType], edge.TargetID)
       }
   }
   ```

   Note: `appendUnique` in `service.go` already exists. Ordinal preservation for `list-of(ref)` is implicit because `parsed.Edges[edgeType]` is a `[]string` whose order matches the resolver's iteration; `flattenEdges` writes ordinals by index.

4. The `detectCyclesForAcyclicEdges` call already follows; because synthesized edge-types carry `Acyclic` per the property declaration, no new cycle code is needed.

5. `Service.Modify` gets the same insertion in the same relative position. Modify's pre-image fetch (Plan 7.b §4.3) already provides the previous `parsed.Edges`; the existing edge-rewrite logic computes diffs and emits add/remove edge rows.

6. New error type:

   ```go
   type RefValidationError struct {
       Op       string // "create" | "modify"
       NodeID   string
       NodeType string
       Errors   []RefError
   }

   func (refErr *RefValidationError) Error() string {
       // human-readable summary; iterates Errors and joins Reasons
   }
   ```

7. `NewIndexRefLookup(repo *index.NodeRepo) RefLookup` — production implementation:

   ```go
   type indexRefLookup struct{ repo *index.NodeRepo }

   func (lookup *indexRefLookup) FindByID(nodeID string) (string, bool) {
       row, getErr := lookup.repo.Get(nodeID)
       if getErr != nil || row == nil {
           return "", false
       }
       return row.Type, true
   }

   func (lookup *indexRefLookup) FindByTitle(targetType, title string) ([]string, error) {
       // SELECT id FROM nodes WHERE (type = ?targetType OR ?targetType = '*') AND title = ?title
       // Implementer: add a thin method on NodeRepo (FindByTitle) if one doesn't exist.
   }
   ```

   `NodeRepo.FindByTitle(targetType, title string) ([]string, error)` is a new method on the existing repo. Single SQL query; ordering by `id` for stable doctor candidate lists.

8. Update `cmd/tusk/cmd_node_create.go` and `cmd/tusk/cmd_node_modify.go` to pass `node.NewIndexRefLookup(nodes)` as the new constructor argument. The MCP runtime (`internal/mcp/runtime.go`) does the same in its NodeService construction site.

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/node/... -run "TestServiceCreate_Ref|TestServiceModify_Ref" -v
go test ./internal/node/...
go test ./...
```

Expected: PASS. The whole-suite run catches any constructor-signature drift from threading the new argument.

- [ ] **Step 5: Commit**

```bash
git add internal/node/refs.go internal/node/service.go internal/node/service_test.go internal/index/node_repo.go cmd/tusk/cmd_node_create.go cmd/tusk/cmd_node_modify.go internal/mcp/runtime.go
git commit -m "feat(node): Service.Create/Modify resolve ref properties via index lookup"
```

---

## Task 8: Doctor — ref issue kinds + property_drift extension

**Files:** Modify `internal/doctor/doctor.go`, `internal/doctor/doctor_test.go`. Possibly `internal/index/property_drift_repo.go` (no schema changes — just the Kind constants documented).

Plan 7.c.1 reuses the `property_drift` table for ref drift events. Doctor declares four new `Kind` constants and extends `renderPropertyDriftMessage` to format their detail strings.

- [ ] **Step 1: Append failing tests to `internal/doctor/doctor_test.go`**

```go
func TestRun_SurfacesRefDangling(test *testing.T) {
	idx, _ := index.Open(filepath.Join(test.TempDir(), "idx.db"))
	defer idx.Close()

	driftRepo := index.NewPropertyDriftRepo(idx)

	if appendErr := driftRepo.Append(index.PropertyDriftRow{
		NodeID:   "tickets/auth",
		NodeType: "ticket",
		Kind:     doctor.IssueRefDangling,
		Property: "assignee",
		Details:  `{"value":"alice","to":"person"}`,
	}); appendErr != nil {
		test.Fatalf("append: %v", appendErr)
	}

	report, runErr := doctor.Run(doctor.Config{PropertyDrift: driftRepo})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if len(report.Issues) != 1 || report.Issues[0].Kind != doctor.IssueRefDangling {
		test.Fatalf("Issues = %+v", report.Issues)
	}

	if !strings.Contains(report.Issues[0].Message, "alice") || !strings.Contains(report.Issues[0].Message, "person") {
		test.Errorf("Message = %q", report.Issues[0].Message)
	}
}

func TestRun_SurfacesRefAmbiguous(test *testing.T) {
	idx, _ := index.Open(filepath.Join(test.TempDir(), "idx.db"))
	defer idx.Close()

	driftRepo := index.NewPropertyDriftRepo(idx)

	if appendErr := driftRepo.Append(index.PropertyDriftRow{
		NodeID:   "tickets/auth",
		NodeType: "ticket",
		Kind:     doctor.IssueRefAmbiguous,
		Property: "assignee",
		Details:  `{"value":"alice","to":"person","candidates":["people/alice-1","people/alice-2"]}`,
	}); appendErr != nil {
		test.Fatalf("append: %v", appendErr)
	}

	report, _ := doctor.Run(doctor.Config{PropertyDrift: driftRepo})

	if !strings.Contains(report.Issues[0].Message, "people/alice-1") {
		test.Errorf("Message = %q", report.Issues[0].Message)
	}
}

func TestRun_SurfacesRefTypeMismatch(test *testing.T) {
	idx, _ := index.Open(filepath.Join(test.TempDir(), "idx.db"))
	defer idx.Close()

	driftRepo := index.NewPropertyDriftRepo(idx)

	driftRepo.Append(index.PropertyDriftRow{
		NodeID:   "tickets/auth",
		NodeType: "ticket",
		Kind:     doctor.IssueRefTypeMismatch,
		Property: "assignee",
		Details:  `{"value":"[[people/bob]]","to":"person","actual_type":"user"}`,
	})

	report, _ := doctor.Run(doctor.Config{PropertyDrift: driftRepo})

	if !strings.Contains(report.Issues[0].Message, "user") || !strings.Contains(report.Issues[0].Message, "person") {
		test.Errorf("Message = %q", report.Issues[0].Message)
	}
}

func TestRun_SurfacesRefCycle(test *testing.T) {
	idx, _ := index.Open(filepath.Join(test.TempDir(), "idx.db"))
	defer idx.Close()

	driftRepo := index.NewPropertyDriftRepo(idx)

	driftRepo.Append(index.PropertyDriftRow{
		NodeID:   "tickets/c",
		NodeType: "ticket",
		Kind:     doctor.IssueRefCycle,
		Property: "parent",
		Details:  `{"path":["tickets/a","tickets/b","tickets/c","tickets/a"]}`,
	})

	report, _ := doctor.Run(doctor.Config{PropertyDrift: driftRepo})

	if !strings.Contains(report.Issues[0].Message, "cycle") {
		test.Errorf("Message = %q", report.Issues[0].Message)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/doctor/... -run TestRun_SurfacesRef
```

Expected: FAIL — `IssueRefDangling` etc. constants undefined.

- [ ] **Step 3: Add constants and extend `renderPropertyDriftMessage`**

In `internal/doctor/doctor.go`:

```go
const (
    // ... existing ...

    IssueRefDangling     = "ref_dangling"
    IssueRefAmbiguous    = "ref_ambiguous"
    IssueRefTypeMismatch = "ref_type_mismatch"
    IssueRefCycle        = "ref_cycle"
)
```

Extend `renderPropertyDriftMessage` switch:

```go
case IssueRefDangling:
    return formatRefDangling(row)  // "node-types: ref property %q value %q did not resolve to any %s"
case IssueRefAmbiguous:
    return formatRefAmbiguous(row) // "node-types: ref property %q value %q matches multiple %s candidates: %s"
case IssueRefTypeMismatch:
    return formatRefTypeMismatch(row) // "node-types: ref property %q value %q target type %s does not match required %s"
case IssueRefCycle:
    return formatRefCycle(row) // "node-types: ref property %q forms a cycle: %s"
```

Each `format*` helper parses `row.Details` (JSON-encoded) and produces a human-readable string. Suggested helper signature:

```go
func formatRefDangling(row index.PropertyDriftRow) string {
    var details struct {
        Value string `json:"value"`
        To    string `json:"to"`
    }
    _ = json.Unmarshal([]byte(row.Details), &details) // best-effort
    return fmt.Sprintf("node-types: ref property %q value %q did not resolve to any %q", row.Property, details.Value, details.To)
}
```

The `_ = json.Unmarshal` ignores parse errors; if details are malformed, the message degrades gracefully to the raw row.

Add `import "encoding/json"` to `doctor.go` if not present.

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/doctor/... -run TestRun_SurfacesRef -v
go test ./internal/doctor/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/doctor/doctor.go internal/doctor/doctor_test.go
git commit -m "feat(doctor): surface ref drift issues from property_drift table"
```

---

## Task 9: Reindex — ref drift surfacing + summary counters

**Files:** Modify `internal/reindex/reindex.go`, `internal/reindex/reindex_test.go`. Possibly `cmd/tusk/cmd_reindex.go` for summary line.

Reindex's per-file walk calls `ResolveRefs` after property validation. Hard errors → drift rows in `property_drift` (with the new ref-issue kinds). Clean pass → existing edge rewrite path persists the new edges via the diff with the previous edge set.

- [ ] **Step 1: Append failing tests to `internal/reindex/reindex_test.go`**

```go
func TestReindex_RefDanglingProducesDrift(test *testing.T) {
	dir := test.TempDir()

	// Write tusk.toml with ticket→ref→person; person type declared.
	manifestContent := `
[workspace]
name = "test"

[node-types.person]
properties = [
    { name = "name", type = "string", required = true },
]

[node-types.ticket]
properties = [
    { name = "assignee", type = "ref", to = "person" },
]
`
	os.WriteFile(filepath.Join(dir, "tusk.toml"), []byte(manifestContent), 0o644)

	// Write a ticket file referencing a non-existent person.
	os.MkdirAll(filepath.Join(dir, "tickets"), 0o755)
	os.WriteFile(filepath.Join(dir, "tickets/auth.md"), []byte(
		"---\ntype: ticket\ntitle: Auth\nassignee: missing\n---\n\nbody\n",
	), 0o644)

	idx, _ := index.Open(filepath.Join(dir, ".tusk", "index.db"))
	defer idx.Close()

	driftRepo := index.NewPropertyDriftRepo(idx)
	loaded, _ := manifest.Load(filepath.Join(dir, "tusk.toml"))

	report, runErr := reindex.Run(reindex.Config{
		WorkspaceRoot: dir,
		Nodes:         index.NewNodeRepo(idx),
		Edges:         index.NewEdgeRepo(idx),
		EdgeTypes:     loaded.EdgeTypes,
		NodeTypes:     loaded.NodeTypes,
		PropertyDrift: driftRepo,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if report.RefDangling < 1 {
		test.Errorf("RefDangling = %d, want >= 1", report.RefDangling)
	}

	rows, _ := driftRepo.ListAll()

	var found bool

	for _, row := range rows {
		if row.Kind == doctor.IssueRefDangling && row.Property == "assignee" {
			found = true
		}
	}

	if !found {
		test.Errorf("expected ref_dangling row for assignee, got %+v", rows)
	}
}

func TestReindex_RefCleanPassClearsDrift(test *testing.T) {
	// Setup: same as above, but person/alice exists.
	// Pre-seed driftRepo with a stale ref_dangling row for tickets/auth.
	// Run reindex.
	// Assert: stale row is gone (ClearForNode runs on clean pass).
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/reindex/... -run TestReindex_Ref
```

Expected: FAIL — `Report.RefDangling` undefined; reindex doesn't invoke `ResolveRefs`.

- [ ] **Step 3: Wire ref resolution into reindex**

In `internal/reindex/reindex.go`:

1. Extend `Config` and `Report`:

```go
type Config struct {
    // ... existing fields ...
    NodeTypes     map[string]manifest.NodeType
    PropertyDrift *index.PropertyDriftRepo
}

type Report struct {
    // ... existing ...
    RefDangling     int
    RefAmbiguous    int
    RefTypeMismatch int
    RefCycle        int
}
```

(Plan 7.b already added `NodeTypes` + `PropertyDrift` for property validation. If they're present, reuse them. If not — Plan 7.b wired them in `cmd/tusk/cmd_reindex.go` — verify and reuse.)

2. In the per-file walk, after the existing property validation:

```go
if config.NodeTypes != nil && config.PropertyDrift != nil {
    refLookup := node.NewIndexRefLookup(config.Nodes)
    refResult := node.ResolveRefs(parsed, config.NodeTypes, refLookup)

    for _, refErr := range refResult.HardErrors {
        details, _ := json.Marshal(map[string]any{
            "value":       refErr.Value,
            "to":          refErr.To,
            "candidates":  refErr.Candidates,
            "actual_type": refErr.ActualType,
        })

        config.PropertyDrift.Append(index.PropertyDriftRow{
            NodeID:     parsed.ID,
            NodeType:   parsed.Type,
            Kind:       string(refErr.Kind),
            Property:   refErr.Property,
            Details:    string(details),
            ObservedAt: time.Now().UnixNano(),
        })

        switch refErr.Kind {
        case node.RefErrDangling:
            report.RefDangling++
        case node.RefErrAmbiguous:
            report.RefAmbiguous++
        case node.RefErrTypeMismatch:
            report.RefTypeMismatch++
        case node.RefErrCycle:
            report.RefCycle++
        }
    }

    // On clean pass for THIS node, clear the ref-* drift rows.
    // ClearForNode already deletes everything keyed by node_id; that's the
    // same behavior used for property validation, and is acceptable here too.
    if len(refResult.HardErrors) == 0 {
        // ClearForNode is the existing path; it removes all property_drift
        // rows for this node, including stale ref-* and property-* alike.
    }

    for _, edge := range refResult.Edges {
        parsed.Edges[edge.EdgeType] = appendUniqueString(parsed.Edges[edge.EdgeType], edge.TargetID)
    }
}
```

The `ClearForNode` call on clean pass is the same one Plan 7.b's reindex already invokes after property validation succeeds. It's a single point per node; we don't add a second clear.

3. Summary line — `cmd/tusk/cmd_reindex.go` reads the new counters and adds rows to the summary block when any are non-zero. Format:

```
ref-dangling     <N>
ref-ambiguous    <N>
ref-type-mismatch <N>
ref-cycle        <N>
```

Only print rows whose count is > 0 (matches the existing pattern for property-violations).

4. Watcher integration: the watcher invokes reindex per file via the same `reindex.Config` path. No watcher-specific code needed beyond confirming the watcher's reindex call passes `NodeTypes` and `PropertyDrift` (Plan 7.b already ensured this).

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/reindex/... -run TestReindex_Ref -v
go test ./internal/reindex/...
go test ./...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/reindex/reindex.go internal/reindex/reindex_test.go cmd/tusk/cmd_reindex.go cmd/tusk/cmd_reindex_test.go
git commit -m "feat(reindex): surface ref drift via property_drift; summary counters"
```

---

## Task 10: Pack — alias map + URL resolution

**Files:** Create `internal/typepacks/aliases.go`, `internal/typepacks/aliases_test.go`.

Built-in pack name → URL map. `Resolve(arg)` distinguishes name vs URL by the presence of `://`.

- [ ] **Step 1: Write the failing test in `internal/typepacks/aliases_test.go`**

```go
package typepacks_test

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/typepacks"
)

func TestResolve_BuiltinNameMapsToURL(test *testing.T) {
	url, resolveErr := typepacks.Resolve("kanban")

	if resolveErr != nil {
		test.Fatalf("Resolve(kanban): %v", resolveErr)
	}

	if !strings.HasPrefix(url, "https://raw.githubusercontent.com/germanamz/tusk/") {
		test.Errorf("kanban URL = %q", url)
	}
}

func TestResolve_RawURLPassesThrough(test *testing.T) {
	url, resolveErr := typepacks.Resolve("https://example.com/pack.toml")

	if resolveErr != nil {
		test.Fatalf("Resolve(https): %v", resolveErr)
	}

	if url != "https://example.com/pack.toml" {
		test.Errorf("URL = %q", url)
	}
}

func TestResolve_FileURLPassesThrough(test *testing.T) {
	url, resolveErr := typepacks.Resolve("file:///tmp/pack.toml")

	if resolveErr != nil {
		test.Fatalf("Resolve(file): %v", resolveErr)
	}

	if url != "file:///tmp/pack.toml" {
		test.Errorf("URL = %q", url)
	}
}

func TestResolve_UnknownNameRejects(test *testing.T) {
	_, resolveErr := typepacks.Resolve("not-a-pack")

	if resolveErr == nil {
		test.Fatal("expected error")
	}

	if !strings.Contains(resolveErr.Error(), "unknown pack name") {
		test.Errorf("err = %v", resolveErr)
	}

	if !strings.Contains(resolveErr.Error(), "kanban") {
		test.Errorf("err should list supported names: %v", resolveErr)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/typepacks/... -run TestResolve
```

Expected: FAIL — package doesn't exist.

- [ ] **Step 3: Implement `internal/typepacks/aliases.go`**

```go
// Package typepacks implements `tusk pack add`: fetch, validate, and
// merge community-shared TOML pack content into the workspace manifest.
package typepacks

import (
    "fmt"
    "sort"
    "strings"
)

// BuiltinAliases maps a built-in pack short name to its canonical URL.
// The pack TOML files at these URLs ship in Plans 7.c.2/7.c.3/7.c.4;
// they 404 until then, but the alias map is committed in 7.c.1 so the
// CLI surface is complete.
var BuiltinAliases = map[string]string{
    "kanban": "https://raw.githubusercontent.com/germanamz/tusk/main/packs/kanban.toml",
    "vault":  "https://raw.githubusercontent.com/germanamz/tusk/main/packs/vault.toml",
    "tags":   "https://raw.githubusercontent.com/germanamz/tusk/main/packs/tags.toml",
}

// Resolve maps arg to a fetchable URL. If arg contains "://" it's
// treated as a URL and returned verbatim. Otherwise it must be a
// built-in name from BuiltinAliases.
func Resolve(arg string) (string, error) {
    if strings.Contains(arg, "://") {
        return arg, nil
    }

    url, found := BuiltinAliases[arg]

    if found {
        return url, nil
    }

    return "", fmt.Errorf("pack add: unknown pack name %q; supported names: %s. To install from a URL, pass the full URL.", arg, supportedNames())
}

func supportedNames() string {
    names := make([]string, 0, len(BuiltinAliases))

    for name := range BuiltinAliases {
        names = append(names, name)
    }

    sort.Strings(names)

    return strings.Join(names, ", ")
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/typepacks/... -run TestResolve -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/typepacks/aliases.go internal/typepacks/aliases_test.go
git commit -m "feat(typepacks): pack name alias map and Resolve helper"
```

---

## Task 11: Pack — Fetch (HTTP + file://)

**Files:** Create `internal/typepacks/fetch.go`, `internal/typepacks/fetch_test.go`. Create `internal/typepacks/testdata/sample.toml`.

`Fetch(ctx, url)` reads pack bytes from HTTP/HTTPS or file:// URLs. 30-second timeout, 3-redirect cap, 1 MiB size cap.

- [ ] **Step 1: Write the failing test**

```go
package typepacks_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/typepacks"
)

func TestFetch_HTTPSuccess(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Write([]byte(`[node-types.task]
properties = [{ name = "summary", type = "string" }]
`))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	body, fetchErr := typepacks.Fetch(ctx, server.URL)

	if fetchErr != nil {
		test.Fatalf("Fetch: %v", fetchErr)
	}

	if !strings.Contains(string(body), "node-types.task") {
		test.Errorf("body = %q", body)
	}
}

func TestFetch_HTTP404(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, fetchErr := typepacks.Fetch(context.Background(), server.URL)

	if fetchErr == nil {
		test.Fatal("expected fetch error on 404")
	}

	if !strings.Contains(fetchErr.Error(), "404") {
		test.Errorf("err = %v", fetchErr)
	}
}

func TestFetch_FileURL(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "pack.toml")

	if writeErr := os.WriteFile(path, []byte("[node-types.task]\n"), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	body, fetchErr := typepacks.Fetch(context.Background(), "file://"+path)

	if fetchErr != nil {
		test.Fatalf("Fetch file://: %v", fetchErr)
	}

	if !strings.Contains(string(body), "node-types.task") {
		test.Errorf("body = %q", body)
	}
}

func TestFetch_FileURLNotFound(test *testing.T) {
	_, fetchErr := typepacks.Fetch(context.Background(), "file:///does/not/exist.toml")

	if fetchErr == nil {
		test.Fatal("expected error for missing file")
	}
}

func TestFetch_OversizeRejected(test *testing.T) {
	big := strings.Repeat("x", 2*1024*1024) // 2 MiB

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Write([]byte(big))
	}))
	defer server.Close()

	_, fetchErr := typepacks.Fetch(context.Background(), server.URL)

	if fetchErr == nil {
		test.Fatal("expected oversize error")
	}

	if !strings.Contains(fetchErr.Error(), "size") {
		test.Errorf("err = %v", fetchErr)
	}
}

func TestFetch_TooManyRedirects(test *testing.T) {
	var server *httptest.Server

	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, server.URL, http.StatusFound)
	}))
	defer server.Close()

	_, fetchErr := typepacks.Fetch(context.Background(), server.URL)

	if fetchErr == nil {
		test.Fatal("expected redirect-loop error")
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/typepacks/... -run TestFetch
```

Expected: FAIL — `Fetch` undefined.

- [ ] **Step 3: Implement `internal/typepacks/fetch.go`**

```go
package typepacks

import (
    "context"
    "fmt"
    "io"
    "net/http"
    "net/url"
    "os"
    "strings"
    "time"
)

const (
    fetchTimeout    = 30 * time.Second
    fetchSizeCap    = 1 << 20 // 1 MiB
    fetchRedirectCap = 3
)

// Fetch reads pack bytes from rawURL. Supports http://, https://, and
// file:// schemes. Enforces a 30s timeout, a 1 MiB size cap, and a
// 3-hop redirect cap.
func Fetch(ctx context.Context, rawURL string) ([]byte, error) {
    parsed, parseErr := url.Parse(rawURL)

    if parseErr != nil {
        return nil, fmt.Errorf("pack add: fetch %s: parse url: %w", rawURL, parseErr)
    }

    switch parsed.Scheme {
    case "file":
        body, readErr := os.ReadFile(parsed.Path)

        if readErr != nil {
            return nil, fmt.Errorf("pack add: fetch %s: %w", rawURL, readErr)
        }

        if len(body) > fetchSizeCap {
            return nil, fmt.Errorf("pack add: fetch %s: response exceeds size cap (%d bytes)", rawURL, fetchSizeCap)
        }

        return body, nil

    case "http", "https":
        return fetchHTTP(ctx, rawURL)

    default:
        return nil, fmt.Errorf("pack add: fetch %s: unsupported scheme %q", rawURL, parsed.Scheme)
    }
}

func fetchHTTP(ctx context.Context, rawURL string) ([]byte, error) {
    fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
    defer cancel()

    request, requestErr := http.NewRequestWithContext(fetchCtx, http.MethodGet, rawURL, nil)

    if requestErr != nil {
        return nil, fmt.Errorf("pack add: fetch %s: build request: %w", rawURL, requestErr)
    }

    request.Header.Set("User-Agent", "tusk/v1") // version baked at compile-time later if needed

    client := &http.Client{
        CheckRedirect: func(_ *http.Request, via []*http.Request) error {
            if len(via) >= fetchRedirectCap {
                return fmt.Errorf("redirect loop after %d hops", fetchRedirectCap)
            }

            return nil
        },
    }

    response, doErr := client.Do(request)

    if doErr != nil {
        if strings.Contains(doErr.Error(), "redirect") {
            return nil, fmt.Errorf("pack add: fetch %s: redirect loop", rawURL)
        }

        return nil, fmt.Errorf("pack add: fetch %s: %w", rawURL, doErr)
    }

    defer response.Body.Close()

    if response.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("pack add: fetch %s: HTTP %d", rawURL, response.StatusCode)
    }

    limited := io.LimitReader(response.Body, int64(fetchSizeCap)+1)
    body, readErr := io.ReadAll(limited)

    if readErr != nil {
        return nil, fmt.Errorf("pack add: fetch %s: read body: %w", rawURL, readErr)
    }

    if len(body) > fetchSizeCap {
        return nil, fmt.Errorf("pack add: fetch %s: response exceeds size cap (%d bytes)", rawURL, fetchSizeCap)
    }

    return body, nil
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/typepacks/... -run TestFetch -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/typepacks/fetch.go internal/typepacks/fetch_test.go
git commit -m "feat(typepacks): Fetch with HTTP/file scheme, size cap, redirect cap, timeout"
```

---

## Task 12: Pack — Validate (allowed sections + manifest schema)

**Files:** Create `internal/typepacks/validate.go`, `internal/typepacks/validate_test.go`. Create test fixtures `internal/typepacks/testdata/{sample,invalid-section,bad-toml}.toml`.

Pack content is decoded twice — once for shape (allowed top-level keys), once through the manifest validator. Rejects on either.

- [ ] **Step 1: Create test fixtures**

`internal/typepacks/testdata/sample.toml`:

```toml
[node-types.task]
description = "A unit of work"
properties = [
    { name = "summary", type = "string", required = true },
    { name = "priority", type = "int" },
]

[edge-types.parent]
description = "Hierarchical parent"
from = ["task"]
to = ["task"]
cardinality = "many-to-one"
acyclic = true
```

`internal/typepacks/testdata/invalid-section.toml`:

```toml
[workspace]
name = "evil-workspace"

[node-types.task]
properties = [{ name = "summary", type = "string" }]
```

`internal/typepacks/testdata/bad-toml.toml`:

```toml
[node-types.task]
properties = [
    { name = "summary"
```

- [ ] **Step 2: Write the failing test**

```go
package typepacks_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/typepacks"
)

func TestValidate_HappyPath(test *testing.T) {
	body, _ := os.ReadFile(filepath.Join("testdata", "sample.toml"))

	loaded, validateErr := typepacks.Validate(body)

	if validateErr != nil {
		test.Fatalf("Validate: %v", validateErr)
	}

	if _, ok := loaded.NodeTypes["task"]; !ok {
		test.Errorf("missing task type")
	}

	if _, ok := loaded.EdgeTypes["parent"]; !ok {
		test.Errorf("missing parent edge")
	}
}

func TestValidate_RejectsDisallowedSection(test *testing.T) {
	body, _ := os.ReadFile(filepath.Join("testdata", "invalid-section.toml"))

	_, validateErr := typepacks.Validate(body)

	if validateErr == nil {
		test.Fatal("expected validate error")
	}

	if !strings.Contains(validateErr.Error(), "workspace") {
		test.Errorf("err = %v", validateErr)
	}
}

func TestValidate_RejectsBadTOML(test *testing.T) {
	body, _ := os.ReadFile(filepath.Join("testdata", "bad-toml.toml"))

	_, validateErr := typepacks.Validate(body)

	if validateErr == nil {
		test.Fatal("expected validate error")
	}

	if !strings.Contains(validateErr.Error(), "TOML") && !strings.Contains(validateErr.Error(), "decode") {
		test.Errorf("err = %v", validateErr)
	}
}

func TestValidate_RejectsRefToMissingType(test *testing.T) {
	body := []byte(`
[node-types.ticket]
properties = [
    { name = "assignee", type = "ref", to = "person" },
]
`)

	_, validateErr := typepacks.Validate(body)

	if validateErr == nil || !strings.Contains(validateErr.Error(), "person") {
		test.Errorf("expected ref-to-missing-type error: %v", validateErr)
	}
}
```

- [ ] **Step 3: Run, verify fail**

```bash
go test ./internal/typepacks/... -run TestValidate
```

Expected: FAIL — `typepacks.Validate` undefined.

- [ ] **Step 4: Implement `internal/typepacks/validate.go`**

```go
package typepacks

import (
    "fmt"

    "github.com/BurntSushi/toml"

    "github.com/germanamz/tusk/internal/manifest"
)

// allowedTopLevelKeys is the closed set of section names a pack file
// may declare. Any other top-level key rejects.
var allowedTopLevelKeys = map[string]struct{}{
    "node-types": {},
    "edge-types": {},
    "behaviors":  {},
}

// Validate decodes packBytes, asserts top-level shape, and runs the
// content through the same manifest validator the engine uses. Returns
// the parsed manifest fragment on success.
func Validate(packBytes []byte) (*manifest.Manifest, error) {
    // (a) raw decode to detect disallowed top-level keys.
    var raw map[string]toml.Primitive

    rawMeta, rawErr := toml.Decode(string(packBytes), &raw)

    if rawErr != nil {
        return nil, fmt.Errorf("pack add: invalid TOML: %w", rawErr)
    }

    for _, key := range rawMeta.Keys() {
        topLevel := key[0]

        if _, ok := allowedTopLevelKeys[topLevel]; !ok {
            return nil, fmt.Errorf("pack add: pack contains disallowed top-level section %q (packs may only contain [node-types], [edge-types], [behaviors])", topLevel)
        }
    }

    // (b) typed decode + manifest schema validation.
    loaded := &manifest.Manifest{}

    typedMeta, typedErr := toml.Decode(string(packBytes), loaded)

    if typedErr != nil {
        return nil, fmt.Errorf("pack add: decode pack: %w", typedErr)
    }

    loaded.Meta = &typedMeta

    if validateErr := manifest.Validate(loaded); validateErr != nil {
        return nil, fmt.Errorf("pack add: %w", validateErr)
    }

    return loaded, nil
}
```

Note: `rawMeta.Keys()` returns multi-segment keys (e.g., `["node-types", "task"]`); iterating their first segment captures every top-level section. Verify against the BurntSushi/toml docs — if `Keys()` doesn't behave this way in our version, an alternative is to scan the raw TOML for `^\[([^\.]+)` headers via regex.

- [ ] **Step 5: Run, verify pass**

```bash
go test ./internal/typepacks/... -run TestValidate -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/typepacks/validate.go internal/typepacks/validate_test.go internal/typepacks/testdata/
git commit -m "feat(typepacks): validate pack TOML against allowed sections + manifest schema"
```

---

## Task 13: Pack — Collision detection + --force splice

**Files:** Create `internal/typepacks/merge.go`, `internal/typepacks/merge_test.go`.

`FindCollisions(userBody []byte, pack *manifest.Manifest)` returns the set of overlapping sections. `StripSections(userBody []byte, sections []string)` removes the named sections from a TOML body via header-line regex.

- [ ] **Step 1: Write the failing test**

```go
package typepacks_test

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/typepacks"
)

func TestFindCollisions_NoOverlap(test *testing.T) {
	user := []byte(`
[node-types.note]
properties = [{ name = "summary", type = "string" }]
`)

	pack := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{
			"task": {Properties: []manifest.PropertyDecl{{Name: "summary", Type: "string"}}},
		},
	}

	collisions, findErr := typepacks.FindCollisions(user, pack)

	if findErr != nil {
		test.Fatalf("FindCollisions: %v", findErr)
	}

	if len(collisions) != 0 {
		test.Errorf("collisions = %v, want none", collisions)
	}
}

func TestFindCollisions_NodeTypeOverlap(test *testing.T) {
	user := []byte(`
[node-types.task]
properties = [{ name = "summary", type = "string" }]
`)

	pack := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{
			"task": {Properties: []manifest.PropertyDecl{{Name: "summary", Type: "string"}}},
		},
	}

	collisions, _ := typepacks.FindCollisions(user, pack)

	if len(collisions) != 1 || collisions[0] != "node-types.task" {
		test.Errorf("collisions = %v, want [node-types.task]", collisions)
	}
}

func TestFindCollisions_EdgeAndBehaviorOverlap(test *testing.T) {
	user := []byte(`
[edge-types.parent]
from = ["task"]
to = ["task"]
cardinality = "many-to-one"

[behaviors.workflow.kanban]
applies-to = ["task"]
`)

	pack := &manifest.Manifest{
		EdgeTypes: manifest.EdgeTypes{
			"parent": {From: []string{"task"}, To: []string{"task"}, Cardinality: manifest.CardinalityManyToOne},
		},
		Behaviors: map[string]map[string]toml.Primitive{
			"workflow": {"kanban": {}},
		},
	}

	collisions, _ := typepacks.FindCollisions(user, pack)

	wantSet := map[string]bool{"edge-types.parent": false, "behaviors.workflow.kanban": false}

	for _, key := range collisions {
		wantSet[key] = true
	}

	for key, found := range wantSet {
		if !found {
			test.Errorf("missing expected collision %q (got %v)", key, collisions)
		}
	}
}

func TestStripSections_RemovesNamedHeaders(test *testing.T) {
	body := []byte(`[workspace]
name = "ws"

[node-types.task]
properties = [{ name = "summary", type = "string" }]

[edge-types.parent]
from = ["task"]
to = ["task"]
cardinality = "many-to-one"

[node-types.note]
properties = [{ name = "summary", type = "string" }]
`)

	stripped := typepacks.StripSections(body, []string{"node-types.task", "edge-types.parent"})

	if strings.Contains(string(stripped), "[node-types.task]") {
		test.Errorf("stripped output still contains [node-types.task]: %q", stripped)
	}

	if strings.Contains(string(stripped), "[edge-types.parent]") {
		test.Errorf("stripped output still contains [edge-types.parent]: %q", stripped)
	}

	if !strings.Contains(string(stripped), "[node-types.note]") {
		test.Errorf("stripped output unexpectedly removed [node-types.note]: %q", stripped)
	}

	if !strings.Contains(string(stripped), "[workspace]") {
		test.Errorf("stripped output unexpectedly removed [workspace]: %q", stripped)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/typepacks/... -run "TestFindCollisions|TestStripSections"
```

Expected: FAIL.

- [ ] **Step 3: Implement `internal/typepacks/merge.go`**

```go
package typepacks

import (
    "bytes"
    "fmt"
    "regexp"

    "github.com/BurntSushi/toml"

    "github.com/germanamz/tusk/internal/manifest"
)

// FindCollisions returns the qualified section names ("node-types.X",
// "edge-types.X", "behaviors.K.I") that exist in both the user manifest
// (raw bytes) and the pack manifest.
func FindCollisions(userBody []byte, pack *manifest.Manifest) ([]string, error) {
    userManifest := &manifest.Manifest{}

    if _, decodeErr := toml.Decode(string(userBody), userManifest); decodeErr != nil {
        return nil, fmt.Errorf("typepacks: decode user manifest: %w", decodeErr)
    }

    var collisions []string

    for typeName := range pack.NodeTypes {
        if _, present := userManifest.NodeTypes[typeName]; present {
            collisions = append(collisions, "node-types."+typeName)
        }
    }

    for edgeName := range pack.EdgeTypes {
        if _, present := userManifest.EdgeTypes[edgeName]; present {
            collisions = append(collisions, "edge-types."+edgeName)
        }
    }

    for kindName, perInstance := range pack.Behaviors {
        for instanceName := range perInstance {
            if userKind, present := userManifest.Behaviors[kindName]; present {
                if _, has := userKind[instanceName]; has {
                    collisions = append(collisions, fmt.Sprintf("behaviors.%s.%s", kindName, instanceName))
                }
            }
        }
    }

    return collisions, nil
}

// sectionHeaderPattern matches a TOML section header on a line by
// itself (with optional leading/trailing whitespace).
var sectionHeaderPattern = regexp.MustCompile(`(?m)^\s*\[([^\]]+)\]\s*$`)

// StripSections removes the named sections from body. Sections is a
// list of qualified names like "node-types.task". Adjacent comments
// above the section header are removed with the section.
func StripSections(body []byte, sections []string) []byte {
    target := make(map[string]struct{}, len(sections))

    for _, name := range sections {
        target[name] = struct{}{}
    }

    var output bytes.Buffer
    var current bytes.Buffer
    var currentHeader string

    flush := func() {
        if _, drop := target[currentHeader]; drop {
            current.Reset()

            return
        }

        output.Write(current.Bytes())
        current.Reset()
    }

    for _, line := range bytes.SplitAfter(body, []byte("\n")) {
        match := sectionHeaderPattern.FindSubmatch(line)

        if match != nil {
            flush()
            currentHeader = string(match[1])
        }

        current.Write(line)
    }

    flush()

    return output.Bytes()
}
```

Notes for implementer:
- `StripSections` walks line-by-line. Each section header starts a new buffered block; on the next header (or EOF) the block flushes (or drops). This naturally takes adjacent comments preceding a header *into* the previous section's block — which means leading comments above a section are removed with it. That matches spec §2.7.
- The `[behaviors.workflow.kanban]` case has a 3-segment header. The regex captures `behaviors.workflow.kanban` as the inner string; the `target` map matches it directly. (Tests cover this.)
- Multi-line array/inline-table values inside a section are NOT parsed — `StripSections` is a header-based splitter, not a TOML decoder. This is fine because we only need to remove whole sections, never partial ones.

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/typepacks/... -run "TestFindCollisions|TestStripSections" -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/typepacks/merge.go internal/typepacks/merge_test.go
git commit -m "feat(typepacks): FindCollisions and StripSections for --force splice"
```

---

## Task 14: Pack — `AddPack` orchestrator (no CLI yet)

**Files:** Create `internal/typepacks/pack.go`, `internal/typepacks/pack_test.go`.

`AddPack` composes Resolve → Fetch → Validate → FindCollisions → (StripSections if --force) → atomic write under workspace lock.

- [ ] **Step 1: Write the failing test**

```go
package typepacks_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/typepacks"
)

func TestAddPack_HappyFileURL(test *testing.T) {
	dir := test.TempDir()

	if writeErr := os.WriteFile(filepath.Join(dir, "tusk.toml"), []byte(`
[workspace]
name = "test"
`), 0o644); writeErr != nil {
		test.Fatalf("write tusk.toml: %v", writeErr)
	}

	packPath := filepath.Join(dir, "pack.toml")

	if writeErr := os.WriteFile(packPath, []byte(`
[node-types.task]
properties = [{ name = "summary", type = "string" }]
`), 0o644); writeErr != nil {
		test.Fatalf("write pack.toml: %v", writeErr)
	}

	if addErr := typepacks.AddPack(context.Background(), "file://"+packPath, false, dir); addErr != nil {
		test.Fatalf("AddPack: %v", addErr)
	}

	body, _ := os.ReadFile(filepath.Join(dir, "tusk.toml"))

	if !strings.Contains(string(body), "[node-types.task]") {
		test.Errorf("tusk.toml = %q", body)
	}

	if !strings.Contains(string(body), "Added by `tusk pack add") {
		test.Errorf("missing pack header comment: %q", body)
	}
}

func TestAddPack_CollisionRejectsWithoutForce(test *testing.T) {
	dir := test.TempDir()

	os.WriteFile(filepath.Join(dir, "tusk.toml"), []byte(`
[workspace]
name = "test"

[node-types.task]
properties = [{ name = "summary", type = "string" }]
`), 0o644)

	packPath := filepath.Join(dir, "pack.toml")
	os.WriteFile(packPath, []byte(`
[node-types.task]
properties = [{ name = "summary", type = "string" }, { name = "priority", type = "int" }]
`), 0o644)

	addErr := typepacks.AddPack(context.Background(), "file://"+packPath, false, dir)

	if addErr == nil {
		test.Fatal("expected collision error")
	}

	if !strings.Contains(addErr.Error(), "node-types.task") {
		test.Errorf("err = %v", addErr)
	}

	// User manifest must be byte-identical to before.
	body, _ := os.ReadFile(filepath.Join(dir, "tusk.toml"))

	if strings.Contains(string(body), "priority") {
		test.Errorf("user manifest unexpectedly mutated: %q", body)
	}
}

func TestAddPack_CollisionWithForceOverwrites(test *testing.T) {
	dir := test.TempDir()

	os.WriteFile(filepath.Join(dir, "tusk.toml"), []byte(`
[workspace]
name = "test"

[node-types.task]
properties = [{ name = "summary", type = "string" }]

[node-types.note]
properties = [{ name = "summary", type = "string" }]
`), 0o644)

	packPath := filepath.Join(dir, "pack.toml")
	os.WriteFile(packPath, []byte(`
[node-types.task]
properties = [{ name = "summary", type = "string" }, { name = "priority", type = "int" }]
`), 0o644)

	if addErr := typepacks.AddPack(context.Background(), "file://"+packPath, true, dir); addErr != nil {
		test.Fatalf("AddPack --force: %v", addErr)
	}

	body, _ := os.ReadFile(filepath.Join(dir, "tusk.toml"))

	// task section is replaced (priority appears now).
	if !strings.Contains(string(body), "priority") {
		test.Errorf("expected pack content with priority, got %q", body)
	}

	// note section is preserved.
	if !strings.Contains(string(body), "[node-types.note]") {
		test.Errorf("--force should not touch unrelated sections: %q", body)
	}
}

func TestAddPack_RejectsBadPack(test *testing.T) {
	dir := test.TempDir()

	os.WriteFile(filepath.Join(dir, "tusk.toml"), []byte("[workspace]\nname = \"test\"\n"), 0o644)

	packPath := filepath.Join(dir, "pack.toml")
	os.WriteFile(packPath, []byte("[workspace]\nname = \"evil\"\n"), 0o644)

	addErr := typepacks.AddPack(context.Background(), "file://"+packPath, false, dir)

	if addErr == nil || !strings.Contains(addErr.Error(), "workspace") {
		test.Errorf("expected disallowed-section error, got %v", addErr)
	}

	// Manifest unchanged.
	body, _ := os.ReadFile(filepath.Join(dir, "tusk.toml"))

	if strings.Contains(string(body), "evil") {
		test.Errorf("manifest unexpectedly mutated: %q", body)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/typepacks/... -run TestAddPack
```

Expected: FAIL — `AddPack` undefined.

- [ ] **Step 3: Implement `internal/typepacks/pack.go`**

```go
package typepacks

import (
    "context"
    "fmt"
    "os"
    "path/filepath"
    "time"

    "github.com/germanamz/tusk/internal/lock"
    "github.com/germanamz/tusk/internal/workspace"
)

// AddPack fetches pack content from source, validates it, detects
// collisions with the user's tusk.toml, and atomically writes the
// merged manifest. Returns the first error encountered without
// modifying tusk.toml.
func AddPack(ctx context.Context, source string, force bool, workspaceRoot string) error {
    rawURL, resolveErr := Resolve(source)

    if resolveErr != nil {
        return resolveErr
    }

    workspaceLock, lockErr := lock.NewWorkspaceLock(workspaceRoot)

    if lockErr != nil {
        return fmt.Errorf("pack add: workspace lock: %w", lockErr)
    }

    if acquireErr := workspaceLock.Acquire(ctx); acquireErr != nil {
        return fmt.Errorf("pack add: acquire lock: %w", acquireErr)
    }

    defer workspaceLock.Release()

    manifestPath := filepath.Join(workspaceRoot, workspace.ManifestFilename)

    userBody, readErr := os.ReadFile(manifestPath)

    if readErr != nil {
        return fmt.Errorf("pack add: read tusk.toml: %w", readErr)
    }

    packBody, fetchErr := Fetch(ctx, rawURL)

    if fetchErr != nil {
        return fetchErr
    }

    pack, validateErr := Validate(packBody)

    if validateErr != nil {
        return validateErr
    }

    collisions, collisionErr := FindCollisions(userBody, pack)

    if collisionErr != nil {
        return collisionErr
    }

    if len(collisions) > 0 && !force {
        return fmt.Errorf("pack add: cannot apply pack from %s: %d colliding sections in tusk.toml: %s\nre-run with --force to overwrite, or remove the colliding sections by hand", rawURL, len(collisions), formatCollisionList(collisions))
    }

    finalBody := userBody

    if len(collisions) > 0 && force {
        finalBody = StripSections(userBody, collisions)
    }

    composed := composeManifest(finalBody, packBody, source)

    if writeErr := atomicWrite(manifestPath, composed); writeErr != nil {
        return fmt.Errorf("pack add: write tusk.toml: %w", writeErr)
    }

    return nil
}

// composeManifest concatenates the user portion (possibly with sections
// stripped), a header comment naming the source and date, and the pack
// body. Pack body is appended verbatim — no TOML re-emit.
func composeManifest(userBody, packBody []byte, source string) []byte {
    header := fmt.Sprintf("\n\n# Added by `tusk pack add %s` on %s\n", source, time.Now().Format("2006-01-02"))

    var output []byte
    output = append(output, userBody...)

    // Ensure a single trailing newline before the header.
    if len(output) > 0 && output[len(output)-1] != '\n' {
        output = append(output, '\n')
    }

    output = append(output, header...)
    output = append(output, packBody...)

    return output
}

// atomicWrite writes content to path via a sibling temp file, fsync,
// and rename. Mirrors internal/node/service.go atomicWrite.
func atomicWrite(path string, content []byte) error {
    dir := filepath.Dir(path)
    tempFile, createErr := os.CreateTemp(dir, ".tusk-pack-*.tmp")

    if createErr != nil {
        return createErr
    }

    tempPath := tempFile.Name()

    if _, writeErr := tempFile.Write(content); writeErr != nil {
        tempFile.Close()
        os.Remove(tempPath)

        return writeErr
    }

    if syncErr := tempFile.Sync(); syncErr != nil {
        tempFile.Close()
        os.Remove(tempPath)

        return syncErr
    }

    if closeErr := tempFile.Close(); closeErr != nil {
        os.Remove(tempPath)

        return closeErr
    }

    if renameErr := os.Rename(tempPath, path); renameErr != nil {
        os.Remove(tempPath)

        return renameErr
    }

    return nil
}

func formatCollisionList(sections []string) string {
    var output string

    for _, section := range sections {
        output += "\n  - [" + section + "]"
    }

    return output
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/typepacks/... -run TestAddPack -v
go test ./internal/typepacks/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/typepacks/pack.go internal/typepacks/pack_test.go
git commit -m "feat(typepacks): AddPack orchestrator with workspace lock + atomic write"
```

---

## Task 15: CLI — `tusk pack add` command

**Files:** Create `cmd/tusk/cmd_pack.go`, `cmd/tusk/cmd_pack_add.go`, `cmd/tusk/cmd_pack_add_test.go`. Modify `cmd/tusk/main.go` (or wherever the root command is wired) to register the new subtree.

`tusk pack add <name|url>` invokes `typepacks.AddPack` and maps errors to documented exit codes.

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackAdd_HappyFileURL(test *testing.T) {
	dir := test.TempDir()

	previous := chdir(test, dir)
	defer previous()

	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"init", "--name", "test"})

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("init: %v", execErr)
	}

	packPath := filepath.Join(dir, "pack.toml")
	os.WriteFile(packPath, []byte(`
[node-types.task]
properties = [{ name = "summary", type = "string" }]
`), 0o644)

	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{"pack", "add", "file://" + packPath})

	var stdout, stderr bytes.Buffer

	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("pack add: %v", execErr)
	}

	body, _ := os.ReadFile(filepath.Join(dir, "tusk.toml"))

	if !strings.Contains(string(body), "[node-types.task]") {
		test.Errorf("tusk.toml = %q", body)
	}
}

func TestPackAdd_UnknownNameFails(test *testing.T) {
	dir := test.TempDir()
	previous := chdir(test, dir)
	defer previous()

	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"init", "--name", "test"})
	rootCmd.Execute()

	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{"pack", "add", "not-a-pack"})

	var stderr bytes.Buffer

	rootCmd.SetErr(&stderr)

	execErr := rootCmd.Execute()

	if execErr == nil {
		test.Fatal("expected error")
	}

	if !strings.Contains(execErr.Error(), "unknown pack name") {
		test.Errorf("err = %v", execErr)
	}
}

func TestPackAdd_RejectsCollisionWithoutForce(test *testing.T) {
	dir := test.TempDir()
	previous := chdir(test, dir)
	defer previous()

	os.WriteFile(filepath.Join(dir, "tusk.toml"), []byte(`
[workspace]
name = "test"

[node-types.task]
properties = [{ name = "summary", type = "string" }]
`), 0o644)

	packPath := filepath.Join(dir, "pack.toml")
	os.WriteFile(packPath, []byte(`
[node-types.task]
properties = [{ name = "summary", type = "string" }, { name = "priority", type = "int" }]
`), 0o644)

	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"pack", "add", "file://" + packPath})
	rootCmd.SetErr(new(bytes.Buffer))

	if execErr := rootCmd.Execute(); execErr == nil {
		test.Fatal("expected collision error")
	}

	body, _ := os.ReadFile(filepath.Join(dir, "tusk.toml"))

	if strings.Contains(string(body), "priority") {
		test.Errorf("manifest unexpectedly mutated: %q", body)
	}
}

func TestPackAdd_ForceOverwrites(test *testing.T) {
	// Same setup as the previous test; pass --force; verify priority lands.
}
```

`chdir` is a small test helper:

```go
func chdir(test *testing.T, target string) func() {
	test.Helper()

	previous, _ := os.Getwd()

	if chErr := os.Chdir(target); chErr != nil {
		test.Fatalf("chdir: %v", chErr)
	}

	return func() { os.Chdir(previous) }
}
```

(If `cmd/tusk/cmd_test_helpers_test.go` already provides one, reuse it.)

- [ ] **Step 2: Run, verify fail**

```bash
go test ./cmd/tusk/... -run TestPackAdd
```

Expected: FAIL — `pack add` not registered.

- [ ] **Step 3: Implement the cobra commands**

`cmd/tusk/cmd_pack.go`:

```go
package main

import "github.com/spf13/cobra"

func newPackCmd() *cobra.Command {
    packCmd := &cobra.Command{
        Use:   "pack",
        Short: "Manage type packs",
    }

    packCmd.AddCommand(newPackAddCmd())

    return packCmd
}
```

`cmd/tusk/cmd_pack_add.go`:

```go
package main

import (
    "fmt"
    "os"

    "github.com/spf13/cobra"

    "github.com/germanamz/tusk/internal/typepacks"
)

func newPackAddCmd() *cobra.Command {
    var force bool

    addCmd := &cobra.Command{
        Use:   "add <name-or-url>",
        Short: "Fetch and merge a type pack into tusk.toml",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            cwd, getCwdErr := os.Getwd()

            if getCwdErr != nil {
                return getCwdErr
            }

            if addErr := typepacks.AddPack(cmd.Context(), args[0], force, cwd); addErr != nil {
                return addErr
            }

            fmt.Fprintf(cmd.OutOrStdout(), "pack add: applied %q to tusk.toml\n", args[0])

            return nil
        },
    }

    addCmd.Flags().BoolVar(&force, "force", false, "remove colliding sections from tusk.toml before appending the pack")

    return addCmd
}
```

Register in `newRootCmd` (wherever `newInitCmd`, `newNodeCmd`, etc. are added):

```go
rootCmd.AddCommand(newPackCmd())
```

Exit-code mapping (per spec §2.8) is mostly handled implicitly:
- `0` for success.
- `1` for unknown-name / collision-without-force / argument errors (cobra returns these as plain errors).
- `2` for fetch failures — these come back from `typepacks.Fetch` already prefixed with `"pack add: fetch ..."`.
- `3` for validation failures — `typepacks.Validate` errors are prefixed with `"pack add:"` and contain `"invalid TOML"`, `"disallowed top-level"`, or schema errors.

The CLI returns a single error from `RunE`; cobra exits with code 1 on any non-nil error. Differentiating exit codes 2/3 requires inspecting the error and calling `os.Exit(N)` from a small wrapper. To keep this simple, the spec's exit-code table is achieved by adding a thin error classifier:

```go
RunE: func(cmd *cobra.Command, args []string) error {
    cwd, _ := os.Getwd()
    addErr := typepacks.AddPack(cmd.Context(), args[0], force, cwd)

    if addErr == nil {
        fmt.Fprintf(cmd.OutOrStdout(), "pack add: applied %q to tusk.toml\n", args[0])
        return nil
    }

    msg := addErr.Error()

    switch {
    case strings.Contains(msg, "fetch"):
        os.Exit(2)
    case strings.Contains(msg, "invalid TOML"), strings.Contains(msg, "disallowed top-level"), strings.Contains(msg, "decode pack"):
        os.Exit(3)
    }

    return addErr // cobra exits with 1
},
```

(String-matching exit codes is admittedly fragile — but Plan 7.b uses a similar pattern for property errors, and this matches the existing v1 CLI conventions. A more principled `pack add: ErrFetch / ErrValidate` typed error set is in the §10 ledger as a residual.)

- [ ] **Step 4: Run, verify pass**

```bash
go test ./cmd/tusk/... -run TestPackAdd -v
go test ./...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/tusk/cmd_pack.go cmd/tusk/cmd_pack_add.go cmd/tusk/cmd_pack_add_test.go cmd/tusk/main.go
git commit -m "feat(cli): tusk pack add subcommand with --force"
```

---

## Task 16: MCP — surface ref kinds in structured rejection

**Files:** Modify `internal/mcp/tools.go`, `internal/mcp/tools_test.go`.

`tusk_node_create` and `tusk_node_modify` already structure rejections (Plan 7.b §7.2). When `node.RefValidationError` is returned, the MCP envelope's `kind` field carries `ref_dangling`, `ref_ambiguous`, `ref_type_mismatch`, or `ref_cycle`.

- [ ] **Step 1: Append failing test**

```go
func TestTusk_NodeCreate_RefDanglingReturnsStructuredKind(test *testing.T) {
	dir := test.TempDir()

	manifestContent := `
[workspace]
name = "test"

[node-types.person]
properties = [{ name = "name", type = "string", required = true }]

[node-types.ticket]
properties = [{ name = "assignee", type = "ref", to = "person" }]
`
	os.WriteFile(filepath.Join(dir, "tusk.toml"), []byte(manifestContent), 0o644)

	runtime, _ := mcp.Open(dir)
	defer runtime.Close()

	result, callErr := runtime.CallTool("tusk_node_create", map[string]any{
		"path":  "tickets/auth.md",
		"type":  "ticket",
		"title": "Auth",
		"properties": map[string]any{
			"assignee": "missing",
		},
	})

	if callErr != nil {
		test.Fatalf("CallTool: %v", callErr)
	}

	body := decodeJSONEnvelope(test, result)

	if body["ok"] != false {
		test.Errorf("ok = %v, want false", body["ok"])
	}

	errors := body["errors"].([]any)

	if len(errors) != 1 {
		test.Fatalf("errors = %v", errors)
	}

	first := errors[0].(map[string]any)

	if first["kind"] != "ref_dangling" {
		test.Errorf("kind = %v, want ref_dangling", first["kind"])
	}

	if first["property"] != "assignee" {
		test.Errorf("property = %v", first["property"])
	}
}
```

(Reuse `decodeJSONEnvelope` from existing MCP tests.)

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/mcp/... -run TestTusk_NodeCreate_Ref
```

Expected: FAIL — current envelope renderer doesn't recognize `RefValidationError`.

- [ ] **Step 3: Extend the MCP envelope renderer**

Find the existing function in `internal/mcp/tools.go` that renders `PropertyValidationError` to the structured envelope (Plan 7.b's contribution). Add a parallel `errors.As` arm for `*node.RefValidationError`:

```go
var refErr *node.RefValidationError

if errors.As(err, &refErr) {
    var rendered []map[string]any

    for _, refError := range refErr.Errors {
        rendered = append(rendered, map[string]any{
            "kind":     string(refError.Kind),
            "property": refError.Property,
            "value":    refError.Value,
            "to":       refError.To,
            "candidates": refError.Candidates, // nil-safe; JSON omits if nil
            "actual_type": refError.ActualType,
            "reason":   refError.Reason,
        })
    }

    return map[string]any{
        "ok":     false,
        "errors": rendered,
    }, nil
}
```

`tusk_node_modify` does the same — both tools share the rendering path.

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/mcp/... -run TestTusk_NodeCreate_Ref -v
go test ./internal/mcp/...
go test ./...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/tools.go internal/mcp/tools_test.go
git commit -m "feat(mcp): surface ref-validation errors with structured kind"
```

---

## Task 17: End-to-end smoke + draft PR

**Files:** none new; runs verification.

- [ ] **Step 1: Full test suite + race**

```bash
make test
make test-race
```

Expected: PASS for both.

- [ ] **Step 2: Lint + vet + fmt**

```bash
make lint
make vet
make fmt
git status --porcelain  # confirm fmt didn't dirty the tree
```

Expected: PASS, clean tree.

- [ ] **Step 3: End-to-end manual smoke**

```bash
go build -o bin/tusk ./cmd/tusk
SMOKE=$(mktemp -d)
cd "$SMOKE"

./bin/tusk init --name smoke
cat > /tmp/sample-pack.toml <<'EOF'
[node-types.person]
properties = [{ name = "name", type = "string", required = true }]

[node-types.task]
properties = [
    { name = "summary", type = "string", required = true },
    { name = "assignee", type = "ref", to = "person" },
]
EOF

./bin/tusk pack add file:///tmp/sample-pack.toml
cat tusk.toml

./bin/tusk node create --type person --title alice --prop name=Alice
./bin/tusk node create --type task --title "Ship the thing" --prop summary="ship" --prop assignee=alice
./bin/tusk query 'edge.assignee'  # should list one row

# Negative: dangling ref rejects
if ./bin/tusk node create --type task --title bad --prop summary="bad" --prop assignee=ghost ; then
    echo "FAIL: should have rejected"
    exit 1
fi
echo "PASS: dangling ref rejected"

# Negative: collision without --force rejects
if ./bin/tusk pack add file:///tmp/sample-pack.toml ; then
    echo "FAIL: should have rejected (collision)"
    exit 1
fi
echo "PASS: collision rejected"

# Force overwrites
./bin/tusk pack add --force file:///tmp/sample-pack.toml

# Doctor surfaces nothing for a clean workspace
./bin/tusk doctor

cd - && rm -rf "$SMOKE"
```

Expected: every echo succeeds; final doctor surfaces no issues.

- [ ] **Step 4: Commit any incidental fixes from smoke; push branch**

```bash
git status
git push -u origin feat/plan-7c1
```

- [ ] **Step 5: Open draft PR**

```bash
git log --oneline v1..feat/plan-7c1
gh pr create --base v1 --head feat/plan-7c1 --draft \
  --title "feat(v1): plan 7.c.1 — pack platform and ref" \
  --body "$(cat <<'EOF'
## Summary
- New `tusk pack add <name|url>` CLI command for fetching and merging community-shared TOML pack content into the workspace manifest. Built-in name aliases for kanban/vault/tags resolve to canonical URLs in the tusk repo. URL-fetch with no cache; collision detection with --force override; atomic write under workspace lock.
- New `ref` property type. Frontmatter values resolve as wikilink (node-ID lookup) or bare title (lookup within `to` type scope). Auto-generated edge type per ref property; collision with explicit [edge-types.X] rejected. Multi-source merge for the same property name across node types.
- Validator integration following Plan 7.b's pattern: dangling/ambiguous/type-mismatched/cyclic refs reject for tusk-owned writes; surface via `tusk doctor` for external edits.
- Doctor extension: four new issue kinds (ref_dangling, ref_ambiguous, ref_type_mismatch, ref_cycle) reusing the property_drift table.
- Reindex extension: counters per kind; summary line surfaces totals.
- Architectural shift vs v1 design spec §7.1: drops the [workspace] type-packs activation list and [type-packs.X.<sub>] override sections in favor of pure-templating semantics. Documented in the spec doc (§1.2 / §10).

## Spec
- docs/superpowers/specs/2026-05-07-tusk-v1-7c1-pack-platform-and-ref-design.md

## Test plan
- [ ] make test and make test-race green
- [ ] tusk pack add file://<sample.toml> appends pack content to tusk.toml under standard sections
- [ ] tusk pack add of an alias whose URL 404s exits non-zero (kanban/vault/tags pack URLs aren't live until 7.c.2-4)
- [ ] tusk pack add with collision exits 1; with --force succeeds and only the colliding sections are replaced
- [ ] tusk node create with a ref property to a non-existent target rejects; file is not created
- [ ] tusk node create with bare-title and wikilink ref shapes both produce the same edge in the index
- [ ] tusk node modify --unset of a ref property removes the corresponding edge
- [ ] tusk reindex surfaces ref drift counts and tusk doctor renders the new issue kinds
- [ ] MCP tusk_node_create returns structured kind=ref_dangling on rejection
EOF
)"
```

---

## Summary

This plan ships:

1. A pack templating mechanism (`tusk pack add`) that fetches TOML from canonical URLs or arbitrary URLs and merges into the user's workspace manifest under standard sections. The engine has no notion of packs; they're purely a CLI/templating affordance.

2. A `ref` property type that desugars frontmatter strings (bare titles or wikilinks) into typed edges with auto-generated edge types. Manifest-load-time validation catches malformed declarations and target-existence errors; runtime resolution surfaces dangling/ambiguous/type-mismatched/cyclic refs as `property_drift` rows for `tusk doctor`.

Subsequent plans 7.c.2 (tags), 7.c.3 (kanban), and 7.c.4 (vault) ship pack content built on this foundation.

---

## Spec

`docs/superpowers/specs/2026-05-07-tusk-v1-7c1-pack-platform-and-ref-design.md`

---

## Self-Review Notes

**1. Spec coverage.** Every spec section maps to at least one task:

| Spec § | Task(s) |
|---|---|
| §1 Goal & Scope | All — Plan 7.c.1 implements the full in-scope list |
| §2.1 Command shape | T15 |
| §2.2 Name alias resolution | T10 |
| §2.3 Fetch | T11 |
| §2.4 Pre-merge validation | T12 |
| §2.5 Collision detection | T13 (+ T14 wires it) |
| §2.6 Atomic write | T14 |
| §2.7 Append style | T14 (composeManifest) |
| §2.8 Exit codes | T15 |
| §3.1 Schema extension | T1 |
| §3.2 Manifest-load validation | T2 |
| §3.3 Auto-edge synthesis | T3 |
| §3.4 Collision rule | T5 |
| §3.5 Multi-source merge | T4 |
| §4.1-4.2 Authoring + parse algorithm | T6 |
| §4.3 Edge representation | T6 (no extra storage) + T7 (Service merges into parsed.Edges) |
| §4.4 Validation outcomes | T7 (tusk-owned reject) + T9 (reindex drift) |
| §4.5 Required interaction | T7 (existing Plan 7.b code path) |
| §4.6 Modify edge case | T7 |
| §4.7 Cycle detection | T7 (existing detectCyclesForAcyclicEdges via synthesized Acyclic flag) |
| §4.8 Self-reference | T7 (no special code; works by default) |
| §4.9 Watcher / reindex | T9 |
| §5.1 Service.Create | T7 |
| §5.2 Service.Modify | T7 |
| §5.3 Reindex | T9 |
| §5.4 Watcher | T9 (piggybacks on reindex) |
| §5.5 Doctor | T8 |
| §5.6 Lock + atomicity | T14 |
| §6 MCP surface | T16 (existing tools surface new kinds) |
| §7 File layout | T1-T16 distribute the file additions per the layout |
| §8 Testing | tests in every task |
| §9 Open questions | documentation, not tasks |
| §10 Plan 7.c+ ledger | documentation, not tasks |

**2. Placeholder scan.** No `TBD` / `TODO` / `FIXME` markers. Every task either shows test code in full or describes the production code as behavior + signatures + invariants per the established convention. Tests are the precise behavioral spec; the implementer reads the test plus the description plus the spec section reference and writes the implementation.

**3. Type / method consistency:**

- `manifest.PropertyDecl` extension fields `To, Inverse, Acyclic, Ordered` — declared T1; used T2-T5; consumed by `node.ResolveRefs` indirectly via the synthesized edge types.
- `node.RefLookup` interface — declared T6; production impl `NewIndexRefLookup` in T7; consumed by `Service.Create`/`Service.Modify` T7 and reindex T9.
- `node.RefError{Kind, Property, Value, To, Candidates, ActualType, Reason}` — declared T6; consumed T7 (Service rejection) + T9 (reindex drift detail JSON) + T16 (MCP envelope).
- `node.RefErrKind` constants (`RefErrDangling`, `RefErrAmbiguous`, `RefErrTypeMismatch`, `RefErrCycle`) — declared T6; used T7 + T9 + T16.
- `node.RefEdge{EdgeType, TargetID, Ordinal}` — declared T6; consumed T7 (merged into parsed.Edges) + T9 (same).
- `node.RefValidationError{Op, NodeID, NodeType, Errors}` — declared T7; consumed T16 (MCP renderer).
- `doctor.IssueRefDangling` etc. constants — declared T8; used in reindex T9 and the smoke test T17.
- `typepacks.Resolve, typepacks.Fetch, typepacks.Validate, typepacks.FindCollisions, typepacks.StripSections, typepacks.AddPack` — each declared in its own task (T10-T14); composed in T14 (AddPack); CLI consumes AddPack only T15.
- `typepacks.BuiltinAliases` — declared T10; not directly accessed by tests after T10 but stable.

**4. Bundle assignment** (one implementer subagent per bundle, per the established convention):

| Bundle | Tasks | Theme |
|---|---|---|
| 1 | T1-T2 | Manifest ref schema (PropertyDecl extension + load-time validation) |
| 2 | T3-T5 | Manifest auto-edge synthesis (single-source, multi-source merge, explicit-collision) |
| 3 | T6-T7 | `ResolveRefs` algorithm + Service.Create/Modify integration |
| 4 | T8-T9 | Doctor + Reindex + watcher (the drift surface) |
| 5 | T10-T14 | Pack platform internals (alias map, fetch, validate, merge, orchestrator) |
| 6 | T15-T17 | Pack CLI command + MCP integration + e2e smoke + draft PR |

Bundle 5 is the widest by file count but each file is small and well-isolated — five independent functions composed by `AddPack`. Bundle 6 finishes the plan and produces the draft PR.
