package tui

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
)

func TestBuildTree_RootsOnly(test *testing.T) {
	taskA := &domain.Task{ID: uuid.New(), ShortID: "aaaaaaaa", Title: "Task A"}
	taskB := &domain.Task{ID: uuid.New(), ShortID: "bbbbbbbb", Title: "Task B"}

	nodes := buildTree([]*domain.Task{taskA, taskB}, nil)
	if len(nodes) != 2 {
		test.Fatalf("expected 2 roots, got %d", len(nodes))
	}
}

func TestBuildTree_ParentAndChildren(test *testing.T) {
	parent := &domain.Task{ID: uuid.New(), ShortID: "aaaaaaaa", Title: "Parent"}
	child1 := &domain.Task{ID: uuid.New(), ShortID: "bbbbbbbb", Title: "Child 1", ParentID: &parent.ID}
	child2 := &domain.Task{ID: uuid.New(), ShortID: "cccccccc", Title: "Child 2", ParentID: &parent.ID}

	nodes := buildTree([]*domain.Task{parent, child1, child2}, nil)
	if len(nodes) != 1 {
		test.Fatalf("expected 1 root, got %d", len(nodes))
	}
	if len(nodes[0].Children) != 2 {
		test.Fatalf("expected 2 children, got %d", len(nodes[0].Children))
	}
}

func TestBuildTree_ThreeLevels(test *testing.T) {
	root := &domain.Task{ID: uuid.New(), ShortID: "aaaaaaaa", Title: "Root"}
	child := &domain.Task{ID: uuid.New(), ShortID: "bbbbbbbb", Title: "Child", ParentID: &root.ID}
	grandchild := &domain.Task{ID: uuid.New(), ShortID: "cccccccc", Title: "Grandchild", ParentID: &child.ID}

	nodes := buildTree([]*domain.Task{root, child, grandchild}, nil)
	if len(nodes) != 1 {
		test.Fatalf("expected 1 root, got %d", len(nodes))
	}
	if len(nodes[0].Children) != 1 {
		test.Fatalf("expected 1 child, got %d", len(nodes[0].Children))
	}
	if len(nodes[0].Children[0].Children) != 1 {
		test.Fatalf("expected 1 grandchild, got %d", len(nodes[0].Children[0].Children))
	}
}

func TestBuildTree_SubtreeRoot(test *testing.T) {
	root := &domain.Task{ID: uuid.New(), ShortID: "aaaaaaaa", Title: "Root"}
	child := &domain.Task{ID: uuid.New(), ShortID: "bbbbbbbb", Title: "Child", ParentID: &root.ID}

	nodes := buildTree([]*domain.Task{root, child}, &root.ID)
	if len(nodes) != 1 {
		test.Fatalf("expected 1 root, got %d", len(nodes))
	}
	if nodes[0].Task.ShortID != "aaaaaaaa" {
		test.Fatalf("expected root to be aaaaaaaa, got %s", nodes[0].Task.ShortID)
	}
	if len(nodes[0].Children) != 1 {
		test.Fatalf("expected 1 child, got %d", len(nodes[0].Children))
	}
}

func TestBuildTree_OrphanedChildren(test *testing.T) {
	missingParentID := uuid.New()
	orphan := &domain.Task{ID: uuid.New(), ShortID: "aaaaaaaa", Title: "Orphan", ParentID: &missingParentID}

	nodes := buildTree([]*domain.Task{orphan}, nil)
	if len(nodes) != 1 {
		test.Fatalf("expected 1 root (orphan promoted), got %d", len(nodes))
	}
}

func TestBuildTree_Empty(test *testing.T) {
	nodes := buildTree([]*domain.Task{}, nil)
	if len(nodes) != 0 {
		test.Fatalf("expected 0 roots, got %d", len(nodes))
	}
}

func TestRenderTree_Text(test *testing.T) {
	root := &domain.Task{ID: uuid.New(), ShortID: "aaaaaaaa", Title: "Root task", Status: "active"}
	child := &domain.Task{ID: uuid.New(), ShortID: "bbbbbbbb", Title: "Child task", Status: "pending", ParentID: &root.ID}
	grandchild := &domain.Task{ID: uuid.New(), ShortID: "cccccccc", Title: "Grandchild", Status: "pending", ParentID: &child.ID}

	nodes := buildTree([]*domain.Task{root, child, grandchild}, nil)

	var buf bytes.Buffer
	renderer := NewRenderer(&buf, "text", false, nil)
	if err := renderer.renderTree(nodes); err != nil {
		test.Fatalf("renderTree: %v", err)
	}

	output := buf.String()
	// Root at indent 0
	if !strings.Contains(output, "aaaaaaaa [active] Root task") {
		test.Fatalf("expected root line, got:\n%s", output)
	}
	// Child at indent 2
	if !strings.Contains(output, "  bbbbbbbb [pending] Child task") {
		test.Fatalf("expected child line with 2-space indent, got:\n%s", output)
	}
	// Grandchild at indent 4
	if !strings.Contains(output, "    cccccccc [pending] Grandchild") {
		test.Fatalf("expected grandchild line with 4-space indent, got:\n%s", output)
	}
}

func TestRenderTree_TextEmpty(test *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, "text", false, nil)
	if err := renderer.renderTree(nil); err != nil {
		test.Fatalf("renderTree: %v", err)
	}
	// renderTree itself produces no output for nil nodes;
	// the "No tasks." message is printed by runTree at a higher level.
	if buf.String() != "" {
		test.Fatalf("expected empty output for nil nodes, got %q", buf.String())
	}
}

func TestRenderTree_JSON(test *testing.T) {
	root := &domain.Task{ID: uuid.New(), ShortID: "aaaaaaaa", Title: "Root", Status: "active"}
	child := &domain.Task{ID: uuid.New(), ShortID: "bbbbbbbb", Title: "Child", Status: "pending", ParentID: &root.ID}

	nodes := buildTree([]*domain.Task{root, child}, nil)

	var buf bytes.Buffer
	renderer := NewRenderer(&buf, "json", false, nil)
	if err := renderer.renderTree(nodes); err != nil {
		test.Fatalf("renderTree: %v", err)
	}

	var parsed []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		test.Fatalf("JSON unmarshal: %v", err)
	}
	if len(parsed) != 1 {
		test.Fatalf("expected 1 root in JSON, got %d", len(parsed))
	}
	if parsed[0]["short_id"] != "aaaaaaaa" {
		test.Fatalf("expected root short_id aaaaaaaa, got %v", parsed[0]["short_id"])
	}
	// parent_id should be present and null for root (not omitted)
	if _, ok := parsed[0]["parent_id"]; !ok {
		test.Fatal("expected parent_id field in root JSON (should be null)")
	}
	if parsed[0]["parent_id"] != nil {
		test.Fatalf("expected null parent_id for root, got %v", parsed[0]["parent_id"])
	}
	children, ok := parsed[0]["children"].([]any)
	if !ok {
		test.Fatalf("expected children array, got %T", parsed[0]["children"])
	}
	if len(children) != 1 {
		test.Fatalf("expected 1 child, got %d", len(children))
	}
}

func TestSortTasks_Order(test *testing.T) {
	o1 := 1.0
	o3 := 3.0
	o2 := 2.0
	taskA := &domain.Task{ID: uuid.New(), ShortID: "aaaaaaaa", Order: &o3}
	taskB := &domain.Task{ID: uuid.New(), ShortID: "bbbbbbbb", Order: &o1}
	taskC := &domain.Task{ID: uuid.New(), ShortID: "cccccccc", Order: &o2}
	tasks := []*domain.Task{taskA, taskB, taskC}
	sortTasks(tasks, "order")
	if tasks[0].ShortID != "bbbbbbbb" || tasks[1].ShortID != "cccccccc" || tasks[2].ShortID != "aaaaaaaa" {
		test.Fatalf("expected order [b,c,a], got %s/%s/%s",
			tasks[0].ShortID, tasks[1].ShortID, tasks[2].ShortID)
	}
}

func TestSortTasks_OrderNullsLast(test *testing.T) {
	o1 := 1.0
	taskA := &domain.Task{ID: uuid.New(), ShortID: "aaaaaaaa"}
	taskB := &domain.Task{ID: uuid.New(), ShortID: "bbbbbbbb", Order: &o1}
	tasks := []*domain.Task{taskA, taskB}
	sortTasks(tasks, "order")
	if tasks[0].ShortID != "bbbbbbbb" {
		test.Fatalf("expected ordered task first, got %s", tasks[0].ShortID)
	}
}

func TestSortTasks_UrgencyDescending(test *testing.T) {
	taskA := &domain.Task{ID: uuid.New(), ShortID: "aaaaaaaa", Urgency: 1.0}
	taskB := &domain.Task{ID: uuid.New(), ShortID: "bbbbbbbb", Urgency: 5.0}
	tasks := []*domain.Task{taskA, taskB}
	sortTasks(tasks, "urgency")
	if tasks[0].ShortID != "bbbbbbbb" {
		test.Fatalf("expected urgent task first, got %s", tasks[0].ShortID)
	}
}

func TestValidateSortMode(test *testing.T) {
	for _, mode := range []string{"order", "urgency", "created", "priority", "due", ""} {
		if err := validateSortMode(mode); err != nil {
			test.Errorf("expected %q to validate, got %v", mode, err)
		}
	}
	if err := validateSortMode("bogus"); err == nil {
		test.Error("expected invalid --sort to error")
	}
}

func TestRenderTree_JSONEmpty(test *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, "json", false, nil)
	if err := renderer.renderTree(nil); err != nil {
		test.Fatalf("renderTree: %v", err)
	}
	var parsed []any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		test.Fatalf("JSON unmarshal: %v", err)
	}
	if len(parsed) != 0 {
		test.Fatalf("expected empty JSON array, got %d elements", len(parsed))
	}
}
