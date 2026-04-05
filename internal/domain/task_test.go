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
