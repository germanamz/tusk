package node

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
)

func ticketDateDecls() map[string]manifest.NodeType {
	return map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{
			{Name: "due", Type: "date"},
			{Name: "remind_at", Type: "datetime"},
		}},
	}
}

func seedUnquotedDateFile(test *testing.T, root, relPath string) {
	test.Helper()

	if mkErr := os.MkdirAll(filepath.Dir(filepath.Join(root, relPath)), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	body := "---\ntype: ticket\ndue: 2026-06-11\nremind_at: 2026-06-10T09:00:00Z\n---\n\nbody\n"

	if writeErr := os.WriteFile(filepath.Join(root, relPath), []byte(body), 0o644); writeErr != nil {
		test.Fatalf("seed: %v", writeErr)
	}
}

func TestCanonicalizeFileOnDisk_QuotesDateAndDatetimeAndConverges(test *testing.T) {
	root := test.TempDir()
	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	defer store.Close()

	fileStates := index.NewFileStateRepo(store)
	relPath := "tickets/foo.md"
	seedUnquotedDateFile(test, root, relPath)

	healed, healErr := CanonicalizeFileOnDisk(
		context.Background(), root, fileStates, "worker-1", time.Minute, relPath, ticketDateDecls(),
	)

	if healErr != nil {
		test.Fatalf("CanonicalizeFileOnDisk: %v", healErr)
	}

	if !healed {
		test.Fatalf("expected healed=true on unquoted dates")
	}

	body, _ := os.ReadFile(filepath.Join(root, relPath))

	if !strings.Contains(string(body), `due: "2026-06-11"`) {
		test.Errorf("date not quoted:\n%s", body)
	}

	if !strings.Contains(string(body), `remind_at: "2026-06-10T09:00:00Z"`) {
		test.Errorf("datetime not quoted:\n%s", body)
	}

	// Convergence: a second pass finds no time.Time and rewrites nothing.
	healedAgain, healErr := CanonicalizeFileOnDisk(
		context.Background(), root, fileStates, "worker-1", time.Minute, relPath, ticketDateDecls(),
	)

	if healErr != nil {
		test.Fatalf("second CanonicalizeFileOnDisk: %v", healErr)
	}

	if healedAgain {
		test.Errorf("expected healed=false on an already-canonical file (convergence)")
	}
}

func TestCanonicalizeFileOnDisk_ErrBusySkips(test *testing.T) {
	root := test.TempDir()
	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	defer store.Close()

	fileStates := index.NewFileStateRepo(store)
	relPath := "tickets/foo.md"
	seedUnquotedDateFile(test, root, relPath)

	// A concurrent writer holds the lease.
	if ensureErr := fileStates.EnsurePlaceholder(relPath); ensureErr != nil {
		test.Fatalf("placeholder: %v", ensureErr)
	}

	if _, claimErr := fileStates.Claim(relPath, "other-worker", time.Minute); claimErr != nil {
		test.Fatalf("pre-claim: %v", claimErr)
	}

	healed, healErr := CanonicalizeFileOnDisk(
		context.Background(), root, fileStates, "worker-1", time.Minute, relPath, ticketDateDecls(),
	)

	if !errors.Is(healErr, index.ErrBusy) {
		test.Fatalf("expected ErrBusy, got %v", healErr)
	}

	if healed {
		test.Errorf("expected healed=false when the lease is busy")
	}

	// The file is left untouched (unquoted) for a later pass to heal.
	body, _ := os.ReadFile(filepath.Join(root, relPath))

	if !strings.Contains(string(body), "due: 2026-06-11\n") {
		test.Errorf("busy lease should leave the file unwritten; got:\n%s", body)
	}
}
