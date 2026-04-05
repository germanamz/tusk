package tui

import "testing"

func TestParseUDAFlags_Valid(t *testing.T) {
	cases := []struct {
		name   string
		input  []string
		expect map[string]any
	}{
		{"single", []string{"env=prod"}, map[string]any{"env": "prod"}},
		{"multiple", []string{"env=prod", "team=backend"}, map[string]any{"env": "prod", "team": "backend"}},
		{"empty value clears", []string{"env="}, map[string]any{"env": ""}},
		{"value with equals", []string{"desc=a=b"}, map[string]any{"desc": "a=b"}},
		{"duplicate last wins", []string{"env=dev", "env=prod"}, map[string]any{"env": "prod"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseUDAFlags(tc.input)
			if err != nil {
				t.Fatalf("parseUDAFlags(%v) error: %v", tc.input, err)
			}
			for k, want := range tc.expect {
				if got[k] != want {
					t.Errorf("key %q: got %v, want %v", k, got[k], want)
				}
			}
			if len(got) != len(tc.expect) {
				t.Errorf("got %d keys, want %d", len(got), len(tc.expect))
			}
		})
	}
}

func TestParseUDAFlags_Invalid(t *testing.T) {
	cases := []struct {
		name  string
		input []string
	}{
		{"no equals", []string{"invalid"}},
		{"empty key", []string{"=value"}},
		{"invalid key chars", []string{"my.key=value"}},
		{"key starts with digit", []string{"1key=value"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseUDAFlags(tc.input)
			if err == nil {
				t.Fatalf("parseUDAFlags(%v) = nil error, want error", tc.input)
			}
		})
	}
}

func TestParseUDAFlags_Empty(t *testing.T) {
	got, err := parseUDAFlags(nil)
	if err != nil {
		t.Fatalf("parseUDAFlags(nil) error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}
