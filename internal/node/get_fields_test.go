package node_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/node"
)

// TestGetRun_NamedPropertyFieldImpliesProperties pins #851: fields naming one
// property (properties.<name>) ask for properties like fields=properties.
func TestGetRun_NamedPropertyFieldImpliesProperties(test *testing.T) {
	service, _ := newTestService(test)

	if _, createErr := service.Create(node.CreateInput{RelPath: "notes/a.md", Type: "note", Title: "A"}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	result, runErr := node.GetRun(service, node.GetRequest{ID: "notes/a", Fields: []string{"id", "properties.owner"}})

	if runErr != nil {
		test.Fatalf("GetRun: %v", runErr)
	}

	if !result.IncludeProperties || result.IncludeBody || result.IncludeEdges {
		test.Errorf("GetRun: properties %v, body %v, edges %v; want properties only", result.IncludeProperties, result.IncludeBody, result.IncludeEdges)
	}
}
