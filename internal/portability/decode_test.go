package portability

import (
	"errors"
	"strings"
	"testing"
)

func TestDecode_SchemaVersionMismatch(test *testing.T) {
	src := `{"schema_version": 999, "tusk_version": "v9.9.9", "exported_at": "2026-04-26T00:00:00Z", "workflows": null, "projects": null, "players": null, "tags": null, "tasks": null, "relations": null, "annotations": null, "notes": null, "events": null}`

	_, err := Decode(strings.NewReader(src))
	if err == nil {
		test.Fatalf("expected error, got nil")
	}
	var importErr *ImportError
	if !errors.As(err, &importErr) {
		test.Fatalf("expected *ImportError, got %T: %v", err, err)
	}
	if len(importErr.Issues) != 1 {
		test.Fatalf("expected 1 issue, got %d", len(importErr.Issues))
	}
	issue := importErr.Issues[0]
	if issue.Kind != "schema" {
		test.Errorf("expected Kind=schema, got %q", issue.Kind)
	}
	if !strings.Contains(issue.Message, "999") {
		test.Errorf("expected message to mention 999, got %q", issue.Message)
	}
	if !strings.Contains(issue.Message, "1") {
		test.Errorf("expected message to mention supported version 1, got %q", issue.Message)
	}
}

func TestDecode_MalformedJSON(test *testing.T) {
	src := `{not valid json`

	_, err := Decode(strings.NewReader(src))
	if err == nil {
		test.Fatalf("expected error, got nil")
	}
	var importErr *ImportError
	if !errors.As(err, &importErr) {
		test.Fatalf("expected *ImportError, got %T: %v", err, err)
	}
	if len(importErr.Issues) != 1 {
		test.Fatalf("expected 1 issue, got %d", len(importErr.Issues))
	}
	if importErr.Issues[0].Kind != "json" {
		test.Errorf("expected Kind=json, got %q", importErr.Issues[0].Kind)
	}
}

func TestDecode_UnknownTopLevelField(test *testing.T) {
	// `foo` is not part of PortableWorkspace; DisallowUnknownFields
	// must reject the dump immediately.
	src := `{"schema_version": 1, "tusk_version": "v0.13.0", "exported_at": "2026-04-26T00:00:00Z", "foo": 1}`

	_, err := Decode(strings.NewReader(src))
	if err == nil {
		test.Fatalf("expected error, got nil")
	}
	var importErr *ImportError
	if !errors.As(err, &importErr) {
		test.Fatalf("expected *ImportError, got %T: %v", err, err)
	}
	if importErr.Issues[0].Kind != "json" {
		test.Errorf("expected Kind=json (DisallowUnknownFields surfaces as a JSON decode error), got %q", importErr.Issues[0].Kind)
	}
	if !strings.Contains(importErr.Issues[0].Message, "foo") {
		test.Errorf("expected message to name the unknown field 'foo', got %q", importErr.Issues[0].Message)
	}
}

func TestDecode_EmptyWorkspace(test *testing.T) {
	src := `{"schema_version": 1, "tusk_version": "v0.13.0", "exported_at": "2026-04-26T00:00:00Z"}`

	got, err := Decode(strings.NewReader(src))

	if err != nil {
		test.Fatalf("Decode: %v", err)
	}

	if got.SchemaVersion != SchemaVersion {
		test.Errorf("SchemaVersion mismatch: got %d", got.SchemaVersion)
	}
	if got.TuskVersion != "v0.13.0" {
		test.Errorf("TuskVersion mismatch: got %q", got.TuskVersion)
	}
	if len(got.Tasks) != 0 || len(got.Projects) != 0 || len(got.Workflows) != 0 ||
		len(got.Players) != 0 || len(got.Tags) != 0 || len(got.Relations) != 0 ||
		len(got.Annotations) != 0 || len(got.Notes) != 0 || len(got.Events) != 0 {
		test.Errorf("expected all entity lists empty, got %+v", got)
	}
}

func TestImportError_ErrorMessage(test *testing.T) {
	importErrVal := &ImportError{Issues: []ImportIssue{
		{Kind: "schema", Message: "x"},
		{Kind: "json", Message: "y"},
	}}
	want := "import failed: 2 issues"
	if got := importErrVal.Error(); got != want {
		test.Errorf("Error() = %q, want %q", got, want)
	}
}
