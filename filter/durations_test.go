package filter

import (
	"strings"
	"testing"
	"time"
)

func TestParseRelativeDuration(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Duration
		wantErr string
	}{
		{in: "7d", want: 7 * 24 * time.Hour},
		{in: "2w", want: 14 * 24 * time.Hour},
		{in: "24h", want: 24 * time.Hour},
		{in: "30m", want: 30 * time.Minute},
		{in: "90s", want: 90 * time.Second},
		{in: "", wantErr: "must not be empty"},
		{in: "d", wantErr: "must begin with a positive integer"},
		{in: "7", wantErr: "missing unit suffix"},
		{in: "0d", wantErr: "must be positive"},
		{in: "-1d", wantErr: "must begin with a positive integer"},
		{in: "7x", wantErr: `unknown duration unit "x"`},
		{in: "1w2d", wantErr: `unknown duration unit "w2d"`},
		{in: "1.5d", wantErr: `unknown duration unit ".5d"`},
	}

	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseRelativeDuration(tc.in)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil (result %v)", tc.wantErr, got)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q missing substring %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
