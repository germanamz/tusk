package syntax

import "testing"

func TestParseFieldsCarriesBareAndModifiedFields(t *testing.T) {
	fs, errs := ParseFields("status=active +status=review -status=done +urgent -blocked title=\"Hello world\"")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(fs.Fields) != 4 {
		t.Fatalf("expected 4 fields, got %d: %+v", len(fs.Fields), fs.Fields)
	}

	want := []struct {
		key      string
		value    string
		modifier byte
	}{
		{"status", "active", 0},
		{"status", "review", '+'},
		{"status", "done", '-'},
		{"title", "Hello world", 0},
	}
	for i, w := range want {
		got := fs.Fields[i]
		if got.Key != w.key || got.Value != w.value || got.Modifier != w.modifier {
			t.Errorf("field[%d] = %+v, want %+v", i, got, w)
		}
	}

	if len(fs.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(fs.Tags))
	}
	if fs.Tags[0].Name != "urgent" || fs.Tags[0].Exclude {
		t.Errorf("tag[0] = %+v", fs.Tags[0])
	}
	if fs.Tags[1].Name != "blocked" || !fs.Tags[1].Exclude {
		t.Errorf("tag[1] = %+v", fs.Tags[1])
	}
}

func TestParseFieldsDoesNotValidateDomain(t *testing.T) {
	fs, errs := ParseFields("bogus=yes")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(fs.Fields) != 1 || fs.Fields[0].Key != "bogus" || fs.Fields[0].Value != "yes" {
		t.Fatalf("unexpected fields: %+v", fs.Fields)
	}
}
