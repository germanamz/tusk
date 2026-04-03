package tui

import (
	"testing"

	"github.com/germanamz/tusk/internal/domain"
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
