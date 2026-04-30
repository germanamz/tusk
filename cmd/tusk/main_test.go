package main

import (
	"reflect"
	"testing"
)

func TestStripConfigFlag(test *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "space form",
			in:   []string{"list", "--config", "/tmp/a.toml", "+urgent"},
			want: []string{"list", "+urgent"},
		},
		{
			name: "equals form",
			in:   []string{"list", "--config=/tmp/a.toml", "+urgent"},
			want: []string{"list", "+urgent"},
		},
		{
			name: "absent",
			in:   []string{"list", "+urgent"},
			want: []string{"list", "+urgent"},
		},
		{
			name: "at end space form",
			in:   []string{"list", "--config", "/tmp/a.toml"},
			want: []string{"list"},
		},
		{
			name: "multiple occurrences — last wins at resolve time, but strip removes all",
			in:   []string{"list", "--config", "/tmp/a.toml", "--config=/tmp/b.toml"},
			want: []string{"list"},
		},
		{
			name: "mixed with --db",
			in:   []string{"--config", "/tmp/a.toml", "list"},
			want: []string{"list"},
		},
	}

	for _, testCase := range tests {
		test.Run(testCase.name, func(test *testing.T) {
			got := stripConfigFlag(testCase.in)
			if !reflect.DeepEqual(got, testCase.want) {
				test.Fatalf("got %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestIsCompletionInvocation(test *testing.T) {
	tests := []struct {
		name string
		in   []string
		want bool
	}{
		{name: "empty", in: []string{}, want: false},
		{name: "bare completion", in: []string{"completion"}, want: true},
		{name: "completion bash", in: []string{"completion", "bash"}, want: true},
		{name: "hidden rpc", in: []string{"__complete", "task", ""}, want: true},
		{name: "config space before completion", in: []string{"--config", "/tmp/x", "completion", "zsh"}, want: true},
		{name: "config equals before completion", in: []string{"--config=/tmp/x", "completion", "fish"}, want: true},
		{name: "db space before completion", in: []string{"--db", "/tmp/y", "completion", "bash"}, want: true},
		{name: "db equals before completion", in: []string{"--db=/tmp/y", "completion", "bash"}, want: true},
		{name: "format before completion", in: []string{"--format", "json", "completion", "bash"}, want: true},
		{name: "no-color before completion", in: []string{"--no-color", "completion", "bash"}, want: true},
		{name: "player before completion", in: []string{"--player", "me", "completion", "bash"}, want: true},
		{name: "task list", in: []string{"task", "list"}, want: false},
		{name: "config before task list", in: []string{"--config", "/tmp/x", "task", "list"}, want: false},
		{name: "version", in: []string{"version"}, want: false},
		{name: "mcp serve", in: []string{"mcp", "serve"}, want: false},
	}

	for _, testCase := range tests {
		test.Run(testCase.name, func(test *testing.T) {
			if got := isCompletionInvocation(testCase.in); got != testCase.want {
				test.Fatalf("isCompletionInvocation(%v) = %v, want %v", testCase.in, got, testCase.want)
			}
		})
	}
}
