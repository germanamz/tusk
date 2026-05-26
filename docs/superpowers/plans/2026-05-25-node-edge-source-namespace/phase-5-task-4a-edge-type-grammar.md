# Phase 5 — Task 4a: Lexer/parser/validator accept qualified edge-type identifiers

**Phase:** 5 (Reference-resolution grammar)
**Spec:** § *Naming conventions*

**Goal:** Filter expressions accept qualified edge-type identifiers
(`:references->X`, `markdown:references->X`) in addition to the
existing bare form (`references->X`). The compiler emits the same
scope-aware `edges(source, type)` SQL pattern that Task 5.4
established for `nodes(source, type)`. The validator looks up the
*bare* type name in the manifest after parsing off the qualifier.

This closes the "same grammar across all surfaces" promise from the
phase-5 overview that Task 5.4 only partially delivered (node-type
literal values worked because they pass through the bareword-value
lexer path which already accepts `:`; edge-type identifiers don't,
because identifier-position lexing rejects `:`).

## Inherits From

After Task 5.4:
- `typeref.Parse` / `ParseMany` available.
- `internal/filter/compile.go` has `compileTypeRef(raw, columnPrefix, op)`
  emitting `[<prefix>source IS NULL AND ]<prefix>type <op> ?` and
  `[<prefix>source = ? AND ]<prefix>type <op> ?` per scope.
- Node-type literal values (`type=:section`) work end-to-end.

## Files

- **Modify:** `internal/filter/lexer.go` — extend `Next()` to emit a
  single identifier token for `<source>:<type>` and `:<type>` when the
  pattern is unambiguous in identifier position. *Or* leave the lexer
  alone and have the parser stitch tokens — pick whichever is less
  intrusive (see Step 4).
- **Modify:** `internal/filter/parser.go` — accept qualified edge-type
  identifiers in `parsePredicate` and `parseEdgePredicate`. Pass the
  full canonical string (`source:type` or `:type`) into
  `EdgePredicate.EdgeType`.
- **Modify:** `internal/filter/compile.go` — `compileEdgePredicate`
  calls `compileTypeRef(predicate.EdgeType, edgeAlias+".", OpEQ)` and
  prepends the returned params before any inner-predicate params.
- **Modify:** `internal/filter/validate.go` — parse
  `EdgePredicate.EdgeType` via `typeref.Parse`, look up `ref.Type`
  against the manifest; report `not declared` with the bare name in
  the message.
- **Modify:** `internal/filter/compile_test.go` — add
  `TestCompileEdgeTypeRefScopes` covering bare, `:type`, and
  `source:type` forms.
- **Modify:** `internal/filter/parser_test.go` — add coverage for
  parser acceptance of qualified edge-type identifiers (parse
  succeeds and `EdgePredicate.EdgeType` carries the canonical form).
- **Modify:** `internal/filter/validate_test.go` — add coverage for
  validator behavior on qualified edge-type names (validates against
  bare `ref.Type` in the manifest).

## Steps

- [ ] **Step 1: Audit current parser/lexer behavior**

Run:
```
go test ./internal/filter/... -run TestParser -v
```

Confirm the current grammar:
- `references->X` parses (bare ident → arrow).
- `:references->X` fails — first token is `TokenColon`, not `TokenIdent`.
- `markdown:references->X` fails — after `markdown` is consumed as the
  edge type, the parser expects `->`/`<-` but sees `:`.

This step is just observation; no code change.

- [ ] **Step 2: Write failing tests**

In `internal/filter/compile_test.go`:

```go
func TestCompileEdgeTypeRefScopes(test *testing.T) {
	test.Parallel()

	loaded := manifest.Manifest{
		EdgeTypes: map[string]manifest.EdgeType{
			"references": {Cardinality: manifest.CardinalityOneToMany},
		},
	}

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

Add a validator test in `internal/filter/validate_test.go`:

```go
func TestValidateQualifiedEdgeType(test *testing.T) {
	test.Parallel()

	loaded := manifest.Manifest{
		EdgeTypes: map[string]manifest.EdgeType{
			"references": {Cardinality: manifest.CardinalityOneToMany},
		},
	}

	// All three forms must validate clean against the bare manifest key.
	for _, input := range []string{"references->id=x", ":references->id=x", "markdown:references->id=x"} {
		expr, parseErrs := filter.NewParser(input).Parse()

		if len(parseErrs) > 0 {
			test.Fatalf("parse %q: %v", input, parseErrs[0])
		}

		if errs := filter.Validate(expr, loaded); len(errs) > 0 {
			test.Errorf("validate %q: %v", input, errs[0])
		}
	}

	// An unqualified unknown should still surface "not declared", with
	// the bare-name hint engine intact.
	expr, _ := filter.NewParser("referenced->id=x").Parse()
	errs := filter.Validate(expr, loaded)

	if len(errs) == 0 || !strings.Contains(errs[0].Message, "not declared") {
		test.Errorf("expected not-declared error for bare unknown edge type, got %v", errs)
	}
}
```

- [ ] **Step 3: Run the new tests to verify they fail**

Run:
```
go test ./internal/filter/... -run 'TestCompileEdgeTypeRefScopes|TestValidateQualifiedEdgeType' -v
```

Expected: FAIL — parser rejects the qualified inputs before validate
or compile gets a chance.

- [ ] **Step 4: Choose the grammar approach and implement**

Two viable shapes; the implementer picks one based on which is less
disruptive to existing parser tests:

**Approach A — parser stitches tokens (recommended).** Leave the
lexer alone. In `parsePredicate`:

1. If `peek().Kind == TokenColon && peekN(1).Kind == TokenIdent &&
   peekN(2).Kind in (TokenArrowOut, TokenArrowIn)`: consume the
   colon, the type ident, and proceed into `parseEdgePredicate` with
   the canonical string `":" + typeIdent.Value` stamped onto the
   resulting `EdgePredicate.EdgeType`.

2. If `peek().Kind == TokenIdent && peekN(1).Kind == TokenColon &&
   peekN(2).Kind == TokenIdent && peekN(3).Kind in (TokenArrowOut,
   TokenArrowIn)`: consume the source ident, the colon, the type
   ident, and proceed into `parseEdgePredicate` with
   `sourceIdent.Value + ":" + typeIdent.Value` stamped on
   `EdgePredicate.EdgeType`.

   Be careful not to swallow `tree:alias=X` traversal-shortcut
   syntax — `tree`/`parent`/`root` already get routed in
   `parsePredicate` (line 116) before the colon-stitch path; add the
   stitch check **after** the keyword switch.

3. Extract the edge-type token-stitching into a small helper since
   both `parsePredicate` and the inner-recursion path in
   `parseEdgePredicate` (for `A->B->C` chains) need it.

**Approach B — lexer recognizes qualified idents.** Add a lexer mode
or post-process step that, in identifier position, recognizes
`[ident]:ident` and `:ident` as a single `TokenIdent` whose `Value`
holds the canonical `source:type` or `:type` string. This is cleaner
parser-side but more intrusive on the lexer and risks unrelated
breakage in property-name lexing.

Pick Approach A unless evidence in Step 1 suggests otherwise.

- [ ] **Step 5: Wire the compiler**

In `internal/filter/compile.go`, `compileEdgePredicate`:

```go
edgeClause, edgeParams, edgeErr := compileTypeRef(predicate.EdgeType, edgeAlias+".", OpEQ)

if edgeErr != nil {
	return "", nil, edgeErr
}

if predicate.Inner == nil {
	sql := fmt.Sprintf("EXISTS (SELECT 1 FROM edges %s WHERE %s AND %s)", edgeAlias, sourceColumn, edgeClause)

	return sql, edgeParams, nil
}

innerSQL, innerParams, innerErr := compileInnerOnAlias(predicate.Inner, nodeAlias, depth)

if innerErr != nil {
	return "", nil, innerErr
}

sql := fmt.Sprintf("EXISTS (SELECT 1 FROM edges %s JOIN nodes %s ON %s WHERE %s AND %s AND %s)",
	edgeAlias, nodeAlias, joinColumn, sourceColumn, edgeClause, innerSQL)

params := append(edgeParams, innerParams...)

return sql, params, nil
```

- [ ] **Step 6: Update the validator**

In `internal/filter/validate.go`, change the `*EdgePredicate` branch:

```go
case *EdgePredicate:
	ref, parseErr := typeref.Parse(typed.EdgeType)

	if parseErr != nil {
		collector.errors = append(collector.errors, ValidationError{
			Pos:     typed.Pos,
			Message: fmt.Sprintf("edge type %q is not a valid type reference", typed.EdgeType),
		})
	} else if _, declared := collector.manifest.EdgeTypes[ref.Type]; !declared {
		collector.errors = append(collector.errors, ValidationError{
			Pos:     typed.Pos,
			Message: fmt.Sprintf("edge type %q not declared in manifest", ref.Type),
			Hint:    suggestEdgeType(ref.Type, collector.manifest.EdgeTypes),
		})
	}

	if typed.Inner != nil {
		collector.walk(typed.Inner)
	}
```

Add `"github.com/germanamz/tusk/internal/typeref"` to the imports.

- [ ] **Step 7: Run the new tests**

Expected: PASS.

- [ ] **Step 8: Run the full filter suite**

```
go test ./internal/filter/... -v
```

Expected: clean. Existing tests asserting `e0.type = ?` literally
still pass (bare-form emit shape is unchanged).

- [ ] **Step 9: Run the workspace suite**

```
go test ./...
```

Expected: clean.

- [ ] **Step 10: `make vet` and `make lint`**

Expected: clean.

- [ ] **Step 11: Commit**

```
git add internal/filter
git commit -m "feat(filter): accept qualified edge-type identifiers in filter grammar"
```

- [ ] **Step 12: Open the PR**

```
gh pr create --title "feat(filter): accept qualified edge-type identifiers in filter grammar" --body "$(cat <<'EOF'
## Summary
- Filter parser accepts `:references->X` and `markdown:references->X` in addition to bare `references->X`
- Compiler emits the same scope-aware SQL pattern Task 5.4 established for node types, now against `edges(source, type)`
- Validator parses the qualifier before looking up the bare type name in the manifest
- Phase 5, Task 4a of the node/edge source-namespace plan

## Test plan
- [ ] `go test ./internal/filter/... -v` passes (incl. new edge-type scope and validator tests)
- [ ] `go test ./...` passes
- [ ] `make vet && make lint` clean
EOF
)"
```

## Done when

- All three forms of edge-type identifier parse, validate, and compile
  to scope-aware SQL.
- Workspace suite green.
- PR open.
