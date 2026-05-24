package subunit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// readFixture loads a markdown file from testdata. Fails the test if
// the file is missing.
func readFixture(test *testing.T, name string) []byte {
	test.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		test.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// dumpUnits returns a compact, comparable summary of units for
// debugging when a golden assertion fails. Not stable across schema
// changes but cheap to glance at.
func dumpUnits(units []Unit) string {
	var b strings.Builder
	for _, u := range units {
		fmt.Fprintf(&b, "[%d] kind=%s hash=%q parent=%q title=%q text=%q props=%v\n",
			u.Ordinal, u.Kind, u.Hash, u.ParentHash, u.Title, u.Text, u.Properties)
	}
	return b.String()
}

func TestParse_SingleParagraph(test *testing.T) {
	src := readFixture(test, "single-paragraph.md")
	units, err := Parse(src)
	if err != nil {
		test.Fatalf("parse: %v", err)
	}

	if len(units) != 1 {
		test.Fatalf("want 1 unit, got %d\n%s", len(units), dumpUnits(units))
	}

	got := units[0]
	if got.Kind != KindParagraph {
		test.Errorf("kind: want %q, got %q", KindParagraph, got.Kind)
	}
	if got.Ordinal != 0 {
		test.Errorf("ordinal: want 0, got %d", got.Ordinal)
	}
	if got.ParentHash != "" {
		test.Errorf("parent hash: want empty, got %q", got.ParentHash)
	}
	if got.Hash != "380839c0e786" {
		test.Errorf("hash: want %q (golden), got %q -- dump:\n%s",
			"380839c0e786", got.Hash, dumpUnits(units))
	}
	if !strings.Contains(got.Text, "single paragraph") {
		test.Errorf("text: missing expected substring, got %q", got.Text)
	}
	if got.EmbedPayload != got.Text {
		test.Errorf("embed payload: want equal to text, got %q vs %q", got.EmbedPayload, got.Text)
	}
}

func TestParse_H1H2H3Nesting(test *testing.T) {
	src := readFixture(test, "h1-h2-h3-nesting.md")
	units, err := Parse(src)
	if err != nil {
		test.Fatalf("parse: %v", err)
	}

	// Expected sequence (depth-first):
	//   0 section H1 "Top heading"        parent=""
	//   1 paragraph "Intro paragraph..."  parent=H1
	//   2 section H2 "Sub heading"        parent=H1
	//   3 paragraph "Body of H2."         parent=H2
	//   4 section H3 "Deep heading"       parent=H2
	//   5 paragraph "Body of H3."         parent=H3
	//   6 section H2 "Sibling sub"        parent=H1
	//   7 paragraph "Tail paragraph..."   parent=H2-sibling

	if len(units) != 8 {
		test.Fatalf("want 8 units, got %d\n%s", len(units), dumpUnits(units))
	}

	wantKinds := []Kind{
		KindSection, KindParagraph,
		KindSection, KindParagraph,
		KindSection, KindParagraph,
		KindSection, KindParagraph,
	}
	wantHeadingLevels := map[int]int{0: 1, 2: 2, 4: 3, 6: 2}

	for i, want := range wantKinds {
		if units[i].Kind != want {
			test.Errorf("[%d] kind: want %q, got %q", i, want, units[i].Kind)
		}
	}
	for i, wantLvl := range wantHeadingLevels {
		got := units[i].Properties["heading-level"]
		if got != wantLvl {
			test.Errorf("[%d] heading-level: want %d, got %v", i, wantLvl, got)
		}
	}

	// Parent-hash relationships.
	if units[0].ParentHash != "" {
		test.Errorf("H1 parent: want empty, got %q", units[0].ParentHash)
	}
	if units[1].ParentHash != units[0].Hash {
		test.Errorf("H1 intro parent: want %q, got %q", units[0].Hash, units[1].ParentHash)
	}
	if units[2].ParentHash != units[0].Hash {
		test.Errorf("H2 parent: want %q (H1), got %q", units[0].Hash, units[2].ParentHash)
	}
	if units[3].ParentHash != units[2].Hash {
		test.Errorf("H2 body parent: want %q (H2), got %q", units[2].Hash, units[3].ParentHash)
	}
	if units[4].ParentHash != units[2].Hash {
		test.Errorf("H3 parent: want %q (H2), got %q", units[2].Hash, units[4].ParentHash)
	}
	if units[5].ParentHash != units[4].Hash {
		test.Errorf("H3 body parent: want %q (H3), got %q", units[4].Hash, units[5].ParentHash)
	}
	if units[6].ParentHash != units[0].Hash {
		test.Errorf("sibling H2 parent: want %q (H1), got %q", units[0].Hash, units[6].ParentHash)
	}
	if units[7].ParentHash != units[6].Hash {
		test.Errorf("tail paragraph parent: want %q (sibling H2), got %q", units[6].Hash, units[7].ParentHash)
	}

	// Golden hashes: pinned to catch accidental hash-input drift.
	wantHashes := []string{
		"fdfa619e206d", // H1 Top heading
		"ac9d60fbbba7", // Intro paragraph
		"bae4ad573522", // H2 Sub heading
		"4d5288e2b86e", // Body of H2
		"1e40b6a28327", // H3 Deep heading
		"7828482d495e", // Body of H3
		"35e3c6f607a2", // Sibling H2
		"7c107309a7f8", // Tail paragraph
	}
	for i, want := range wantHashes {
		if units[i].Hash != want {
			test.Errorf("[%d] hash: want %q, got %q -- dump:\n%s",
				i, want, units[i].Hash, dumpUnits(units))
		}
	}
}

func TestParse_TaskList(test *testing.T) {
	src := readFixture(test, "task-list.md")
	units, err := Parse(src)
	if err != nil {
		test.Fatalf("parse: %v", err)
	}

	if len(units) != 3 {
		test.Fatalf("want 3 list-items, got %d\n%s", len(units), dumpUnits(units))
	}

	for i, u := range units {
		if u.Kind != KindListItem {
			test.Errorf("[%d] kind: want list-item, got %q", i, u.Kind)
		}
	}

	if got, want := units[0].Properties["checkbox"], true; got != want {
		test.Errorf("[0] checkbox: want true, got %v", got)
	}
	if got, want := units[1].Properties["checkbox"], false; got != want {
		test.Errorf("[1] checkbox: want false, got %v", got)
	}
	if _, has := units[2].Properties["checkbox"]; has {
		test.Errorf("[2] plain bullet should not have checkbox property, props=%v", units[2].Properties)
	}

	if !strings.Contains(units[0].Text, "Done thing") {
		test.Errorf("[0] text: missing 'Done thing', got %q", units[0].Text)
	}
	if !strings.Contains(units[1].Text, "Pending thing") {
		test.Errorf("[1] text: missing 'Pending thing', got %q", units[1].Text)
	}

	// Golden hashes.
	want := []string{
		"940a033a7e20", // [x] Done thing
		"fe6753c692aa", // [ ] Pending thing
		"0163a0bd6595", // Plain bullet without checkbox
	}
	for i, w := range want {
		if units[i].Hash != w {
			test.Errorf("[%d] hash: want %q, got %q", i, w, units[i].Hash)
		}
	}
}

func TestParse_TableWithHeader(test *testing.T) {
	src := readFixture(test, "table-with-header.md")
	units, err := Parse(src)
	if err != nil {
		test.Fatalf("parse: %v", err)
	}

	// Two header cells + two body rows × two cells = 6.
	if len(units) != 6 {
		test.Fatalf("want 6 cells, got %d\n%s", len(units), dumpUnits(units))
	}

	for i, u := range units {
		if u.Kind != KindTableCell {
			test.Errorf("[%d] kind: want table-cell, got %q", i, u.Kind)
		}
	}

	// Header cells.
	if got := units[0].Properties["header"]; got != true {
		test.Errorf("[0] header: want true, got %v", got)
	}
	if got := units[1].Properties["header"]; got != true {
		test.Errorf("[1] header: want true, got %v", got)
	}
	if units[0].Text != "Name" {
		test.Errorf("[0] text: want %q, got %q", "Name", units[0].Text)
	}
	if units[1].Text != "Status" {
		test.Errorf("[1] text: want %q, got %q", "Status", units[1].Text)
	}
	// Header cells embed bare text.
	if units[1].EmbedPayload != "Status" {
		test.Errorf("[1] embed payload: want %q, got %q", "Status", units[1].EmbedPayload)
	}

	// Body cell (Alice).
	if got := units[2].Properties["header"]; got != false {
		test.Errorf("[2] header: want false, got %v", got)
	}
	if got := units[2].Properties["row"]; got != 1 {
		test.Errorf("[2] row: want 1, got %v", got)
	}
	if got := units[2].Properties["column"]; got != 0 {
		test.Errorf("[2] column: want 0, got %v", got)
	}
	if got := units[2].Properties["column-header"]; got != "Name" {
		test.Errorf("[2] column-header: want %q, got %v", "Name", got)
	}
	if units[2].EmbedPayload != "Name: Alice" {
		test.Errorf("[2] embed payload: want %q, got %q", "Name: Alice", units[2].EmbedPayload)
	}

	// Body cell (Open under Status).
	if got := units[3].Properties["column-header"]; got != "Status" {
		test.Errorf("[3] column-header: want %q, got %v", "Status", got)
	}
	if units[3].EmbedPayload != "Status: Open" {
		test.Errorf("[3] embed payload: want %q, got %q", "Status: Open", units[3].EmbedPayload)
	}

	// Last body cell row index.
	if got := units[5].Properties["row"]; got != 2 {
		test.Errorf("[5] row: want 2, got %v", got)
	}

	// Golden hashes.
	want := []string{
		"8448beb571fe", // Name (header)
		"6000dfcdd420", // Status (header)
		"f6a3abb398ef", // Alice
		"7d5831d35398", // Open
		"ede2b8b7b9f8", // Bob
		"dff710cfe8e9", // Done
	}
	for i, w := range want {
		if units[i].Hash != w {
			test.Errorf("[%d] hash: want %q, got %q", i, w, units[i].Hash)
		}
	}
}

func TestParse_FencedCode(test *testing.T) {
	src := readFixture(test, "fenced-code.md")
	units, err := Parse(src)
	if err != nil {
		test.Fatalf("parse: %v", err)
	}

	if len(units) != 2 {
		test.Fatalf("want 2 units, got %d\n%s", len(units), dumpUnits(units))
	}

	if units[0].Kind != KindParagraph {
		test.Errorf("[0] kind: want paragraph, got %q", units[0].Kind)
	}
	if units[1].Kind != KindCodeBlock {
		test.Errorf("[1] kind: want code-block, got %q", units[1].Kind)
	}
	if got := units[1].Properties["lang"]; got != "go" {
		test.Errorf("[1] lang: want %q, got %v", "go", got)
	}
	if !strings.Contains(units[1].Text, "package main") {
		test.Errorf("[1] text: missing 'package main', got %q", units[1].Text)
	}

	// Golden hashes.
	if units[0].Hash != "13bd52317541" {
		test.Errorf("[0] hash: want %q, got %q", "13bd52317541", units[0].Hash)
	}
	if units[1].Hash != "a593bf735c0d" {
		test.Errorf("[1] hash: want %q, got %q", "a593bf735c0d", units[1].Hash)
	}
}

func TestParse_NestedBlockquoteFlattensToOne(test *testing.T) {
	src := readFixture(test, "nested-blockquote.md")
	units, err := Parse(src)
	if err != nil {
		test.Fatalf("parse: %v", err)
	}

	// Nested blockquotes collapse into a single blockquote unit
	// per spec §5.1.
	if len(units) != 1 {
		test.Fatalf("want 1 blockquote, got %d\n%s", len(units), dumpUnits(units))
	}

	got := units[0]
	if got.Kind != KindBlockquote {
		test.Errorf("kind: want blockquote, got %q", got.Kind)
	}
	if !strings.Contains(got.Text, "Outer quote") {
		test.Errorf("text missing outer: %q", got.Text)
	}
	if !strings.Contains(got.Text, "Inner quote") {
		test.Errorf("text missing inner: %q", got.Text)
	}
	if !strings.Contains(got.Text, "Deeper") {
		test.Errorf("text missing deeper: %q", got.Text)
	}
	if got.Hash != "bcb631669171" {
		test.Errorf("hash: want %q, got %q -- dump:\n%s", "bcb631669171", got.Hash, dumpUnits(units))
	}
}

func TestParse_DuplicateParagraphsCollisionSuffix(test *testing.T) {
	src := readFixture(test, "duplicate-paragraphs.md")
	units, err := Parse(src)
	if err != nil {
		test.Fatalf("parse: %v", err)
	}

	if len(units) != 4 {
		test.Fatalf("want 4 paragraphs, got %d\n%s", len(units), dumpUnits(units))
	}

	// First "Same line of text." keeps its bare hash; second and
	// third get "-1" and "-2" suffixes; the middle one is unique.
	bareHash := units[0].Hash
	if strings.Contains(bareHash, "-") {
		test.Errorf("[0] should keep bare hash, got %q", bareHash)
	}
	if units[1].Hash == bareHash {
		test.Errorf("[1] middle paragraph collided with [0]; got %q", units[1].Hash)
	}
	if units[2].Hash != bareHash+"-1" {
		test.Errorf("[2] hash: want %q, got %q", bareHash+"-1", units[2].Hash)
	}
	if units[3].Hash != bareHash+"-2" {
		test.Errorf("[3] hash: want %q, got %q", bareHash+"-2", units[3].Hash)
	}

	// Pin the bare hash so any future change to paragraph hashing
	// surfaces immediately.
	if bareHash != "5eeca445c587" {
		test.Errorf("paragraph bare hash drifted: want %q, got %q", "5eeca445c587", bareHash)
	}
}

func TestParse_HorizontalRuleEmitsNoUnit(test *testing.T) {
	src := readFixture(test, "horizontal-rule.md")
	units, err := Parse(src)
	if err != nil {
		test.Fatalf("parse: %v", err)
	}

	if len(units) != 2 {
		test.Fatalf("want 2 paragraphs (no rule unit), got %d\n%s", len(units), dumpUnits(units))
	}
	for i, u := range units {
		if u.Kind != KindParagraph {
			test.Errorf("[%d] kind: want paragraph, got %q", i, u.Kind)
		}
	}
}

func TestDeriveEdges_WikilinksInParagraph(test *testing.T) {
	src := readFixture(test, "wikilinks-in-paragraph.md")
	units, err := Parse(src)
	if err != nil {
		test.Fatalf("parse: %v", err)
	}

	if len(units) != 1 {
		test.Fatalf("want 1 unit, got %d", len(units))
	}

	edges := DeriveEdges(units[0], []string{"links-to"})
	if len(edges) != 2 {
		test.Fatalf("want 2 edges, got %d (%+v)", len(edges), edges)
	}

	if edges[0].EdgeType != "links-to" || edges[0].TargetID != "notes/auth-rfc" {
		test.Errorf("edge[0]: want links-to->notes/auth-rfc, got %+v", edges[0])
	}
	if edges[1].EdgeType != "links-to" || edges[1].TargetID != "design/oauth-pkce" {
		test.Errorf("edge[1]: want links-to->design/oauth-pkce, got %+v", edges[1])
	}
}

func TestDeriveEdges_MultipleEdgeTypesCrossProduct(test *testing.T) {
	src := readFixture(test, "wikilinks-in-paragraph.md")
	units, _ := Parse(src)

	edges := DeriveEdges(units[0], []string{"links-to", "mentions"})
	if len(edges) != 4 {
		test.Fatalf("want 4 edges (2 targets × 2 types), got %d", len(edges))
	}

	// Order: per-target, types in caller order.
	want := []EdgeSpec{
		{EdgeType: "links-to", TargetID: "notes/auth-rfc"},
		{EdgeType: "mentions", TargetID: "notes/auth-rfc"},
		{EdgeType: "links-to", TargetID: "design/oauth-pkce"},
		{EdgeType: "mentions", TargetID: "design/oauth-pkce"},
	}
	for i, w := range want {
		if edges[i] != w {
			test.Errorf("[%d] want %+v, got %+v", i, w, edges[i])
		}
	}
}

func TestDeriveEdges_NoEdgeTypesReturnsNil(test *testing.T) {
	src := readFixture(test, "wikilinks-in-paragraph.md")
	units, _ := Parse(src)

	if got := DeriveEdges(units[0], nil); got != nil {
		test.Errorf("want nil, got %+v", got)
	}
}

func TestDeriveEdges_NoWikilinksReturnsNil(test *testing.T) {
	src := readFixture(test, "single-paragraph.md")
	units, _ := Parse(src)

	if got := DeriveEdges(units[0], []string{"links-to"}); got != nil {
		test.Errorf("want nil for paragraph with no wikilinks, got %+v", got)
	}
}

// TestParse_OrdinalIsDepthFirstIndex checks ordinal monotonicity
// across a fixture with mixed kinds.
func TestParse_OrdinalIsDepthFirstIndex(test *testing.T) {
	src := readFixture(test, "h1-h2-h3-nesting.md")
	units, _ := Parse(src)
	for i, u := range units {
		if u.Ordinal != i {
			test.Errorf("[%d] ordinal: want %d, got %d", i, i, u.Ordinal)
		}
	}
}

// TestParse_TitleIsSingleLineExcerpt asserts that the title column
// is a single-line trimmed excerpt.
func TestParse_TitleIsSingleLineExcerpt(test *testing.T) {
	src := readFixture(test, "fenced-code.md")
	units, _ := Parse(src)

	// The code block has multi-line text; the title must be just
	// one line.
	codeBlock := units[1]
	if strings.Contains(codeBlock.Title, "\n") {
		test.Errorf("title must not contain newline, got %q", codeBlock.Title)
	}
	if codeBlock.Title == "" {
		test.Errorf("title should not be empty for non-empty code block")
	}
}

// TestParse_SectionHashIncludesCodeBlockContent locks in the fix for
// the bug where two sections sharing a heading but differing only in
// their fenced-code-block content used to hash identically because
// goldmark's Node.Text(source) returns "" for FencedCodeBlock.
func TestParse_SectionHashIncludesCodeBlockContent(test *testing.T) {
	src := readFixture(test, "section-with-code.md")
	units, err := Parse(src)

	if err != nil {
		test.Fatalf("parse: %v", err)
	}

	// Expect: two H2 sections (each containing one body block).
	var sections []Unit
	for _, unit := range units {
		if unit.Kind == KindSection {
			sections = append(sections, unit)
		}
	}

	if len(sections) != 2 {
		test.Fatalf("want 2 sections, got %d\n%s", len(sections), dumpUnits(units))
	}

	if sections[0].Hash == sections[1].Hash {
		test.Errorf("sections with same heading but different bodies (code vs prose) must hash differently; both got %q\n%s",
			sections[0].Hash, dumpUnits(units))
	}

	// A second fixture mutates the code-block body. The H2 section
	// hash for the code-bearing section must change relative to the
	// original.
	mutSrc := readFixture(test, "section-with-code-mutated.md")
	mutUnits, mutErr := Parse(mutSrc)

	if mutErr != nil {
		test.Fatalf("parse mutated: %v", mutErr)
	}

	var mutCodeSection Unit
	for _, unit := range mutUnits {
		if unit.Kind == KindSection {
			mutCodeSection = unit
			break
		}
	}

	if mutCodeSection.Hash == sections[0].Hash {
		test.Errorf("mutating code-block contents must change the section hash; both got %q",
			sections[0].Hash)
	}
}

// TestMakeTitle_TruncatesOnRuneBoundary locks in the fix for the
// UTF-8 truncation bug. titleMaxLen counts runes (including the
// trailing ellipsis), and the result must always be valid UTF-8.
func TestMakeTitle_TruncatesOnRuneBoundary(test *testing.T) {
	// A multi-byte rune ('é' is 2 bytes in UTF-8). Repeating it
	// 200 times yields 200 runes / 400 bytes — comfortably past
	// titleMaxLen (120) in either unit.
	input := strings.Repeat("é", 200)

	got := makeTitle(input)

	if !utf8.ValidString(got) {
		test.Errorf("makeTitle returned invalid UTF-8: %q", got)
	}

	if runeCount := utf8.RuneCountInString(got); runeCount > titleMaxLen {
		test.Errorf("makeTitle returned %d runes, want <= %d", runeCount, titleMaxLen)
	}

	if !strings.HasSuffix(got, "…") {
		test.Errorf("makeTitle should end in ellipsis when truncating, got %q", got)
	}
}

// TestMakeTitle_ShortStringUnchanged verifies that strings shorter
// than titleMaxLen are returned verbatim (after whitespace
// normalization).
func TestMakeTitle_ShortStringUnchanged(test *testing.T) {
	input := "short title that fits"

	got := makeTitle(input)

	if got != input {
		test.Errorf("makeTitle(%q): want %q, got %q", input, input, got)
	}
}
