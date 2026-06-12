package node_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

// newHTMLNodeService builds a lease-configured Service plus the NodeRepo behind
// it, so a test can seed an HTML file row directly (the service's Create path
// rejects HTML, mirroring how HTML files arrive — dropped in and indexed by the
// engine, not authored through the service).
func newHTMLNodeService(test *testing.T) (*node.Service, *index.NodeRepo, string) {
	test.Helper()

	root := test.TempDir()

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	test.Cleanup(func() { store.Close() })

	repo := index.NewNodeRepo(store)

	service := node.NewServiceWithLease(
		root,
		repo,
		nil,
		manifest.EdgeTypes{},
		nil,
		index.NewFileStateRepo(store),
		"test-worker",
		time.Minute,
	)

	return service, repo, root
}

func seedHTMLNode(test *testing.T, repo *index.NodeRepo, root, relPath, content string) {
	test.Helper()

	if mkErr := os.MkdirAll(filepath.Dir(filepath.Join(root, relPath)), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	if writeErr := os.WriteFile(filepath.Join(root, relPath), []byte(content), 0o644); writeErr != nil {
		test.Fatalf("write file: %v", writeErr)
	}

	if upErr := repo.Upsert(index.NodeRow{
		ID:             relPath,
		Type:           "note",
		Path:           relPath,
		Title:          "Sample",
		PropertiesJSON: "{}",
		LastChecksum:   "seed",
	}); upErr != nil {
		test.Fatalf("seed upsert: %v", upErr)
	}
}

func TestService_GetHTMLNode(test *testing.T) {
	service, repo, root := newHTMLNodeService(test)

	seedHTMLNode(test, repo, root, "pages/sample.html",
		`<html><head><meta name="tusk:type" content="note"><title>Sample</title></head>`+
			`<body><h1>Hi</h1><p>Hello &amp; bye</p></body></html>`)

	// Before the fix this returned ErrMissingFrontmatter (Get parsed every file
	// as markdown).
	loaded, getErr := service.Get("pages/sample.html")

	if getErr != nil {
		test.Fatalf("Get HTML node: %v", getErr)
	}

	if loaded.ID != "pages/sample.html" {
		test.Errorf("ID = %q, want pages/sample.html", loaded.ID)
	}

	if loaded.Type != "note" {
		test.Errorf("Type = %q, want note", loaded.Type)
	}

	if loaded.Title != "Sample" {
		test.Errorf("Title = %q, want Sample", loaded.Title)
	}

	if want := "Hi\n\nHello & bye"; string(loaded.Body) != want {
		test.Errorf("Body = %q, want %q", string(loaded.Body), want)
	}
}

func TestService_CreateHTMLNodeRejected(test *testing.T) {
	service, _, _ := newHTMLNodeService(test)

	_, createErr := service.Create(node.CreateInput{RelPath: "pages/new.html", Type: "note"})

	if !errors.Is(createErr, node.ErrHTMLNodeNotEditable) {
		test.Fatalf("Create HTML = %v, want ErrHTMLNodeNotEditable", createErr)
	}
}

func TestService_ModifyHTMLNodeRejected(test *testing.T) {
	service, repo, root := newHTMLNodeService(test)

	seedHTMLNode(test, repo, root, "pages/sample.html",
		`<html><head><meta name="tusk:type" content="note"></head><body><p>x</p></body></html>`)

	_, modErr := service.Modify(node.ModifyInput{
		ID:       "pages/sample.html",
		SetProps: map[string]any{"status": "draft"},
	})

	if !errors.Is(modErr, node.ErrHTMLNodeNotEditable) {
		test.Fatalf("Modify HTML = %v, want ErrHTMLNodeNotEditable", modErr)
	}
}
