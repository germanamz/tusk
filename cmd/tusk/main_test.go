package main

import (
	"reflect"
	"testing"
)

func TestStripConfigFlag(t *testing.T) {
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

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := stripConfigFlag(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
