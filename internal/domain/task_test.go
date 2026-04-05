package domain

import "testing"

func TestValidateUDAKey_Valid(t *testing.T) {
	valid := []string{"env", "team", "my_key", "key-1", "_private", "A", "camelCase"}
	for _, k := range valid {
		if err := ValidateUDAKey(k); err != nil {
			t.Errorf("ValidateUDAKey(%q) = %v, want nil", k, err)
		}
	}
}

func TestValidateUDAKey_Invalid(t *testing.T) {
	cases := []struct {
		key  string
		desc string
	}{
		{"", "empty"},
		{"1abc", "starts with digit"},
		{"my.key", "contains dot"},
		{"my$key", "contains dollar"},
		{"my[key", "contains bracket"},
		{"has space", "contains space"},
		{"key=val", "contains equals"},
		{"key:val", "contains colon"},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			if err := ValidateUDAKey(tc.key); err == nil {
				t.Errorf("ValidateUDAKey(%q) = nil, want error (%s)", tc.key, tc.desc)
			}
		})
	}
}

func TestValidateUDA_Valid(t *testing.T) {
	cases := []map[string]any{
		{},
		{"env": "prod"},
		{"env": "prod", "team": "backend"},
		{"key_with-dash": "value"},
	}
	for _, uda := range cases {
		if err := ValidateUDA(uda); err != nil {
			t.Errorf("ValidateUDA(%v) = %v, want nil", uda, err)
		}
	}
}

func TestValidateUDA_InvalidKey(t *testing.T) {
	uda := map[string]any{"valid": "ok", "1bad": "val"}
	err := ValidateUDA(uda)
	if err == nil {
		t.Fatal("expected error for invalid key")
	}
}

func TestValidateUDA_NonStringValue(t *testing.T) {
	cases := []struct {
		name string
		uda  map[string]any
	}{
		{"int value", map[string]any{"count": 42}},
		{"bool value", map[string]any{"flag": true}},
		{"nil value", map[string]any{"empty": nil}},
		{"nested object", map[string]any{"nested": map[string]any{"a": "b"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateUDA(tc.uda); err == nil {
				t.Errorf("ValidateUDA(%v) = nil, want error", tc.uda)
			}
		})
	}
}

func TestValidateUDA_Nil(t *testing.T) {
	if err := ValidateUDA(nil); err != nil {
		t.Errorf("ValidateUDA(nil) = %v, want nil", err)
	}
}
