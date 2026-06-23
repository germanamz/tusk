// Package subunit defines the in-memory representation of markdown
// sub-document units (sections, paragraphs, list items, code blocks,
// blockquotes, table cells) and the goldmark-backed parser that
// derives them from a file body.
//
// The package is a pure transformation layer: it does not touch the
// SQLite index, the embed queue, or any repository. Callers (the
// reindex pipeline in Task 3, the AST chunker in Task 4) consume the
// emitted `[]Unit` slice and persist it themselves.
//
// Sub-unit identity is the SHA-256 of a kind-specific input truncated
// to 12 hex chars, optionally followed by a `-N` ordinal suffix when
// two units in the same file produce the same bare hash. The hashing
// rules and the reserved properties are owned by this package; the
// node-type and edge-type declarations live in the sibling
// `internal/typepacks/subdocument` pack.
package subunit

// Kind is the sub-unit node-type name. The values match the reserved
// node-type names registered by the sub-document built-in type pack
// (see `internal/typepacks/subdocument.ReservedNodeTypes`).
type Kind string

const (
	// KindSection is a markdown heading and all of its descendant
	// blocks. Sections are not embedded directly; their score is
	// derived from descendant leaves at query time (per spec §5.7).
	KindSection Kind = "section"
	// KindParagraph is a top-level prose paragraph.
	KindParagraph Kind = "paragraph"
	// KindListItem is a single list item (ordered or unordered).
	// Task-list checkboxes surface as the `checkbox` property.
	KindListItem Kind = "list-item"
	// KindCodeBlock is a fenced or indented code block. The
	// `lang` property carries the info-string language tag for
	// fenced blocks.
	KindCodeBlock Kind = "code-block"
	// KindBlockquote is a blockquote. Nested blockquotes collapse
	// into a single unit per spec §5.1.
	KindBlockquote Kind = "blockquote"
	// KindTableCell is one cell of a GFM table. Tables themselves
	// do not emit a unit; only their cells do.
	KindTableCell Kind = "table-cell"
)

// Unit is the parser's output type for one sub-document node. It
// carries everything a downstream consumer needs to write a row into
// the `nodes` table plus the synthesized payload the embedder should
// send to the model.
type Unit struct {
	// Kind is the sub-unit type. One of the Kind* constants above.
	Kind Kind
	// Hash is the unit's content-hash identity, retained only as the
	// fallback id suffix when the kind has no structural address.
	// Twelve hex chars; never carries the leading `#`.
	Hash string
	// Address is the structural address suffix that identifies this unit
	// within its file: "S1.2" (section path), "S1.2P3" (paragraph),
	// "P1" (root-level), "S1.2T1R2C0" (table cell). Empty when the kind
	// has no address rule, in which case the id falls back to Hash.
	// Assigned by Parse.
	Address string
	// ContentHash is the current content fingerprint stored on the row's
	// nodes.content_hash column. For leaf kinds it is sha256(EmbedPayload)
	// — exactly what the embedder sends — so a leaf that merely shifts
	// position keeps its hash and reuses its vector. For sections it is
	// sha256("section\x00<level>\x00<heading-text>") so heading edits are
	// detected (sections are never embedded). Lowercase hex.
	ContentHash string
	// Ordinal is the unit's depth-first position within the file,
	// 0-based. Assigned by Parse across all emitted units.
	Ordinal int
	// ParentAddress is the Address of the enclosing section, or the
	// empty string at the document root (before the first heading, or in
	// a heading-free document). Sub-units of sub-units (paragraphs under a
	// section, an H3 section under an H2) reference the closest enclosing
	// section. Drives parent row-id wiring; assigned by Parse.
	ParentAddress string
	// Text is the literal body text used for storage and display.
	// Uses goldmark's normalized form, which collapses adjacent
	// whitespace and standardizes line endings.
	Text string
	// EmbedPayload is the text sent to the embedder. For most
	// kinds this equals Text; for table-cell body cells with a
	// known column header it is "<column-header>: <cell-text>"
	// (see spec §5.6).
	EmbedPayload string
	// Properties carries the per-kind reserved properties. The
	// keys are exactly those enumerated by
	// `subdocument.ReservedProperties` for each Kind. Values use
	// their natural Go types (int, bool, string).
	Properties map[string]any
	// Title is a single-line excerpt of the unit's text, used as
	// the `title` column on the `nodes` row.
	Title string
}

// EdgeSpec is a pure-data description of an outbound edge from a
// sub-unit. The reindex pipeline (Task 3) resolves TargetID against
// the manifest's path-to-id mapping and then writes the edge row.
type EdgeSpec struct {
	// EdgeType is the manifest edge-type name (e.g. "links-to").
	// Populated by the caller based on which edge types have
	// `wikilinks = true` in the manifest.
	EdgeType string
	// TargetID is the raw wikilink target string as it appears
	// between `[[...]]`. Resolution to a file id is the caller's
	// responsibility; sub-units never target sub-units (§5.4).
	TargetID string
}
