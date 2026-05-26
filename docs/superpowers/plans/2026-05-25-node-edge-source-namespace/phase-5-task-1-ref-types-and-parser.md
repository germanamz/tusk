# Phase 5 — Task 1: `typeref` package — Ref types and parser

**Phase:** 5 (Reference-resolution grammar)
**Spec:** § *Naming conventions*, § *Reference resolution in code*

**Goal:** Introduce `internal/typeref` with the `EdgeRef`, `NodeRef`, and `Scope` types plus a `Parse(string)` function implementing the three-form grammar.

## Inherits From

- Phase 4 complete; user/pack reservations coexist.
- No reference-resolution grammar exists yet; callers use raw type strings.

## Files

- **Create:** `internal/typeref/typeref.go`
- **Create:** `internal/typeref/typeref_test.go`

## Steps

- [ ] **Step 1: Write the failing tests**

Create `internal/typeref/typeref_test.go`:

```go
package typeref_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/typeref"
)

func TestParse(test *testing.T) {
	test.Parallel()

	cases := []struct {
		input     string
		wantScope typeref.Scope
		wantSrc   string
		wantType  string
		wantErr   bool
	}{
		{input: "contains", wantScope: typeref.ScopeAny, wantType: "contains"},
		{input: ":contains", wantScope: typeref.ScopeUser, wantType: "contains"},
		{input: "markdown:contains", wantScope: typeref.ScopeSource, wantSrc: "markdown", wantType: "contains"},
		{input: "go:function", wantScope: typeref.ScopeSource, wantSrc: "go", wantType: "function"},
		{input: ":tag", wantScope: typeref.ScopeUser, wantType: "tag"},

		{input: "", wantErr: true},
		{input: ":", wantErr: true},
		{input: "markdown:", wantErr: true},
		{input: ":markdown:contains", wantErr: true},
		{input: "Markdown:contains", wantErr: true}, // uppercase rejected (type names are [a-z0-9-])
		{input: "markdown:Contains", wantErr: true},
		{input: "mark down:contains", wantErr: true}, // whitespace
	}

	for _, tc := range cases {
		tc := tc
		test.Run(tc.input, func(test *testing.T) {
			test.Parallel()

			got, err := typeref.Parse(tc.input)
			if tc.wantErr {
				if err == nil {
					test.Fatalf("Parse(%q) = %+v, want error", tc.input, got)
				}
				return
			}

			if err != nil {
				test.Fatalf("Parse(%q): %v", tc.input, err)
			}
			if got.Scope != tc.wantScope {
				test.Errorf("Scope = %v, want %v", got.Scope, tc.wantScope)
			}
			if got.Source != tc.wantSrc {
				test.Errorf("Source = %q, want %q", got.Source, tc.wantSrc)
			}
			if got.Type != tc.wantType {
				test.Errorf("Type = %q, want %q", got.Type, tc.wantType)
			}
		})
	}
}

func TestRefString(test *testing.T) {
	test.Parallel()

	cases := []struct {
		ref  typeref.Ref
		want string
	}{
		{ref: typeref.Ref{Scope: typeref.ScopeAny, Type: "contains"}, want: "contains"},
		{ref: typeref.Ref{Scope: typeref.ScopeUser, Type: "contains"}, want: ":contains"},
		{ref: typeref.Ref{Scope: typeref.ScopeSource, Source: "markdown", Type: "contains"}, want: "markdown:contains"},
	}

	for _, tc := range cases {
		if got := tc.ref.String(); got != tc.want {
			test.Errorf("Ref{%+v}.String() = %q, want %q", tc.ref, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/typeref/... -v`

Expected: build failure.

- [ ] **Step 3: Implement the package**

Create `internal/typeref/typeref.go`:

```go
// Package typeref parses the canonical <source>:<type> notation
// used throughout tusk for node-type and edge-type references.
// Three forms are accepted:
//
//   <source>:<type>     scoped to one source
//   :<type>             scoped to the user namespace (source = NULL)
//   <type>              shorthand — matches any source (union)
//
// Type and source names are drawn from [a-z0-9-]. The ':' separator
// is unambiguous against that grammar.
package typeref

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Scope is the resolution scope of a parsed reference.
type Scope int

const (
	// ScopeAny matches rows with any source (union).
	ScopeAny Scope = iota
	// ScopeUser matches rows whose source IS NULL.
	ScopeUser
	// ScopeSource matches rows whose source equals Ref.Source.
	ScopeSource
)

// String returns a readable label for debug output.
func (s Scope) String() string {
	switch s {
	case ScopeAny:
		return "any"
	case ScopeUser:
		return "user"
	case ScopeSource:
		return "source"
	default:
		return fmt.Sprintf("scope(%d)", int(s))
	}
}

// Ref is a parsed type reference. Source is populated only when
// Scope == ScopeSource.
type Ref struct {
	Scope  Scope
	Source string
	Type   string
}

// EdgeRef is an alias for Ref used by edge-typed call sites.
type EdgeRef = Ref

// NodeRef is an alias for Ref used by node-typed call sites.
type NodeRef = Ref

// String renders the ref back to its canonical notation. Useful for
// logging and debug formatting; not used in storage.
func (r Ref) String() string {
	switch r.Scope {
	case ScopeAny:
		return r.Type
	case ScopeUser:
		return ":" + r.Type
	case ScopeSource:
		return r.Source + ":" + r.Type
	default:
		return fmt.Sprintf("invalid(%s)", r.Type)
	}
}

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Parse decodes one of the three canonical forms into a Ref.
// Returns a descriptive error for empty input, malformed segments,
// or invalid characters in either segment.
func Parse(input string) (Ref, error) {
	if input == "" {
		return Ref{}, errors.New("typeref: input is empty")
	}

	if !strings.Contains(input, ":") {
		if !namePattern.MatchString(input) {
			return Ref{}, fmt.Errorf("typeref: type name %q must match [a-z0-9][a-z0-9-]*", input)
		}
		return Ref{Scope: ScopeAny, Type: input}, nil
	}

	parts := strings.SplitN(input, ":", 2)
	if len(parts) != 2 {
		return Ref{}, fmt.Errorf("typeref: input %q has too many ':' separators", input)
	}

	source, typeName := parts[0], parts[1]
	if strings.Contains(typeName, ":") {
		return Ref{}, fmt.Errorf("typeref: input %q has too many ':' separators", input)
	}
	if typeName == "" {
		return Ref{}, fmt.Errorf("typeref: input %q missing type name after ':'", input)
	}
	if !namePattern.MatchString(typeName) {
		return Ref{}, fmt.Errorf("typeref: type name %q must match [a-z0-9][a-z0-9-]*", typeName)
	}

	if source == "" {
		return Ref{Scope: ScopeUser, Type: typeName}, nil
	}

	if !namePattern.MatchString(source) {
		return Ref{}, fmt.Errorf("typeref: source name %q must match [a-z0-9][a-z0-9-]*", source)
	}

	return Ref{Scope: ScopeSource, Source: source, Type: typeName}, nil
}

// ParseMany applies Parse to every input. Returns the first error.
// Useful when callers receive a slice (e.g., graph-expansion config).
func ParseMany(inputs []string) ([]Ref, error) {
	out := make([]Ref, 0, len(inputs))
	for _, in := range inputs {
		ref, err := Parse(in)
		if err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, nil
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/typeref/... -v`

Expected: all PASS.

- [ ] **Step 5: Workspace suite**

Run: `go test ./...`

Expected: clean (new package, no consumers yet).

- [ ] **Step 6: `make vet` and `make lint`**

Expected: clean.

- [ ] **Step 7: Commit**

```
git add internal/typeref
git commit -m "feat(typeref): parser for <source>:<type> canonical notation"
```

- [ ] **Step 8: Open the PR**

```
gh pr create --title "feat(typeref): parser for <source>:<type> canonical notation" --body "$(cat <<'EOF'
## Summary
- New package `internal/typeref` exposing `Ref` (aliased as `EdgeRef`/`NodeRef`), `Scope` enum, `Parse`, `ParseMany`, and `String`
- Implements the three-form grammar: bare = union, `:type` = user namespace, `source:type` = scoped
- No consumers yet — Tasks 5.2–5.5 wire callers through it
- Phase 5, Task 1 of the node/edge source-namespace plan

## Test plan
- [ ] `go test ./internal/typeref/... -v` passes (incl. negative cases)
- [ ] `go test ./...` passes
- [ ] `make vet && make lint` clean
EOF
)"
```

## Done when

- Parser tests pass.
- Workspace suite green.
- PR open.
