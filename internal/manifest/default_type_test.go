package manifest_test

import (
	"strings"
	"testing"
)

func TestDefaultType_AbsentIsEmpty(test *testing.T) {
	loaded := loadTOMLFromString(test, `[workspace]
name = "x"
`)

	if loaded.Workspace.DefaultType != "" {
		test.Errorf("DefaultType = %q, want empty when unset", loaded.Workspace.DefaultType)
	}
}

func TestDefaultType_NamesADeclaredType(test *testing.T) {
	loaded := loadTOMLFromString(test, `[workspace]
name = "x"
default-type = "note"

[node-types.note]
properties = []
`)

	if loaded.Workspace.DefaultType != "note" {
		test.Errorf("DefaultType = %q, want note", loaded.Workspace.DefaultType)
	}
}

func TestDefaultType_RejectsAnUndeclaredType(test *testing.T) {
	_, loadErr := loadTOMLString(test, `[workspace]
name = "x"
default-type = "note"

[node-types.task]
properties = []
`)

	if loadErr == nil {
		test.Fatal("Load accepted an undeclared default-type, want an error")
	}

	for _, want := range []string{"workspace.default-type", `"note"`} {
		if !strings.Contains(loadErr.Error(), want) {
			test.Errorf("error = %q, want it to mention %s", loadErr, want)
		}
	}
}
