package node

import (
	"context"
	"fmt"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/subunit"
)

// syncSubUnits re-derives a markdown page's sub-units from the bytes just
// written for it: their rows and line ranges, the file's contains edges, their
// wikilink edges, and embed jobs for new or changed leaves. Create and Modify
// call it because their writes stamp file_state, so the next reindex skips the
// file and would leave the sub-units describing the bytes before the write
// (#782). It is the reindex worker's markdown sub-unit pass; fileRow is the row
// persistNodeRow just upserted. A nil manifest, one that turns sub-units off,
// or a service without an edge repo skips it.
func (service *Service) syncSubUnits(fileRow index.NodeRow, parsed *Node, file []byte) error {
	if service.manifest == nil || !service.manifest.SubUnitsEnabled() || service.edges == nil {
		return nil
	}

	units, parseErr := subunit.Parse(parsed.Body)

	if parseErr != nil {
		return fmt.Errorf("node: parse sub-units %s: %w", parsed.Path, parseErr)
	}

	// Number the spans against the whole file so line ranges count the
	// frontmatter above the body.
	subunit.AssignLines(units, file, parsed.BodyOffset, service.lineNumbering)

	sync := &subunit.Sync{
		Repo:     service.repo,
		EdgeRepo: service.edges,
		EmbedQ:   service.embedQueue,
		Manifest: service.manifest,
	}

	if _, syncErr := sync.ApplyFile(context.Background(), fileRow, units); syncErr != nil {
		return syncErr
	}

	return nil
}
