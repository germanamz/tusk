package node_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/node"
)

func newTestService(test *testing.T) (*node.Service, string) {
	test.Helper()

	root := test.TempDir()

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	test.Cleanup(func() { store.Close() })

	service := node.NewService(root, index.NewNodeRepo(store))

	return service, root
}

func TestService_CreateWritesFileAndIndexes(test *testing.T) {
	service, root := newTestService(test)

	created, createErr := service.Create(node.CreateInput{
		RelPath: "tickets/fix-login.md",
		Type:    "ticket",
		Title:   "Fix login",
		Body:    []byte("Some body.\n"),
	})

	if createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	if created.ID != "tickets/fix-login" {
		test.Errorf("ID = %q", created.ID)
	}

	onDisk, readErr := os.ReadFile(filepath.Join(root, "tickets/fix-login.md"))

	if readErr != nil {
		test.Fatalf("read file: %v", readErr)
	}

	if !contains(string(onDisk), "type: ticket") {
		test.Errorf("file missing type: %s", string(onDisk))
	}

	loaded, getErr := service.Get("tickets/fix-login")

	if getErr != nil {
		test.Fatalf("Get: %v", getErr)
	}

	if loaded.Title != "Fix login" {
		test.Errorf("Title = %q", loaded.Title)
	}
}

func TestService_CreateRejectsExistingFile(test *testing.T) {
	service, _ := newTestService(test)

	if _, firstErr := service.Create(node.CreateInput{
		RelPath: "x.md", Type: "note", Body: []byte(""),
	}); firstErr != nil {
		test.Fatalf("first Create: %v", firstErr)
	}

	_, secondErr := service.Create(node.CreateInput{
		RelPath: "x.md", Type: "note", Body: []byte(""),
	})

	if secondErr != node.ErrAlreadyExists {
		test.Errorf("err = %v, want ErrAlreadyExists", secondErr)
	}
}

func TestService_ListReturnsAllNodes(test *testing.T) {
	service, _ := newTestService(test)

	service.Create(node.CreateInput{RelPath: "a.md", Type: "note", Body: []byte("")})
	service.Create(node.CreateInput{RelPath: "b.md", Type: "ticket", Body: []byte("")})

	all, listErr := service.List(node.ListFilter{})

	if listErr != nil {
		test.Fatalf("List: %v", listErr)
	}

	if len(all) != 2 {
		test.Errorf("len = %d, want 2", len(all))
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for offset := 0; offset+len(needle) <= len(haystack); offset++ {
		if haystack[offset:offset+len(needle)] == needle {
			return offset
		}
	}

	return -1
}
