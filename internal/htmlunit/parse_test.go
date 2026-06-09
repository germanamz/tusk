package htmlunit

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/subunit"
)

// addresses returns the Address of each unit in order, for compact
// outline assertions.
func addresses(units []subunit.Unit) []string {
	out := make([]string, len(units))
	for i, u := range units {
		out[i] = u.Address
	}
	return out
}

func TestParse_HeadingOutlineDepthNotTagNumber(test *testing.T) {
	src := []byte(`<h1>Top</h1><h3>Skipped level</h3><h1>Second top</h1>`)
	units, err := Parse(src)
	if err != nil {
		test.Fatalf("parse: %v", err)
	}

	want := []string{"S1", "S1.1", "S2"}
	got := addresses(units)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		test.Fatalf("addresses: want %v, got %v", want, got)
	}
	for _, u := range units {
		if u.Kind != subunit.KindSection {
			test.Errorf("kind: want %q, got %q for %q", subunit.KindSection, u.Kind, u.Address)
		}
	}
}
