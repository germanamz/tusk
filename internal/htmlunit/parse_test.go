package htmlunit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
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

func TestParse_ContentHashOrdinalParent(test *testing.T) {
	src := []byte(`<h1>Top</h1><p>Body text</p>`)
	units, err := Parse(src)
	if err != nil {
		test.Fatalf("parse: %v", err)
	}
	if len(units) != 2 {
		test.Fatalf("want 2 units, got %d", len(units))
	}

	sec, para := units[0], units[1]

	for i, u := range units {
		if u.Ordinal != i {
			test.Errorf("ordinal: unit %d has Ordinal %d", i, u.Ordinal)
		}
	}
	if para.ParentAddress != "S1" {
		test.Errorf("paragraph parent address: want S1, got %q", para.ParentAddress)
	}
	if para.ParentHash != sec.Hash {
		test.Errorf("paragraph parent hash: want %q, got %q", sec.Hash, para.ParentHash)
	}

	wantLeaf := sha256.Sum256([]byte(para.EmbedPayload))
	if para.ContentHash != hex.EncodeToString(wantLeaf[:]) {
		test.Errorf("leaf content hash: want %q, got %q", hex.EncodeToString(wantLeaf[:]), para.ContentHash)
	}
	wantSec := sha256.Sum256(fmt.Appendf(nil, "section\x00%d\x00%s", 1, "Top"))
	if sec.ContentHash != hex.EncodeToString(wantSec[:]) {
		test.Errorf("section content hash: want %q, got %q", hex.EncodeToString(wantSec[:]), sec.ContentHash)
	}
	if sec.Hash != subunit.ComputeHash(subunit.Unit{
		Kind:       subunit.KindSection,
		Text:       "Top",
		Properties: map[string]any{"heading-level": 1},
	}) {
		test.Errorf("section hash does not match subunit.ComputeHash")
	}
}

func TestParse_Deterministic(test *testing.T) {
	src := []byte(`<h1>A</h1><p>x</p><h2>B</h2><ul><li>y</li></ul>` +
		`<table><tr><th>h</th></tr><tr><td>v</td></tr></table>`)
	first, err := Parse(src)
	if err != nil {
		test.Fatalf("first parse: %v", err)
	}
	second, err := Parse(src)
	if err != nil {
		test.Fatalf("second parse: %v", err)
	}
	if len(first) != len(second) {
		test.Fatalf("unit count differs: %d vs %d", len(first), len(second))
	}
	for i := range first {
		// subunit.Unit carries a Properties map, so the structs are not
		// comparable with ==; reflect.DeepEqual compares the full value
		// (including the map) for the determinism assertion.
		if !reflect.DeepEqual(first[i], second[i]) {
			test.Errorf("unit %d differs across parses:\n a=%+v\n b=%+v", i, first[i], second[i])
		}
	}
}

func TestParse_ListItemCheckboxProperty(test *testing.T) {
	src := []byte(`<ul>` +
		`<li><input type="checkbox" checked> Done thing</li>` +
		`<li><input type="checkbox"> Pending thing</li>` +
		`<li>Plain bullet without checkbox</li>` +
		`</ul>`)
	units, err := Parse(src)
	if err != nil {
		test.Fatalf("parse: %v", err)
	}
	if len(units) != 3 {
		test.Fatalf("want 3 units, got %d: %v", len(units), addresses(units))
	}

	if got, ok := units[0].Properties["checkbox"]; !ok || got != true {
		test.Errorf("unit 0 checkbox: want true, got %v (present=%v)", got, ok)
	}
	if got, ok := units[1].Properties["checkbox"]; !ok || got != false {
		test.Errorf("unit 1 checkbox: want false, got %v (present=%v)", got, ok)
	}
	if _, ok := units[2].Properties["checkbox"]; ok {
		test.Errorf("unit 2: plain bullet must have no checkbox property")
	}

	// The input element contributes no text; the item text is marker-free.
	if units[0].Text != "Done thing" {
		test.Errorf("unit 0 text: want %q, got %q", "Done thing", units[0].Text)
	}
}

func TestParse_CheckboxAttributeVariants(test *testing.T) {
	cases := []struct {
		name string
		li   string
		want any // nil asserts the property is absent
	}{
		{"bare checked", `<li><input type="checkbox" checked> a</li>`, true},
		{"empty checked", `<li><input type="checkbox" checked=""> a</li>`, true},
		{"checked equals checked", `<li><input type="checkbox" checked="checked"> a</li>`, true},
		{"uppercase type value", `<li><input type="CHECKBOX"> a</li>`, false},
		{"loose list paragraph wrap finds checkbox", `<li><p><input type="checkbox"> a</p></li>`, false},
		{"first input wins", `<li><input type="checkbox"> a <input type="checkbox" checked> b</li>`, false},
		{"non-checkbox input", `<li><input type="text"> a</li>`, nil},
		{"input without type", `<li><input> a</li>`, nil},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			units, err := Parse([]byte("<ul>" + testCase.li + "</ul>"))
			if err != nil {
				test.Fatalf("parse: %v", err)
			}
			if len(units) != 1 {
				test.Fatalf("want 1 unit, got %d: %v", len(units), addresses(units))
			}
			got, ok := units[0].Properties["checkbox"]
			if testCase.want == nil {
				if ok {
					test.Fatalf("checkbox: want absent, got %v", got)
				}
				return
			}
			if !ok || got != testCase.want {
				test.Fatalf("checkbox: want %v, got %v (present=%v)", testCase.want, got, ok)
			}
		})
	}
}

func TestParse_CheckboxOutsideListItemIgnored(test *testing.T) {
	src := []byte(`<p><input type="checkbox" checked> not a todo</p>`)
	units, err := Parse(src)
	if err != nil {
		test.Fatalf("parse: %v", err)
	}
	if len(units) != 1 || units[0].Kind != subunit.KindParagraph {
		test.Fatalf("want 1 paragraph unit, got %v", addresses(units))
	}
	if _, ok := units[0].Properties["checkbox"]; ok {
		test.Errorf("paragraph must not carry a checkbox property")
	}
}

func TestParse_CheckboxToggleChangesHashNotContentHash(test *testing.T) {
	done, err := Parse([]byte(`<ul><li><input type="checkbox" checked> Ship it</li></ul>`))
	if err != nil {
		test.Fatalf("parse done: %v", err)
	}
	open, err := Parse([]byte(`<ul><li><input type="checkbox"> Ship it</li></ul>`))
	if err != nil {
		test.Fatalf("parse open: %v", err)
	}
	if len(done) != 1 || len(open) != 1 {
		test.Fatalf("want 1 unit each, got %d and %d", len(done), len(open))
	}
	if done[0].Hash == open[0].Hash {
		test.Errorf("Hash must differ across checkbox state (reindex diffing relies on it)")
	}
	if done[0].ContentHash != open[0].ContentHash {
		test.Errorf("ContentHash must match across checkbox state (embed payload is marker-free)")
	}
}

func TestParse_NestedListItemsEmitAsPeers(test *testing.T) {
	src := []byte(`<h1>Plan</h1><ul>` +
		`<li><input type="checkbox"> Parent task<ul>` +
		`<li><input type="checkbox" checked> Child done</li>` +
		`<li><input type="checkbox"> Child pending</li>` +
		`</ul></li>` +
		`<li>Sibling after nested</li>` +
		`</ul>`)
	units, err := Parse(src)
	if err != nil {
		test.Fatalf("parse: %v", err)
	}

	want := []string{"S1", "S1L1", "S1L2", "S1L3", "S1L4"}
	got := addresses(units)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		test.Fatalf("addresses: want %v, got %v", want, got)
	}

	// Parent own-text excludes the nested items' text.
	if units[1].Text != "Parent task" {
		test.Errorf("parent text: want %q, got %q", "Parent task", units[1].Text)
	}

	wantBoxes := map[int]bool{1: false, 2: true, 3: false}
	for idx, wantBox := range wantBoxes {
		if got, ok := units[idx].Properties["checkbox"]; !ok || got != wantBox {
			test.Errorf("unit %d checkbox: want %v, got %v (present=%v)", idx, wantBox, got, ok)
		}
	}
	if _, ok := units[4].Properties["checkbox"]; ok {
		test.Errorf("sibling plain bullet must have no checkbox property")
	}
}

func TestParse_NestedCheckboxDoesNotLeakToParent(test *testing.T) {
	src := []byte(`<ul><li>Parent<ul><li><input type="checkbox" checked> Child</li></ul></li></ul>`)
	units, err := Parse(src)
	if err != nil {
		test.Fatalf("parse: %v", err)
	}
	if len(units) != 2 {
		test.Fatalf("want 2 units, got %d: %v", len(units), addresses(units))
	}
	if _, ok := units[0].Properties["checkbox"]; ok {
		test.Errorf("parent without own marker must have no checkbox property")
	}
	if got, ok := units[1].Properties["checkbox"]; !ok || got != true {
		test.Errorf("child checkbox: want true, got %v (present=%v)", got, ok)
	}
}

func TestParse_NestedListInsideWrapperStillRecurses(test *testing.T) {
	src := []byte(`<ul><li>Parent<div><ul><li>Wrapped child</li></ul></div></li></ul>`)
	units, err := Parse(src)
	if err != nil {
		test.Fatalf("parse: %v", err)
	}

	want := []string{"L1", "L2"}
	got := addresses(units)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		test.Fatalf("addresses: want %v, got %v", want, got)
	}
	if units[0].Text != "Parent" {
		test.Errorf("parent text: want %q, got %q", "Parent", units[0].Text)
	}
	if units[1].Text != "Wrapped child" {
		test.Errorf("child text: want %q, got %q", "Wrapped child", units[1].Text)
	}
}
