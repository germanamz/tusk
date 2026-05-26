package typeref_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/typeref"
)

func TestParse(test *testing.T) {
	test.Parallel()

	cases := []struct {
		input     string
		wantScope typeref.Scope
		wantSrc   string
		wantType  string
		wantErr   bool
	}{
		{input: "contains", wantScope: typeref.ScopeAny, wantType: "contains"},
		{input: ":contains", wantScope: typeref.ScopeUser, wantType: "contains"},
		{input: "markdown:contains", wantScope: typeref.ScopeSource, wantSrc: "markdown", wantType: "contains"},
		{input: "go:function", wantScope: typeref.ScopeSource, wantSrc: "go", wantType: "function"},
		{input: ":tag", wantScope: typeref.ScopeUser, wantType: "tag"},

		{input: "", wantErr: true},
		{input: ":", wantErr: true},
		{input: "markdown:", wantErr: true},
		{input: ":markdown:contains", wantErr: true},
		{input: "Markdown:contains", wantErr: true}, // uppercase rejected (type names are [a-z0-9-])
		{input: "markdown:Contains", wantErr: true},
		{input: "mark down:contains", wantErr: true}, // whitespace
	}

	for _, tc := range cases {
		tc := tc
		test.Run(tc.input, func(test *testing.T) {
			test.Parallel()

			got, err := typeref.Parse(tc.input)
			if tc.wantErr {
				if err == nil {
					test.Fatalf("Parse(%q) = %+v, want error", tc.input, got)
				}
				return
			}

			if err != nil {
				test.Fatalf("Parse(%q): %v", tc.input, err)
			}
			if got.Scope != tc.wantScope {
				test.Errorf("Scope = %v, want %v", got.Scope, tc.wantScope)
			}
			if got.Source != tc.wantSrc {
				test.Errorf("Source = %q, want %q", got.Source, tc.wantSrc)
			}
			if got.Type != tc.wantType {
				test.Errorf("Type = %q, want %q", got.Type, tc.wantType)
			}
		})
	}
}

func TestRefString(test *testing.T) {
	test.Parallel()

	cases := []struct {
		ref  typeref.Ref
		want string
	}{
		{ref: typeref.Ref{Scope: typeref.ScopeAny, Type: "contains"}, want: "contains"},
		{ref: typeref.Ref{Scope: typeref.ScopeUser, Type: "contains"}, want: ":contains"},
		{ref: typeref.Ref{Scope: typeref.ScopeSource, Source: "markdown", Type: "contains"}, want: "markdown:contains"},
	}

	for _, tc := range cases {
		if got := tc.ref.String(); got != tc.want {
			test.Errorf("Ref{%+v}.String() = %q, want %q", tc.ref, got, tc.want)
		}
	}
}
