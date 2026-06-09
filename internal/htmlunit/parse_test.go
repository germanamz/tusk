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

func TestParse_BlockKindsAndAddresses(test *testing.T) {
	src := []byte(`<h1>Top</h1>` +
		`<p>First para</p>` +
		`<ul><li>One</li><li>Two</li></ul>` +
		`<blockquote>Quoted</blockquote>` +
		`<pre>code <h2>not a heading</h2></pre>`)
	units, err := Parse(src)
	if err != nil {
		test.Fatalf("parse: %v", err)
	}

	type want struct {
		kind subunit.Kind
		addr string
	}
	wants := []want{
		{subunit.KindSection, "S1"},
		{subunit.KindParagraph, "S1P1"},
		{subunit.KindListItem, "S1L1"},
		{subunit.KindListItem, "S1L2"},
		{subunit.KindBlockquote, "S1Q1"},
		{subunit.KindCodeBlock, "S1B1"},
	}
	if len(units) != len(wants) {
		test.Fatalf("want %d units, got %d: %v", len(wants), len(units), addresses(units))
	}
	for i, w := range wants {
		if units[i].Kind != w.kind || units[i].Address != w.addr {
			test.Errorf("unit %d: want %s/%s, got %s/%s", i, w.kind, w.addr, units[i].Kind, units[i].Address)
		}
	}
}

func TestParse_RootLevelParagraphNoHeading(test *testing.T) {
	units, err := Parse([]byte(`<p>Just prose</p>`))
	if err != nil {
		test.Fatalf("parse: %v", err)
	}
	if len(units) != 1 {
		test.Fatalf("want 1 unit, got %d", len(units))
	}
	if units[0].Kind != subunit.KindParagraph || units[0].Address != "P1" {
		test.Errorf("want paragraph/P1, got %s/%s", units[0].Kind, units[0].Address)
	}
	if units[0].ParentAddress != "" {
		test.Errorf("root paragraph parent: want empty, got %q", units[0].ParentAddress)
	}
}

func TestParse_TableCellAddressing(test *testing.T) {
	src := []byte(`<h1>Top</h1><table>` +
		`<tr><th>Name</th><th>Age</th></tr>` +
		`<tr><td>Ada</td><td>36</td></tr>` +
		`</table>`)
	units, err := Parse(src)
	if err != nil {
		test.Fatalf("parse: %v", err)
	}

	// section + 2 header cells + 2 body cells
	if len(units) != 5 {
		test.Fatalf("want 5 units, got %d: %v", len(units), addresses(units))
	}
	wantAddr := []string{"S1", "S1T1R0C0", "S1T1R0C1", "S1T1R1C0", "S1T1R1C1"}
	if strings.Join(addresses(units), ",") != strings.Join(wantAddr, ",") {
		test.Fatalf("addresses: want %v, got %v", wantAddr, addresses(units))
	}
	for _, u := range units[1:] {
		if u.Kind != subunit.KindTableCell {
			test.Errorf("kind for %q: want table-cell, got %q", u.Address, u.Kind)
		}
	}

	body := units[4] // Age=36 cell
	if body.EmbedPayload != "Age: 36" {
		test.Errorf("body cell embed payload: want %q, got %q", "Age: 36", body.EmbedPayload)
	}
	if got, _ := body.Properties["column-header"].(string); got != "Age" {
		test.Errorf("column-header property: want %q, got %q", "Age", got)
	}
}
