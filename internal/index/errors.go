package index

import (
	"errors"
	"fmt"
)

// ErrSchemaIncompatible is the sentinel returned (wrapped) by Open when
// the on-disk index was written by a binary with a different
// SchemaVersion. Callers detect it with errors.Is and recover by
// deleting the on-disk file and rebuilding from source via the
// reindex pipeline.
var ErrSchemaIncompatible = errors.New("index: on-disk schema version is incompatible with this binary")

// SchemaVersionError is the typed wrapper around ErrSchemaIncompatible
// that carries the observed and expected version strings so callers
// can include them in user-facing messages.
type SchemaVersionError struct {
	Observed string
	Expected string
}

// Error implements error.
func (e *SchemaVersionError) Error() string {
	return fmt.Sprintf("%s (observed=%q, expected=%q)", ErrSchemaIncompatible.Error(), e.Observed, e.Expected)
}

// Unwrap lets errors.Is match against ErrSchemaIncompatible.
func (e *SchemaVersionError) Unwrap() error {
	return ErrSchemaIncompatible
}
