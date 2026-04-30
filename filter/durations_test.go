package filter

import (
	"strings"
	"testing"
	"time"
)

func TestParseRelativeDuration(test *testing.T) {
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

	for _, testCase := range cases {
		test.Run(testCase.in, func(test *testing.T) {
			got, err := ParseRelativeDuration(testCase.in)
			if testCase.wantErr != "" {
				if err == nil {
					test.Fatalf("expected error containing %q, got nil (result %v)", testCase.wantErr, got)
				}
				if !strings.Contains(err.Error(), testCase.wantErr) {
					test.Fatalf("error %q missing substring %q", err.Error(), testCase.wantErr)
				}
				return
			}
			if err != nil {
				test.Fatalf("unexpected error: %v", err)
			}
			if got != testCase.want {
				test.Fatalf("got %v, want %v", got, testCase.want)
			}
		})
	}
}
