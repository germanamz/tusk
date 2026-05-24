package manifest

import (
	"fmt"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// syntheticContextRecentAlias is the alias name assigned to an inline
// [context.recent] block. Prefixed with double-underscores so it cannot
// collide with a user-declared alias name.
const syntheticContextRecentAlias = "__context_recent__"

// Context is the parsed representation of the [context] manifest section.
// Populated by Load (decode-only) and refined by ValidateContext (resolves
// alias references, parses inline [context.recent] blocks). Pinned IDs are
// not validated here — they reference runtime index state and are surfaced
// through doctor.
type Context struct {
	// Pinned lists node IDs (workspace-relative paths without extension)
	// the agent should always have in its warm context.
	Pinned []string

	// Recent is the resolved alias that produces the "recent activity"
	// section of a context digest. nil when neither `recent = "..."` nor
	// [context.recent] is declared, or when validation rejected both.
	Recent *Alias

	// Include lists named aliases (must resolve against Manifest.Aliases)
	// whose results are folded into the digest under their alias name.
	Include []string
}

// ContextError is a per-context validation failure surfaced through doctor.
// Context errors never fail manifest Load.
type ContextError struct {
	Message string
}

// contextTOML mirrors the on-disk [context] block at decode time. Recent is
// held as a toml.Primitive so ValidateContext can discriminate between the
// reference form (recent = "alias-name") and the inline-block form
// ([context.recent] sub-table). When both forms are declared, the body is
// pre-stripped (the inline block is removed) and a ContextError is recorded;
// the reference form takes precedence so the decoded Primitive is a string.
type contextTOML struct {
	Pinned  []string       `toml:"pinned"`
	Recent  toml.Primitive `toml:"recent"`
	Include []string       `toml:"include"`
}

// decodeContext performs a secondary decode of the [context] table and
// stages an unvalidated loaded.Context for ValidateContext to refine.
//
// The body passed here MUST already have any colliding [context.recent]
// block stripped (Load handles that pre-strip and records a ContextError);
// decodeContext itself does not retry or recover from a TOML parse error.
//
// Returns an error only when the [context] table cannot be parsed at all.
// Per-context validation problems are deferred to ValidateContext.
func decodeContext(body string, loaded *Manifest) error {
	var wrapper struct {
		Context contextTOML `toml:"context"`
	}

	meta, decodeErr := toml.Decode(body, &wrapper)

	if decodeErr != nil {
		return decodeErr
	}

	// Detect whether [context] was declared at all. IsDefined checks the
	// MetaData we just produced; a missing table means Context stays nil.
	if !meta.IsDefined("context") {
		return nil
	}

	loaded.Context = &Context{
		Pinned:  append([]string(nil), wrapper.Context.Pinned...),
		Include: append([]string(nil), wrapper.Context.Include...),
	}

	loaded.contextRecentPrimitive = wrapper.Context.Recent
	loaded.contextRecentDefined = meta.IsDefined("context", "recent")
	loaded.contextRecentMeta = &meta

	return nil
}

// ValidateContext finalises loaded.Context after ValidateAliases has run.
// Resolves a string recent into an alias reference; parses a [context.recent]
// inline block as a synthetic alias; verifies each include name resolves to
// a known alias.
//
// Pinned IDs are NOT validated here — they depend on runtime index state and
// are checked at doctor time.
//
// ValidateContext is a no-op when loaded is nil or loaded.Context is nil.
func ValidateContext(loaded *Manifest, introspect VerbIntrospector) {
	if loaded == nil || loaded.Context == nil {
		return
	}

	ctx := loaded.Context

	if loaded.contextRecentDefined {
		resolved, recentErr := resolveContextRecent(loaded, introspect)

		if recentErr != nil {
			loaded.ContextErrors = append(loaded.ContextErrors, ContextError{Message: recentErr.Error()})
		} else {
			ctx.Recent = resolved
		}
	}

	if len(ctx.Include) > 0 {
		validInclude := make([]string, 0, len(ctx.Include))

		for _, name := range ctx.Include {
			if _, ok := loaded.Aliases[name]; ok {
				validInclude = append(validInclude, name)

				continue
			}

			loaded.ContextErrors = append(loaded.ContextErrors, ContextError{
				Message: fmt.Sprintf("context.include: alias %q is not declared in tusk.toml", name),
			})
		}

		ctx.Include = validInclude
	}

	// Drop the raw pieces; they are not needed past validation.
	loaded.contextRecentPrimitive = toml.Primitive{}
	loaded.contextRecentDefined = false
	loaded.contextRecentMeta = nil
}

// resolveContextRecent returns the bound *Alias for the recent field. It
// tries the string-reference form first (cheap, common); on failure it
// decodes the inline-alias shape, validates it through the standard alias
// rules (read-only verb, typed args), and returns the synthetic alias.
func resolveContextRecent(loaded *Manifest, introspect VerbIntrospector) (*Alias, error) {
	if loaded.contextRecentMeta == nil {
		return nil, fmt.Errorf("context: internal: missing toml metadata")
	}

	meta := loaded.contextRecentMeta

	// Try string form: recent = "alias-name".
	var asString string

	if strErr := meta.PrimitiveDecode(loaded.contextRecentPrimitive, &asString); strErr == nil {
		trimmed := strings.TrimSpace(asString)

		if trimmed == "" {
			return nil, fmt.Errorf("context.recent: alias name is empty")
		}

		alias, ok := loaded.Aliases[trimmed]

		if !ok {
			return nil, fmt.Errorf("context.recent: alias %q is not declared in tusk.toml", trimmed)
		}

		bound := alias

		return &bound, nil
	}

	// Inline form: [context.recent] is a sub-table. Decode through the
	// aliasTOML shape so the same validator that handles top-level aliases
	// can run.
	var inline aliasTOML

	if tableErr := meta.PrimitiveDecode(loaded.contextRecentPrimitive, &inline); tableErr != nil {
		return nil, fmt.Errorf("context.recent: cannot parse as string or inline alias: %v", tableErr)
	}

	synthetic := Alias{
		Name:        syntheticContextRecentAlias,
		Command:     inline.Command,
		Description: inline.Description,
		Args:        inline.Args,
	}

	verb, verbErr := resolveAliasVerb(synthetic.Command)

	if verbErr != nil {
		return nil, fmt.Errorf("context.recent: %v", verbErr)
	}

	synthetic.Verb = verb

	if argErrs := validateAliasArgs(synthetic, verb, introspect); len(argErrs) > 0 {
		return nil, fmt.Errorf("context.recent: %s", argErrs[0])
	}

	return &synthetic, nil
}

// SortedContextErrors returns loaded.ContextErrors sorted by Message for
// stable rendering. Returns nil for nil or empty inputs.
func SortedContextErrors(errs []ContextError) []ContextError {
	if len(errs) == 0 {
		return nil
	}

	out := append([]ContextError(nil), errs...)
	sort.Slice(out, func(left, right int) bool {
		return out[left].Message < out[right].Message
	})

	return out
}

// bodyDeclaresContextReferenceRecent reports whether the manifest body
// contains a `recent = ...` assignment directly under the [context] section
// (not under [context.recent] or any other table).
func bodyDeclaresContextReferenceRecent(body string) bool {
	inContextRoot := false

	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(trimmed, "#") {
			continue
		}

		if trimmed == "[context]" {
			inContextRoot = true

			continue
		}

		if strings.HasPrefix(trimmed, "[") {
			inContextRoot = false

			continue
		}

		if !inContextRoot {
			continue
		}

		eq := strings.IndexByte(trimmed, '=')

		if eq <= 0 {
			continue
		}

		key := strings.TrimSpace(trimmed[:eq])

		if key == "recent" {
			return true
		}
	}

	return false
}

// bodyDeclaresContextInlineRecent reports whether the manifest body declares
// a [context.recent] block.
func bodyDeclaresContextInlineRecent(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)

		if trimmed == "[context.recent]" {
			return true
		}
	}

	return false
}

// stripContextRecentBlock removes the [context.recent] block (its header and
// every line until the next table header or EOF) from body and returns the
// result. Used to recover from the both-forms-set collision so the rest of
// the manifest still decodes.
func stripContextRecentBlock(body string) string {
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	skip := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "[context.recent]" {
			skip = true

			continue
		}

		if skip && strings.HasPrefix(trimmed, "[") {
			skip = false
		}

		if skip {
			continue
		}

		out = append(out, line)
	}

	return strings.Join(out, "\n")
}
