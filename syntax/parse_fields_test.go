package syntax

import "testing"

func TestParseFieldsCarriesBareAndModifiedFields(test *testing.T) {
	fs, errs := ParseFields("status=active +status=review -status=done +urgent -blocked title=\"Hello world\"")
	if len(errs) > 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	if len(fs.Fields) != 4 {
		test.Fatalf("expected 4 fields, got %d: %+v", len(fs.Fields), fs.Fields)
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
	for index, wantItem := range want {
		got := fs.Fields[index]
		if got.Key != wantItem.key || got.Value != wantItem.value || got.Modifier != wantItem.modifier {
			test.Errorf("field[%d] = %+v, want %+v", index, got, wantItem)
		}
	}

	if len(fs.Tags) != 2 {
		test.Fatalf("expected 2 tags, got %d", len(fs.Tags))
	}
	if fs.Tags[0].Name != "urgent" || fs.Tags[0].Exclude {
		test.Errorf("tag[0] = %+v", fs.Tags[0])
	}
	if fs.Tags[1].Name != "blocked" || !fs.Tags[1].Exclude {
		test.Errorf("tag[1] = %+v", fs.Tags[1])
	}
}

func TestParseFieldsDoesNotValidateDomain(test *testing.T) {
	fs, errs := ParseFields("bogus=yes")
	if len(errs) > 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	if len(fs.Fields) != 1 || fs.Fields[0].Key != "bogus" || fs.Fields[0].Value != "yes" {
		test.Fatalf("unexpected fields: %+v", fs.Fields)
	}
}
