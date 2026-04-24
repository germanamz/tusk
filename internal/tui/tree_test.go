package tui

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
)

func TestBuildTree_RootsOnly(t *testing.T) {
	a := &domain.Task{ID: uuid.New(), ShortID: "aaaaaaaa", Title: "Task A"}
	b := &domain.Task{ID: uuid.New(), ShortID: "bbbbbbbb", Title: "Task B"}

	nodes := buildTree([]*domain.Task{a, b}, nil)
	if len(nodes) != 2 {
		t.Fatalf("expected 2 roots, got %d", len(nodes))
	}
}

func TestBuildTree_ParentAndChildren(t *testing.T) {
	parent := &domain.Task{ID: uuid.New(), ShortID: "aaaaaaaa", Title: "Parent"}
	child1 := &domain.Task{ID: uuid.New(), ShortID: "bbbbbbbb", Title: "Child 1", ParentID: &parent.ID}
	child2 := &domain.Task{ID: uuid.New(), ShortID: "cccccccc", Title: "Child 2", ParentID: &parent.ID}

	nodes := buildTree([]*domain.Task{parent, child1, child2}, nil)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 root, got %d", len(nodes))
	}
	if len(nodes[0].Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(nodes[0].Children))
	}
}

func TestBuildTree_ThreeLevels(t *testing.T) {
	root := &domain.Task{ID: uuid.New(), ShortID: "aaaaaaaa", Title: "Root"}
	child := &domain.Task{ID: uuid.New(), ShortID: "bbbbbbbb", Title: "Child", ParentID: &root.ID}
	grandchild := &domain.Task{ID: uuid.New(), ShortID: "cccccccc", Title: "Grandchild", ParentID: &child.ID}

	nodes := buildTree([]*domain.Task{root, child, grandchild}, nil)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 root, got %d", len(nodes))
	}
	if len(nodes[0].Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(nodes[0].Children))
	}
	if len(nodes[0].Children[0].Children) != 1 {
		t.Fatalf("expected 1 grandchild, got %d", len(nodes[0].Children[0].Children))
	}
}

func TestBuildTree_SubtreeRoot(t *testing.T) {
	root := &domain.Task{ID: uuid.New(), ShortID: "aaaaaaaa", Title: "Root"}
	child := &domain.Task{ID: uuid.New(), ShortID: "bbbbbbbb", Title: "Child", ParentID: &root.ID}

	nodes := buildTree([]*domain.Task{root, child}, &root.ID)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 root, got %d", len(nodes))
	}
	if nodes[0].Task.ShortID != "aaaaaaaa" {
		t.Fatalf("expected root to be aaaaaaaa, got %s", nodes[0].Task.ShortID)
	}
	if len(nodes[0].Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(nodes[0].Children))
	}
}

func TestBuildTree_OrphanedChildren(t *testing.T) {
	missingParentID := uuid.New()
	orphan := &domain.Task{ID: uuid.New(), ShortID: "aaaaaaaa", Title: "Orphan", ParentID: &missingParentID}

	nodes := buildTree([]*domain.Task{orphan}, nil)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 root (orphan promoted), got %d", len(nodes))
	}
}

func TestBuildTree_Empty(t *testing.T) {
	nodes := buildTree([]*domain.Task{}, nil)
	if len(nodes) != 0 {
		t.Fatalf("expected 0 roots, got %d", len(nodes))
	}
}

func TestRenderTree_Text(t *testing.T) {
	root := &domain.Task{ID: uuid.New(), ShortID: "aaaaaaaa", Title: "Root task", Status: "active"}
	child := &domain.Task{ID: uuid.New(), ShortID: "bbbbbbbb", Title: "Child task", Status: "pending", ParentID: &root.ID}
	grandchild := &domain.Task{ID: uuid.New(), ShortID: "cccccccc", Title: "Grandchild", Status: "pending", ParentID: &child.ID}

	nodes := buildTree([]*domain.Task{root, child, grandchild}, nil)

	var buf bytes.Buffer
	r := NewRenderer(&buf, "text", false, nil)
	if err := r.renderTree(nodes); err != nil {
		t.Fatalf("renderTree: %v", err)
	}

	output := buf.String()
	// Root at indent 0
	if !strings.Contains(output, "aaaaaaaa [active] Root task") {
		t.Fatalf("expected root line, got:\n%s", output)
	}
	// Child at indent 2
	if !strings.Contains(output, "  bbbbbbbb [pending] Child task") {
		t.Fatalf("expected child line with 2-space indent, got:\n%s", output)
	}
	// Grandchild at indent 4
	if !strings.Contains(output, "    cccccccc [pending] Grandchild") {
		t.Fatalf("expected grandchild line with 4-space indent, got:\n%s", output)
	}
}

func TestRenderTree_TextEmpty(t *testing.T) {
	var buf bytes.Buffer
	r := NewRenderer(&buf, "text", false, nil)
	if err := r.renderTree(nil); err != nil {
		t.Fatalf("renderTree: %v", err)
	}
	// renderTree itself produces no output for nil nodes;
	// the "No tasks." message is printed by runTree at a higher level.
	if buf.String() != "" {
		t.Fatalf("expected empty output for nil nodes, got %q", buf.String())
	}
}

func TestRenderTree_JSON(t *testing.T) {
	root := &domain.Task{ID: uuid.New(), ShortID: "aaaaaaaa", Title: "Root", Status: "active"}
	child := &domain.Task{ID: uuid.New(), ShortID: "bbbbbbbb", Title: "Child", Status: "pending", ParentID: &root.ID}

	nodes := buildTree([]*domain.Task{root, child}, nil)

	var buf bytes.Buffer
	r := NewRenderer(&buf, "json", false, nil)
	if err := r.renderTree(nodes); err != nil {
		t.Fatalf("renderTree: %v", err)
	}

	var parsed []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("JSON unmarshal: %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("expected 1 root in JSON, got %d", len(parsed))
	}
	if parsed[0]["short_id"] != "aaaaaaaa" {
		t.Fatalf("expected root short_id aaaaaaaa, got %v", parsed[0]["short_id"])
	}
	// parent_id should be present and null for root (not omitted)
	if _, ok := parsed[0]["parent_id"]; !ok {
		t.Fatal("expected parent_id field in root JSON (should be null)")
	}
	if parsed[0]["parent_id"] != nil {
		t.Fatalf("expected null parent_id for root, got %v", parsed[0]["parent_id"])
	}
	children, ok := parsed[0]["children"].([]any)
	if !ok {
		t.Fatalf("expected children array, got %T", parsed[0]["children"])
	}
	if len(children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(children))
	}
}

func TestSortTasks_Order(t *testing.T) {
	o1 := 1.0
	o3 := 3.0
	o2 := 2.0
	a := &domain.Task{ID: uuid.New(), ShortID: "aaaaaaaa", Order: &o3}
	b := &domain.Task{ID: uuid.New(), ShortID: "bbbbbbbb", Order: &o1}
	c := &domain.Task{ID: uuid.New(), ShortID: "cccccccc", Order: &o2}
	tasks := []*domain.Task{a, b, c}
	sortTasks(tasks, "order")
	if tasks[0].ShortID != "bbbbbbbb" || tasks[1].ShortID != "cccccccc" || tasks[2].ShortID != "aaaaaaaa" {
		t.Fatalf("expected order [b,c,a], got %s/%s/%s",
			tasks[0].ShortID, tasks[1].ShortID, tasks[2].ShortID)
	}
}

func TestSortTasks_OrderNullsLast(t *testing.T) {
	o1 := 1.0
	a := &domain.Task{ID: uuid.New(), ShortID: "aaaaaaaa"}
	b := &domain.Task{ID: uuid.New(), ShortID: "bbbbbbbb", Order: &o1}
	tasks := []*domain.Task{a, b}
	sortTasks(tasks, "order")
	if tasks[0].ShortID != "bbbbbbbb" {
		t.Fatalf("expected ordered task first, got %s", tasks[0].ShortID)
	}
}

func TestSortTasks_UrgencyDescending(t *testing.T) {
	a := &domain.Task{ID: uuid.New(), ShortID: "aaaaaaaa", Urgency: 1.0}
	b := &domain.Task{ID: uuid.New(), ShortID: "bbbbbbbb", Urgency: 5.0}
	tasks := []*domain.Task{a, b}
	sortTasks(tasks, "urgency")
	if tasks[0].ShortID != "bbbbbbbb" {
		t.Fatalf("expected urgent task first, got %s", tasks[0].ShortID)
	}
}

func TestValidateSortMode(t *testing.T) {
	for _, v := range []string{"order", "urgency", "created", "priority", "due", ""} {
		if err := validateSortMode(v); err != nil {
			t.Errorf("expected %q to validate, got %v", v, err)
		}
	}
	if err := validateSortMode("bogus"); err == nil {
		t.Error("expected invalid --sort to error")
	}
}

func TestRenderTree_JSONEmpty(t *testing.T) {
	var buf bytes.Buffer
	r := NewRenderer(&buf, "json", false, nil)
	if err := r.renderTree(nil); err != nil {
		t.Fatalf("renderTree: %v", err)
	}
	var parsed []any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("JSON unmarshal: %v", err)
	}
	if len(parsed) != 0 {
		t.Fatalf("expected empty JSON array, got %d elements", len(parsed))
	}
}
