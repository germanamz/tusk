package manifest

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/germanamz/tusk/internal/cliregistry"
)

// Alias is a manifest-declared, reusable, read-only verb invocation.
// Aliases are looked up by name and dispatched through aliasdispatch.
type Alias struct {
	// Name is the alias name as declared in [alias.<name>].
	Name string

	// Command is the canonical CLI sub-command path the alias targets
	// (e.g. "node list", "query"). Must resolve to a ReadOnly VerbSpec.
	Command string

	// Description is an optional human-readable summary.
	Description string

	// Args carries the raw TOML-typed values declared under [alias.<name>.args].
	// Keys must match either a positional name declared by the verb's VerbSpec
	// or a flag name on the verb's Cobra command. Values must match the
	// destination type (string/int/bool/[]string).
	Args map[string]any

	// Verb is the resolved VerbSpec for Command. Set by ValidateAliases.
	Verb cliregistry.VerbSpec

	// argsType is the TOML type args was declared with when it is not a
	// table ("Array", "String", ...). The decoder drops such a value without
	// an error, leaving Args nil, so validation rejects the alias instead of
	// letting it run with no arguments (#847).
	argsType string
}

// Invocation renders the alias as its command followed by its args in key
// order, e.g. `query filter="type=ticket AND status!=done" take=20`. A string
// is quoted when it is empty or holds a space, quote, '=' or ','; a list
// joins its items with ','.
func (alias Alias) Invocation() string {
	keys := make([]string, 0, len(alias.Args))

	for key := range alias.Args {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	parts := append(make([]string, 0, len(keys)+1), alias.Command)

	for _, key := range keys {
		parts = append(parts, key+"="+invocationValue(alias.Args[key]))
	}

	return strings.Join(parts, " ")
}

// invocationValue renders one args value for Invocation.
func invocationValue(value any) string {
	switch typed := value.(type) {
	case string:
		if typed == "" || strings.ContainsAny(typed, " \t\"=,") {
			return strconv.Quote(typed)
		}

		return typed

	case []any:
		items := make([]string, 0, len(typed))

		for _, item := range typed {
			items = append(items, invocationValue(item))
		}

		return strings.Join(items, ",")
	}

	return fmt.Sprint(value)
}

// AliasError is a per-alias validation failure surfaced through doctor.
// Name is empty for parse-level errors (e.g. malformed [alias.<name>] block).
type AliasError struct {
	Name    string
	Message string
}

// FlagSpec describes one flag declared on a Cobra command, in the form
// ValidateAliases needs to type-check alias arguments.
type FlagSpec struct {
	Name string
	// Kind is one of "string", "int", "bool", "stringSlice".
	Kind string
}

// VerbIntrospector returns the flag specs registered for the given verb.
// ValidateAliases calls this once per verb referenced by an alias.
// Production code passes a closure that walks the Cobra root; tests pass a
// hand-built map.
type VerbIntrospector func(verb string) (flags []FlagSpec, ok bool)

// aliasTOML mirrors the on-disk [alias.<name>] block. ValidateAliases
// consumes the decoded form and never sees this struct.
type aliasTOML struct {
	Command     string         `toml:"command"`
	Description string         `toml:"description"`
	Args        map[string]any `toml:"args"`
}

// ValidateAliases validates every alias declared in loaded against the
// cliregistry (read-only vs. write) and the verb's flag/positional set
// (via introspect). Results are stamped onto loaded.Aliases (the valid set)
// and loaded.AliasErrors (the rejected set with reasons). Never returns an
// error: bad aliases are surfaced through doctor, not by failing engine
// startup.
//
// Validation rules per alias:
//  1. command must resolve to a verb in cliregistry.ReadOnly or
//     cliregistry.Write. Otherwise: "unknown verb %q".
//  2. If the verb is in cliregistry.Write: "alias targets write verb %q,
//     which is not permitted".
//  3. Each args key must match a registered positional name or a flag name
//     reported by introspect. Otherwise: "arg %q is not a flag or
//     positional on %q".
//  4. Each args value's TOML type must match the destination type.
//     Otherwise: "arg %q has type %T, want %s for flag/positional %q".
//
// ValidateAliases ignores nil loaded or empty alias sets and is a no-op.
func ValidateAliases(loaded *Manifest, introspect VerbIntrospector) {
	if loaded == nil {
		return
	}

	if len(loaded.Aliases) == 0 {
		return
	}

	// Stable iteration order so AliasErrors is deterministic.
	names := make([]string, 0, len(loaded.Aliases))

	for name := range loaded.Aliases {
		names = append(names, name)
	}

	sort.Strings(names)

	validated := make(map[string]Alias, len(loaded.Aliases))

	var aliasErrors []AliasError

	for _, name := range names {
		alias := loaded.Aliases[name]

		verb, verbErr := resolveAliasVerb(alias.Command)

		if verbErr != nil {
			aliasErrors = append(aliasErrors, AliasError{Name: name, Message: verbErr.Error()})

			continue
		}

		alias.Verb = verb

		if argErrs := validateAliasArgs(alias, verb, introspect); len(argErrs) > 0 {
			for _, message := range argErrs {
				aliasErrors = append(aliasErrors, AliasError{Name: name, Message: message})
			}

			continue
		}

		validated[name] = alias
	}

	loaded.Aliases = validated
	loaded.AliasErrors = aliasErrors
}

// resolveAliasVerb returns the ReadOnly VerbSpec for command, or an error
// when the command names an unknown verb or a write verb.
func resolveAliasVerb(command string) (cliregistry.VerbSpec, error) {
	if command == "" {
		return cliregistry.VerbSpec{}, fmt.Errorf("command is empty")
	}

	if spec, ok := cliregistry.ReadOnly[command]; ok {
		return spec, nil
	}

	if _, ok := cliregistry.Write[command]; ok {
		return cliregistry.VerbSpec{}, fmt.Errorf("alias targets write verb %q, which is not permitted", command)
	}

	return cliregistry.VerbSpec{}, fmt.Errorf("unknown verb %q", command)
}

// validateAliasArgs returns one message per arg that does not match a
// declared positional or flag, or whose TOML-decoded type disagrees with
// the destination type.
func validateAliasArgs(alias Alias, verb cliregistry.VerbSpec, introspect VerbIntrospector) []string {
	if alias.argsType != "" {
		return []string{argsNotTableMessage(alias, verb)}
	}

	if len(alias.Args) == 0 {
		return nil
	}

	flags, flagsOK := []FlagSpec{}, false

	if introspect != nil {
		flags, flagsOK = introspect(verb.Verb)
	}

	positionalSet := make(map[string]struct{}, len(verb.Positionals))

	for _, name := range verb.Positionals {
		positionalSet[name] = struct{}{}
	}

	flagByName := make(map[string]FlagSpec, len(flags))

	for _, spec := range flags {
		flagByName[spec.Name] = spec
	}

	// Stable iteration so the error list is deterministic.
	argNames := make([]string, 0, len(alias.Args))

	for name := range alias.Args {
		argNames = append(argNames, name)
	}

	sort.Strings(argNames)

	var messages []string

	for _, name := range argNames {
		value := alias.Args[name]

		if _, isPositional := positionalSet[name]; isPositional {
			if typeMatch, wantKind := aliasValueMatchesKind(value, "string"); !typeMatch {
				messages = append(messages,
					fmt.Sprintf("arg %q has type %T, want %s for positional %q on %q",
						name, value, wantKind, name, verb.Verb))
			}

			continue
		}

		flagSpec, isFlag := flagByName[name]

		if !isFlag {
			if !flagsOK {
				messages = append(messages,
					fmt.Sprintf("arg %q is not a flag or positional on %q (no flag introspection available)", name, verb.Verb))

				continue
			}

			messages = append(messages,
				fmt.Sprintf("arg %q is not a flag or positional on %q", name, verb.Verb))

			continue
		}

		if typeMatch, wantKind := aliasValueMatchesKind(value, flagSpec.Kind); !typeMatch {
			messages = append(messages,
				fmt.Sprintf("arg %q has type %T, want %s for flag %q on %q",
					name, value, wantKind, name, verb.Verb))
		}
	}

	return messages
}

// tomlTypeNames spells the TOML types MetaData.Type reports the way an error
// message reads them. Types missing here fall back to their lowercased name.
var tomlTypeNames = map[string]string{
	"Array":     "an array",
	"ArrayHash": "an array of tables",
	"Bool":      "a boolean",
	"Float":     "a float",
	"Integer":   "an integer",
	"String":    "a string",
}

// argsTypeOf returns the TOML type of the args value at keyPath when it is
// declared as anything but a table, or "" when it is a table or absent.
func argsTypeOf(meta toml.MetaData, keyPath ...string) string {
	if !meta.IsDefined(keyPath...) {
		return ""
	}

	if kind := meta.Type(keyPath...); kind != "Hash" {
		return kind
	}

	return ""
}

// argsNotTableMessage says that args must be a table and shows the header to
// write, with the verb's first positional as the example key.
func argsNotTableMessage(alias Alias, verb cliregistry.VerbSpec) string {
	kind, ok := tomlTypeNames[alias.argsType]

	if !ok {
		kind = "a " + strings.ToLower(alias.argsType)
	}

	header := "alias." + alias.Name + ".args"

	if alias.Name == syntheticContextRecentAlias {
		header = "context.recent.args"
	}

	example := "one key per flag"

	if len(verb.Positionals) > 0 {
		example = fmt.Sprintf("one key per flag or positional, e.g. %s = \"...\"", verb.Positionals[0])
	}

	return fmt.Sprintf("args is %s, but it must be a table: write [%s] with %s", kind, header, example)
}

// aliasValueMatchesKind reports whether the TOML-decoded value is assignable
// to a destination of the given kind. The string form returned alongside the
// match boolean is the human-readable destination kind ValidateAliases
// surfaces in error messages.
//
// TOML decoding rules followed:
//   - int64 → matches "int"; float64 with an exact-integer value also matches
//   - float64 / int64 / int → matches "float"
//   - string → matches "string"
//   - bool → matches "bool"
//   - []any with all-string elements → matches "stringSlice"
func aliasValueMatchesKind(value any, kind string) (bool, string) {
	switch kind {
	case "string":
		_, ok := value.(string)

		return ok, "string"

	case "int":
		switch typed := value.(type) {
		case int:
			return true, "int"
		case int64:
			return true, "int"
		case float64:
			return typed == float64(int64(typed)), "int"
		}

		return false, "int"

	case "float":
		switch value.(type) {
		case float64, float32, int, int64:
			return true, "float"
		}

		return false, "float"

	case "bool":
		_, ok := value.(bool)

		return ok, "bool"

	case "stringSlice":
		raw, ok := value.([]any)

		if !ok {
			// A bare string is also acceptable for stringSlice flags (single value).
			if _, isString := value.(string); isString {
				return true, "stringSlice"
			}

			return false, "stringSlice"
		}

		for _, item := range raw {
			if _, isString := item.(string); !isString {
				return false, "stringSlice"
			}
		}

		return true, "stringSlice"
	}

	// Unknown destination kind — accept and let the dispatcher fail loudly.
	return true, kind
}
