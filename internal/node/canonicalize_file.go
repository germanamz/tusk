package node

import (
	"bytes"
	"context"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
)

// CanonicalizeFileOnDisk rewrites a markdown node file in place so any date the
// YAML parser produced as a time.Time (an unquoted on-disk date) is re-emitted
// as a canonical quoted string. The write is lease-protected and
// atomically-renamed, reusing WriteWithLease, and is a no-op when the file
// carries no such date — so reindex self-heal converges after a single rewrite
// (the rewritten file parses back to a string, which CanonicalizeDates leaves
// untouched). Returns whether the file was rewritten.
//
// An ErrBusy from the lease (a concurrent writer such as tusk node modify holds
// it) propagates unchanged; callers skip and let a later pass heal the file.
func CanonicalizeFileOnDisk(
	ctx context.Context,
	root string,
	fileStates *index.FileStateRepo,
	workerID string,
	ttl time.Duration,
	relPath string,
	decls map[string]manifest.NodeType,
) (bool, error) {
	rewrote := false

	mutator := func(current []byte) (Mutation, error) {
		if current == nil {
			return WriteNoChange(), nil
		}

		parsed, parseErr := ParseFile(relPath, current)

		if parseErr != nil {
			return Mutation{}, parseErr
		}

		if !CanonicalizeDates(parsed, decls) {
			return WriteNoChange(), nil
		}

		rendered, renderErr := renderMarkdown(parsed.Properties, parsed.Body)

		if renderErr != nil {
			return Mutation{}, renderErr
		}

		if bytes.Equal(rendered, current) {
			return WriteNoChange(), nil
		}

		rewrote = true

		return WriteReplace(rendered), nil
	}

	if writeErr := WriteWithLease(ctx, root, fileStates, workerID, ttl, relPath, mutator); writeErr != nil {
		return false, writeErr
	}

	return rewrote, nil
}
