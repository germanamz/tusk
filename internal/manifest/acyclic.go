package manifest

import (
	"encoding/json"
	"fmt"
)

// Acyclicity is how strictly an edge type, or a ref property, keeps its graph
// free of cycles: the `acyclic` key (#852). AcyclicError refuses a cycle when
// `tusk edge add` or a node write would close one, and doctor reports a cycle
// written by hand as an error. AcyclicWarning and AcyclicAdvice let writes
// through and only set the level doctor reports at. AcyclicNone allows
// cycles. The values double as doctor severities.
type Acyclicity string

// The `acyclic` levels. true spells AcyclicError and false AcyclicNone, the
// two a manifest written before levels existed can hold.
const (
	AcyclicNone    Acyclicity = ""
	AcyclicError   Acyclicity = "error"
	AcyclicWarning Acyclicity = "warning"
	AcyclicAdvice  Acyclicity = "advice"
)

// acyclicNoneSpelling is how a manifest names AcyclicNone as a string.
const acyclicNoneSpelling = "none"

// RefusesCycles reports whether a write that would close a cycle is refused.
func (level Acyclicity) RefusesCycles() bool {
	return level == AcyclicError
}

// UnmarshalTOML decodes `acyclic = true | false | "error" | "warning" |
// "advice" | "none"`. Any other value fails the manifest load, like a bad
// cardinality: a typo such as "warn" would otherwise turn the check off.
func (level *Acyclicity) UnmarshalTOML(value any) error {
	switch typed := value.(type) {
	case bool:
		*level = AcyclicNone

		if typed {
			*level = AcyclicError
		}

		return nil
	case string:
		switch Acyclicity(typed) {
		case AcyclicError, AcyclicWarning, AcyclicAdvice:
			*level = Acyclicity(typed)

			return nil
		case acyclicNoneSpelling:
			*level = AcyclicNone

			return nil
		}

		return fmt.Errorf(`acyclic %q is not one of true, false, "error", "warning", "advice", "none"`, typed)
	}

	return fmt.Errorf(`acyclic takes true, false, "error", "warning", "advice" or "none", not %v`, value)
}

// MarshalJSON keeps the bool shape for the levels a bool spells, so the JSON
// SchemaFingerprint hashes is unchanged for a manifest written before levels
// existed and upgrading does not force a full reindex.
func (level Acyclicity) MarshalJSON() ([]byte, error) {
	switch level {
	case AcyclicNone:
		return []byte("false"), nil
	case AcyclicError:
		return []byte("true"), nil
	}

	return json.Marshal(string(level))
}

// String names the level as a manifest spells it, for messages.
func (level Acyclicity) String() string {
	if level == AcyclicNone {
		return acyclicNoneSpelling
	}

	return string(level)
}
