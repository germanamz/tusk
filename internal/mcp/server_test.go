package mcp_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/mcp"
)

func TestNewServer_ReturnsServer(test *testing.T) {
	root := test.TempDir()

	os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(`[workspace]
name = "x"
`), 0o644)

	rt, _ := mcp.Open(root)
	defer rt.Close()

	srv := mcp.NewServer(rt)

	if srv == nil {
		test.Fatalf("NewServer returned nil")
	}
}
