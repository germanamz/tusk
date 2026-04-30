package domain

import "testing"

func TestValidateUDAKey_Valid(test *testing.T) {
	valid := []string{"env", "team", "my_key", "key-1", "_private", "A", "camelCase"}
	for _, key := range valid {
		if err := ValidateUDAKey(key); err != nil {
			test.Errorf("ValidateUDAKey(%q) = %v, want nil", key, err)
		}
	}
}

func TestValidateUDAKey_Invalid(test *testing.T) {
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
	for _, testCase := range cases {
		test.Run(testCase.desc, func(test *testing.T) {
			if err := ValidateUDAKey(testCase.key); err == nil {
				test.Errorf("ValidateUDAKey(%q) = nil, want error (%s)", testCase.key, testCase.desc)
			}
		})
	}
}

func TestValidateUDA_Valid(test *testing.T) {
	cases := []map[string]any{
		{},
		{"env": "prod"},
		{"env": "prod", "team": "backend"},
		{"key_with-dash": "value"},
	}
	for _, uda := range cases {
		if err := ValidateUDA(uda); err != nil {
			test.Errorf("ValidateUDA(%v) = %v, want nil", uda, err)
		}
	}
}

func TestValidateUDA_InvalidKey(test *testing.T) {
	uda := map[string]any{"valid": "ok", "1bad": "val"}
	err := ValidateUDA(uda)
	if err == nil {
		test.Fatal("expected error for invalid key")
	}
}

func TestValidateUDA_NonStringValue(test *testing.T) {
	cases := []struct {
		name string
		uda  map[string]any
	}{
		{"int value", map[string]any{"count": 42}},
		{"bool value", map[string]any{"flag": true}},
		{"nil value", map[string]any{"empty": nil}},
		{"nested object", map[string]any{"nested": map[string]any{"a": "b"}}},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			if err := ValidateUDA(testCase.uda); err == nil {
				test.Errorf("ValidateUDA(%v) = nil, want error", testCase.uda)
			}
		})
	}
}

func TestValidateUDA_Nil(test *testing.T) {
	if err := ValidateUDA(nil); err != nil {
		test.Errorf("ValidateUDA(nil) = %v, want nil", err)
	}
}
