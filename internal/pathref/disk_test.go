package pathref_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/pathref"
)

func diskFixture(test *testing.T) string {
	test.Helper()

	root := test.TempDir()

	for _, dir := range []string{"server/ledger/core", "docs"} {
		if mkErr := os.MkdirAll(filepath.Join(root, dir), 0o755); mkErr != nil {
			test.Fatalf("mkdir: %v", mkErr)
		}
	}

	for _, file := range []string{"server/ledger/core/service.go", "go.mod"} {
		if writeErr := os.WriteFile(filepath.Join(root, file), []byte("x"), 0o644); writeErr != nil {
			test.Fatalf("write: %v", writeErr)
		}
	}

	return root
}

func TestDisk_AnchoredAndPresent(test *testing.T) {
	disk := pathref.NewDisk(diskFixture(test))

	cases := []struct {
		target   string
		anchored bool
		present  bool
	}{
		{"server/ledger/core/service.go", true, true},
		{"server/ledger", true, true},
		{"server/ledger/core/gone.go", true, false},
		{"server/renamed/x.go", true, false},
		{"go.mod", true, true},
		{"go.sum", false, false},
		{"go.mod/inner", false, false},
		{"application/json", false, false},
		{"Server/ledger", false, false},
		{"server/Ledger", true, false},
	}

	for _, entry := range cases {
		if got := disk.Anchored(entry.target); got != entry.anchored {
			test.Errorf("Anchored(%q) = %v, want %v", entry.target, got, entry.anchored)
		}

		if got := disk.Present(entry.target); got != entry.present {
			test.Errorf("Present(%q) = %v, want %v", entry.target, got, entry.present)
		}
	}
}

func TestDisk_FollowsDirectorySymlinks(test *testing.T) {
	root := diskFixture(test)

	if linkErr := os.Symlink(filepath.Join(root, "server"), filepath.Join(root, "srv")); linkErr != nil {
		test.Skipf("symlink: %v", linkErr)
	}

	disk := pathref.NewDisk(root)

	if !disk.Anchored("srv/ledger") || !disk.Present("srv/ledger/core/service.go") {
		test.Errorf("a symlinked directory should anchor and resolve")
	}
}

func TestDisk_UnreadableDirectoryCountsAsPresent(test *testing.T) {
	if os.Geteuid() == 0 {
		test.Skip("root reads every directory")
	}

	root := diskFixture(test)
	locked := filepath.Join(root, "server/ledger")

	if chmodErr := os.Chmod(locked, 0o000); chmodErr != nil {
		test.Fatalf("chmod: %v", chmodErr)
	}

	test.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	if !pathref.NewDisk(root).Present("server/ledger/core/anything.go") {
		test.Errorf("an unreadable directory can't prove a path missing")
	}
}
