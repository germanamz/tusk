package html_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/typepacks/html"
)

func TestHTMLSourceIsHTML(test *testing.T) {
	test.Parallel()

	if html.Source() != "html" {
		test.Errorf("Source() = %q, want %q", html.Source(), "html")
	}
}

func TestHTMLReservedNodeTypesMirrorSubdocument(test *testing.T) {
	test.Parallel()

	want := []string{"section", "paragraph", "list-item", "code-block", "blockquote", "table-cell"}

	if len(html.ReservedNodeTypes) != len(want) {
		test.Fatalf("ReservedNodeTypes len = %d, want %d (%v)", len(html.ReservedNodeTypes), len(want), html.ReservedNodeTypes)
	}

	for index, name := range want {
		if html.ReservedNodeTypes[index] != name {
			test.Errorf("ReservedNodeTypes[%d] = %q, want %q", index, html.ReservedNodeTypes[index], name)
		}
	}
}

func TestHTMLReservedEdgeTypesMirrorSubdocument(test *testing.T) {
	test.Parallel()

	want := []string{"contains", "contained-by"}

	if len(html.ReservedEdgeTypes) != len(want) {
		test.Fatalf("ReservedEdgeTypes = %v, want %v", html.ReservedEdgeTypes, want)
	}

	for index, name := range want {
		if html.ReservedEdgeTypes[index] != name {
			test.Errorf("ReservedEdgeTypes[%d] = %q, want %q", index, html.ReservedEdgeTypes[index], name)
		}
	}
}

func TestHTMLReservedPropertiesMirrorSubdocumentVerbatim(test *testing.T) {
	test.Parallel()

	want := map[string][]string{
		"section":    {"heading-level"},
		"paragraph":  {},
		"list-item":  {"checkbox"},
		"code-block": {"lang"},
		"blockquote": {},
		"table-cell": {"header", "row", "column", "column-header"},
	}

	if len(html.ReservedProperties) != len(want) {
		test.Fatalf("ReservedProperties has %d node types, want %d", len(html.ReservedProperties), len(want))
	}

	for nodeType, wantProps := range want {
		gotProps, declared := html.ReservedProperties[nodeType]

		if !declared {
			test.Errorf("ReservedProperties missing entry for node type %q", nodeType)

			continue
		}

		if len(gotProps) != len(wantProps) {
			test.Errorf("ReservedProperties[%q] = %v, want %v", nodeType, gotProps, wantProps)

			continue
		}

		for index, prop := range wantProps {
			if gotProps[index] != prop {
				test.Errorf("ReservedProperties[%q][%d] = %q, want %q", nodeType, index, gotProps[index], prop)
			}
		}
	}
}

func TestHTMLReservedPropertiesHasNoDataKey(test *testing.T) {
	test.Parallel()

	for nodeType, props := range html.ReservedProperties {
		for _, prop := range props {
			if prop == "data" {
				test.Errorf("ReservedProperties[%q] contains %q; the data-signals exemption is owned by Phase 4, not this pack", nodeType, "data")
			}
		}
	}
}

func TestHTMLNodeTypesReExportSubdocumentSchema(test *testing.T) {
	test.Parallel()

	nodeTypes := html.NodeTypes()

	for _, name := range html.ReservedNodeTypes {
		if _, has := nodeTypes[name]; !has {
			test.Errorf("NodeTypes() missing %q", name)
		}
	}

	section, has := nodeTypes["section"]

	if !has {
		test.Fatalf("NodeTypes() missing 'section'")
	}

	foundHeadingLevel := false

	for _, prop := range section.Properties {
		if prop.Name == "heading-level" {
			foundHeadingLevel = true

			if prop.Type != "int" {
				test.Errorf("section.heading-level type = %q, want %q", prop.Type, "int")
			}
		}
	}

	if !foundHeadingLevel {
		test.Errorf("NodeTypes()[section] missing heading-level property")
	}
}

func TestHTMLNodeTypesReturnsFreshMap(test *testing.T) {
	test.Parallel()

	first := html.NodeTypes()
	delete(first, "section")

	second := html.NodeTypes()

	if _, has := second["section"]; !has {
		test.Errorf("NodeTypes() returned a shared map: mutation of one call leaked into another")
	}
}

func TestHTMLEdgeTypesReExportContains(test *testing.T) {
	test.Parallel()

	edgeTypes := html.EdgeTypes()

	contains, has := edgeTypes["contains"]

	if !has {
		test.Fatalf("EdgeTypes() missing 'contains'")
	}

	if contains.Inverse != "contained-by" {
		test.Errorf("contains.Inverse = %q, want %q", contains.Inverse, "contained-by")
	}

	if contains.Cardinality != manifest.CardinalityOneToMany {
		test.Errorf("contains.Cardinality = %q, want one-to-many", contains.Cardinality)
	}
}
