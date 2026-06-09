package html_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/typepacks/html"
	"github.com/germanamz/tusk/internal/typepacks/subdocument"
)

// TestSourcesAreDistinct pins that the HTML and markdown sub-document
// packs occupy different source namespaces — the single dimension that
// distinguishes their otherwise-identical reserved-name sets.
func TestSourcesAreDistinct(test *testing.T) {
	test.Parallel()

	if html.Source() == subdocument.Source() {
		test.Fatalf("html.Source() and subdocument.Source() both %q; must be distinct", html.Source())
	}

	if html.Source() != "html" {
		test.Errorf("html.Source() = %q, want %q", html.Source(), "html")
	}

	if subdocument.Source() != "markdown" {
		test.Errorf("subdocument.Source() = %q, want %q", subdocument.Source(), "markdown")
	}
}

// TestReservedNodeTypesMatchSubdocumentVerbatim confirms the HTML pack
// reserves exactly the same node-type names as the markdown pack, in
// the same order — the spec mandates a verbatim mirror.
func TestReservedNodeTypesMatchSubdocumentVerbatim(test *testing.T) {
	test.Parallel()

	if len(html.ReservedNodeTypes) != len(subdocument.ReservedNodeTypes) {
		test.Fatalf("ReservedNodeTypes len mismatch: html=%v subdocument=%v", html.ReservedNodeTypes, subdocument.ReservedNodeTypes)
	}

	for index := range subdocument.ReservedNodeTypes {
		if html.ReservedNodeTypes[index] != subdocument.ReservedNodeTypes[index] {
			test.Errorf("ReservedNodeTypes[%d]: html=%q subdocument=%q", index, html.ReservedNodeTypes[index], subdocument.ReservedNodeTypes[index])
		}
	}
}

// TestReservedEdgeTypesMatchSubdocumentVerbatim is the edge-type
// analogue of the node-type test above.
func TestReservedEdgeTypesMatchSubdocumentVerbatim(test *testing.T) {
	test.Parallel()

	if len(html.ReservedEdgeTypes) != len(subdocument.ReservedEdgeTypes) {
		test.Fatalf("ReservedEdgeTypes mismatch: html=%v subdocument=%v", html.ReservedEdgeTypes, subdocument.ReservedEdgeTypes)
	}

	for index := range subdocument.ReservedEdgeTypes {
		if html.ReservedEdgeTypes[index] != subdocument.ReservedEdgeTypes[index] {
			test.Errorf("ReservedEdgeTypes[%d]: html=%q subdocument=%q", index, html.ReservedEdgeTypes[index], subdocument.ReservedEdgeTypes[index])
		}
	}
}

// TestReservedPropertiesMatchSubdocumentVerbatim confirms the HTML
// pack's reserved properties are byte-for-byte the subdocument set —
// same node types, same property names in the same order, with NO extra
// keys (in particular, no "data" signals key: that drift exemption is
// owned by Phase 4, not this declarations pack).
func TestReservedPropertiesMatchSubdocumentVerbatim(test *testing.T) {
	test.Parallel()

	if len(html.ReservedProperties) != len(subdocument.ReservedProperties) {
		test.Fatalf("ReservedProperties node-type count mismatch: html=%v subdocument=%v", html.ReservedProperties, subdocument.ReservedProperties)
	}

	for nodeType, markdownProps := range subdocument.ReservedProperties {
		htmlProps, has := html.ReservedProperties[nodeType]

		if !has {
			test.Errorf("html ReservedProperties missing node type %q", nodeType)

			continue
		}

		if len(htmlProps) != len(markdownProps) {
			test.Errorf("html ReservedProperties[%q] = %v, want %v (verbatim)", nodeType, htmlProps, markdownProps)

			continue
		}

		for index, prop := range markdownProps {
			if htmlProps[index] != prop {
				test.Errorf("html ReservedProperties[%q][%d] = %q, want %q", nodeType, index, htmlProps[index], prop)
			}
		}
	}

	// Explicit guard: no node type may reserve "data" — Phase 4 owns that.
	for nodeType, props := range html.ReservedProperties {
		for _, prop := range props {
			if prop == "data" {
				test.Errorf("html ReservedProperties[%q] contains %q; the data-signals exemption is owned by Phase 4", nodeType, "data")
			}
		}
	}
}
