package portability

import (
	"encoding/json"
	"fmt"
	"io"
)

// ImportIssue is a single problem detected during import. Phases 2 and 3
// both produce these — Phase 2 (codec) issues come from malformed JSON
// or schema-version mismatches; Phase 3 (service) issues come from FK,
// taxonomy, cycle, and collision validation.
type ImportIssue struct {
	Kind        string `json:"kind"`         // "schema" | "json" | "taxonomy" | "fk" | "cycle" | "workflow" | "collision"
	EntityKind  string `json:"entity_kind"`  // "task" | "relation" | "project" | … | "" for codec-level issues
	EntityID    string `json:"entity_id"`    // UUID if present, short_id if known, "" if neither
	JSONPointer string `json:"json_pointer"` // e.g. "/tasks/42/parent_id" for codec-level errors; "" otherwise
	Message     string `json:"message"`      // one-line human message
}

// ImportError aggregates every issue detected during import. The
// validation pass collects all issues before returning so the user sees
// the full picture in one round-trip. ImportError satisfies the error
// interface; its Error() returns "import failed: <N> issues".
type ImportError struct {
	Issues []ImportIssue
}

func (e *ImportError) Error() string {
	return fmt.Sprintf("import failed: %d issues", len(e.Issues))
}

// Decode reads a JSON-encoded PortableWorkspace from r. It returns
// (*PortableWorkspace, nil) on success.
//
// Failure modes:
//   - Malformed JSON → returns *ImportError with one Kind="json" issue.
//     Wire shape errors (wrong type for a field, malformed UUID, unknown
//     top-level field) end up here.
//   - schema_version != SchemaVersion → returns *ImportError with one
//     Kind="schema" issue naming both the dump's value and the supported
//     value.
//
// Decode does not validate referential integrity, taxonomy, or cycles;
// those are the PortabilityService's responsibility.
func Decode(r io.Reader) (*PortableWorkspace, error) {
	var ws PortableWorkspace
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&ws); err != nil {
		return nil, &ImportError{Issues: []ImportIssue{{
			Kind:    "json",
			Message: fmt.Sprintf("decode failed: %v", err),
		}}}
	}
	if ws.SchemaVersion != SchemaVersion {
		return nil, &ImportError{Issues: []ImportIssue{{
			Kind: "schema",
			Message: fmt.Sprintf(
				"unsupported schema_version %d (this tusk supports %d); the dump was produced by tusk %s",
				ws.SchemaVersion, SchemaVersion, ws.TuskVersion,
			),
		}}}
	}
	return &ws, nil
}
