# Phase 5 — Task 4: Filter compiler parses type-literals as type-refs

**Phase:** 5 (Reference-resolution grammar)
**Spec:** § *Naming conventions*, § *Reference resolution in code*

**Goal:** User-input *node-type literals* that arrive through filter
expressions (`type=section`, `type=:section`, `type=markdown:section`,
including the same forms nested inside edge predicates like
`references->type=:section`) are parsed via `typeref.Parse` and
compiled to scope-aware SQL predicates against `nodes(source, type)`.
Bare names keep their current meaning (`ScopeAny`); qualified forms
select the user-namespace or a specific source per the spec grammar.

**Out of scope (see follow-up task):** qualified syntax for the
*edge-type* itself (`:references->X`, `markdown:references->X`)
requires lexer/parser changes — `:` is not a valid identifier
character today and edge types sit in identifier position, so the
existing grammar rejects these forms. Splitting this out keeps the
compiler change minimal and lets the parser/validator work land
together as one coherent change.

## Why this differs from the originally-drafted plan

The original draft assumed `internal/query` exposes a `NodeType string`
field on `query.Request` / `query.ListRequest` that gets translated to
SQL at the use site. The current code has no such field: every type
name a caller passes ends up inside the `Filter` string and is parsed
by `internal/filter` into an AST, then compiled by
`internal/filter/compile.go` to a `type = ?` predicate (and similarly
`edges.type = ?` inside `EdgePredicate` compilation).

The graph-expansion `EdgeTypes []string` plumbing is already
typeref-aware (`query_service.go:393`, `semantic_subunits.go:152`,
both done in Tasks 5.2 and 5.3). What is left is the
filter-expression compile path, which is where the remaining
user-input type strings flow.

## Inherits From

After Task 5.3:
- `typeref.Parse` / `ParseMany` available.
- `EdgeRef` and `NeighborsByEdgeRefs` available.
- Graph-expansion walker takes `EdgeRefs`.

After Task 5.2:
- `nodes` and `edges` rows both carry a nullable `source` column.

## Files

- **Modify:** `internal/filter/compile.go`
- **Modify:** `internal/filter/compile_test.go`
- **Modify:** any existing `compile_test.go` expectation that asserts
  the literal SQL `type = ?` shape (the bare path must still produce
  that exact shape for backward compatibility, so most tests are
  unaffected; only tests that pin the full SQL byte-for-byte may need
  re-wording).

## Steps

- [x] **Step 1: Locate the type-literal compile sites**

The two in-scope SQL emit sites for node-type literals are:

1. `compileProperty` (`internal/filter/compile.go:171`) — emits
   `type = ?` (or `type != ?`, etc.) for top-level `type=X` predicates.
2. `compilePropertyOnAlias` (`internal/filter/compile.go:347`) — emits
   `<alias>.type = ?` for `type=X` predicates inside edge predicates
   (`references->type=section`).

The edge-type emit site (`compileEdgePredicate`, line 297) is
deferred — see Out of scope above. The traversal-shortcut path
(`compileTraversalShortcut`) takes the edge type from
`shortcut.EdgeType` which the validator resolved from manifest
configuration; that name is already canonical.

- [x] **Step 2: Write the failing test**

In `internal/filter/compile_test.go`, add a focused unit test that
checks each scope produces the right SQL fragment + params for a
top-level `type=` predicate. Use `Compile` end-to-end so the parser
also exercises the lexer's bareword + colon acceptance.

```go
func TestCompileNodeTypeRefScopes(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		input      string
		wantClause string
		wantParams []any
	}{
		{
			name:       "bare type",
			input:      "type=section",
			wantClause: "type = ?",
			wantParams: []any{"section"},
		},
		{
			name:       "user-namespace type",
			input:      "type=:section",
			wantClause: "source IS NULL AND type = ?",
			wantParams: []any{"section"},
		},
		{
			name:       "source-qualified type",
			input:      "type=markdown:section",
			wantClause: "source = ? AND type = ?",
			wantParams: []any{"markdown", "section"},
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			expr, parseErrs := filter.NewParser(testCase.input).Parse()
			if len(parseErrs) > 0 {
				test.Fatalf("parse: %v", parseErrs[0])
			}

			sqlText, params, compileErr := filter.Compile(expr, filter.CompileOptions{})
			if compileErr != nil {
				test.Fatalf("compile: %v", compileErr)
			}

			if !strings.Contains(sqlText, testCase.wantClause) {
				test.Errorf("sql missing %q\ngot: %s", testCase.wantClause, sqlText)
			}

			if !reflect.DeepEqual(params, testCase.wantParams) {
				test.Errorf("params = %v, want %v", params, testCase.wantParams)
			}
		})
	}
}
```

Add a companion test for edge-type scopes in an edge predicate:

```go
func TestCompileEdgeTypeRefScopes(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name           string
		input          string
		wantEdgeClause string
		wantFirstParam any
	}{
		{
			name:           "bare edge type",
			input:          "references->type=section",
			wantEdgeClause: "e0.type = ?",
			wantFirstParam: "references",
		},
		{
			name:           "user-namespace edge type",
			input:          ":references->type=section",
			wantEdgeClause: "e0.source IS NULL AND e0.type = ?",
			wantFirstParam: "references",
		},
		{
			name:           "source-qualified edge type",
			input:          "markdown:references->type=section",
			wantEdgeClause: "e0.source = ? AND e0.type = ?",
			wantFirstParam: "markdown",
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			// Drive the test through a manifest-validated AST so
			// edge-type validation does not reject our scope-qualified
			// names. Build the manifest with an edge type "references"
			// declared so Validate(...) is happy regardless of scope.
			loaded := manifest.Manifest{
				EdgeTypes: map[string]manifest.EdgeType{
					"references": {Cardinality: manifest.CardinalityOneToMany},
				},
			}

			expr, parseErrs := filter.NewParser(testCase.input).Parse()
			if len(parseErrs) > 0 {
				test.Fatalf("parse: %v", parseErrs[0])
			}

			if validateErrs := filter.Validate(expr, loaded); len(validateErrs) > 0 {
				test.Fatalf("validate: %v", validateErrs[0])
			}

			sqlText, params, compileErr := filter.Compile(expr, filter.CompileOptions{})
			if compileErr != nil {
				test.Fatalf("compile: %v", compileErr)
			}

			if !strings.Contains(sqlText, testCase.wantEdgeClause) {
				test.Errorf("sql missing %q\ngot: %s", testCase.wantEdgeClause, sqlText)
			}

			if len(params) == 0 || params[0] != testCase.wantFirstParam {
				test.Errorf("first param = %v, want %v", params, testCase.wantFirstParam)
			}
		})
	}
}
```

(Adjust imports — `reflect`, `strings`, `filter`, `manifest` — to
match the existing test file's conventions.)

- [x] **Step 3: Run the new tests to verify they fail**

Run:
```
go test ./internal/filter/... -run 'TestCompileNodeTypeRefScopes|TestCompileEdgeTypeRefScopes' -v
```

Expected: FAIL — the bare-name cases pass, but `:section` and
`markdown:section` are currently compiled as literal strings into the
value param, so neither the `source IS NULL AND` nor the
`source = ? AND` clauses appear and the wrong params are passed.

If the edge-type validator rejects `:references` or
`markdown:references` (it checks the literal string against the
manifest edge-type map), the failure will surface there first. That
is expected; the next step also updates the validator to typeref-parse
the edge type before lookup.

- [x] **Step 4: Extend `compileProperty` / `compilePropertyOnAlias` for `type`**

In `internal/filter/compile.go`:

a. Add a tiny helper that emits the scope-aware fragment for a typed
column. Place it next to `compileProperty`:

```go
// compileTypeRef parses `raw` as a typeref and returns a SQL fragment
// + params constraining a `(source, type)` pair to the parsed scope.
// `columnPrefix` is "" for the nodes table or "<alias>." for an
// aliased nodes/edges row.
func compileTypeRef(raw, columnPrefix string, op Op) (string, []any, error) {
	ref, parseErr := typeref.Parse(raw)

	if parseErr != nil {
		return "", nil, fmt.Errorf("compile: parse type ref: %w", parseErr)
	}

	sqlOp := opToSQL(op)

	switch ref.Scope {
	case typeref.ScopeAny:
		return fmt.Sprintf("%stype %s ?", columnPrefix, sqlOp), []any{ref.Type}, nil
	case typeref.ScopeUser:
		return fmt.Sprintf("%ssource IS NULL AND %stype %s ?", columnPrefix, columnPrefix, sqlOp), []any{ref.Type}, nil
	case typeref.ScopeSource:
		return fmt.Sprintf("%ssource = ? AND %stype %s ?", columnPrefix, columnPrefix, sqlOp), []any{ref.Source, ref.Type}, nil
	}

	return "", nil, fmt.Errorf("compile: unknown typeref scope %v", ref.Scope)
}
```

b. In `compileProperty`, branch on `predicate.Property == "type"`
before the generic core-column emit:

```go
if predicate.Property == "type" {
	stringValue, ok := predicate.Value.(StringValue)

	if !ok {
		return "", nil, fmt.Errorf("compile: type predicate with non-StringValue")
	}

	return compileTypeRef(stringValue.V, "", predicate.Op)
}
```

c. Mirror in `compilePropertyOnAlias`:

```go
if predicate.Property == "type" {
	stringValue, ok := predicate.Value.(StringValue)

	if !ok {
		return "", nil, fmt.Errorf("compile: type predicate with non-StringValue")
	}

	return compileTypeRef(stringValue.V, alias+".", predicate.Op)
}
```

d. In `compileEdgePredicate`, replace the two literal `%s.type = ?`
emits with calls to `compileTypeRef(predicate.EdgeType, edgeAlias+".", OpEQ)`,
appending the returned params before the inner predicate's params.
Update both branches (with and without `predicate.Inner`).

Imports: add `"github.com/germanamz/tusk/internal/typeref"` to
`compile.go`.

- [x] **Step 5: ~~Teach the validator to look up edge types by parsed ref~~ — skipped**

Skipped for this task. With the parser unchanged, the validator will
never see a qualified edge-type string (`:references`,
`markdown:references`), so updating the lookup now would add
untestable code. The validator change lands with the parser/lexer
extension in the follow-up task (see Out of scope above).

- [ ] **Step 6: Run the new tests to verify they pass**

Run:
```
go test ./internal/filter/... -run 'TestCompileNodeTypeRefScopes|TestCompileEdgeTypeRefScopes' -v
```

Expected: PASS.

- [ ] **Step 7: Run the full filter suite**

Run:
```
go test ./internal/filter/... -v
```

Expected: clean. Existing tests that pin `type = ?` literal SQL still
pass (the bare-name branch emits the identical fragment).

- [ ] **Step 8: Run the workspace suite**

Run:
```
go test ./...
```

Expected: clean.

- [ ] **Step 9: `make vet` and `make lint`**

Expected: clean.

- [ ] **Step 10: Commit**

```
git add internal/filter docs/superpowers/plans/2026-05-25-node-edge-source-namespace
git commit -m "feat(filter): compile node/edge type literals as scope-aware typerefs"
```

- [ ] **Step 11: Open the PR**

```
gh pr create --title "feat(filter): compile node/edge type literals as scope-aware typerefs" --body "$(cat <<'EOF'
## Summary
- Top-level `type=X` predicates now parse `X` via `typeref.Parse` and emit scope-aware SQL against `nodes(source, type)`
- Edge-predicate emit sites (`references->X`, `:references->X`, `markdown:references->X`) emit scope-aware SQL against `edges(source, type)`
- Filter validator parses the edge-type qualifier before looking up the bare name in the manifest
- Phase 5, Task 4 of the node/edge source-namespace plan

## Test plan
- [ ] `go test ./internal/filter/... -v` passes (incl. new scope tests for both node-type and edge-type literals)
- [ ] `go test ./...` passes
- [ ] `make vet && make lint` clean
EOF
)"
```

## Done when

- Both new tests (`TestCompileNodeTypeRefScopes`,
  `TestCompileEdgeTypeRefScopes`) pass.
- Workspace suite green.
- PR open.
