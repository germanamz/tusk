package bookview

import (
	"sort"
	"strings"

	"github.com/germanamz/tusk/internal/index"
)

// nodeLinks holds one node's reading rails, split by direction.
type nodeLinks struct {
	out []LinkRef
	in  []LinkRef
}

// adjacentEdge pairs an edge touching the focus node with the id of its far end
// and the direction that end sits in ("out" when the focus is the edge's
// source, "in" when it is the target).
type adjacentEdge struct {
	edge      index.EdgeRow
	farID     string
	direction string
}

// linkRail accumulates one direction's rail, dropping duplicates as they
// arrive. It keys on the LinkRef value itself rather than a synthetic id: Title
// and Type are both functions of ID, so two refs that compare equal serialize to
// identical bytes and the second carries no information a reader could act on.
// That is what collapses the N sections of one note all linking to the same
// target down to a single entry, and it is why the de-duplication cannot lose a
// link that the file-level rails used to show.
type linkRail struct {
	refs []LinkRef
	seen map[LinkRef]struct{}
}

// newLinkRail seeds the rail with a non-nil slice so an isolated node's links
// marshal to [] rather than null, which would force every frontend consumer to
// null-check before iterating.
func newLinkRail() linkRail {
	return linkRail{refs: make([]LinkRef, 0), seen: make(map[LinkRef]struct{})}
}

func (rail *linkRail) add(ref LinkRef) {
	if _, ok := rail.seen[ref]; ok {
		return
	}

	rail.seen[ref] = struct{}{}
	rail.refs = append(rail.refs, ref)
}

// linksOf returns the reading rails of nodeID, projected to file level.
//
// It walks the index itself rather than calling webui.Neighbors, because that
// function drops every neighbor whose far end is a sub-unit — the file-level
// rule the graph view is built on. Sub-units source real edges (subunit sync
// derives index.EdgeRow{SourceID: "<file>#<section>", Kind: "direct"} rows), so
// under that rule a note whose backlinks all originate in other notes' sections
// reports an empty Backlinks rail while looking like it worked. A reading UI
// cannot under-report "what links here".
//
// Instead each sub-unit far end is rolled up to the file it belongs to: a link
// authored in c#S1 — or in a paragraph nested under it, c#S1P1 — is a link from
// note c, so it surfaces as c, navigable and reading the way a person expects.
// Rolling up makes duplicates possible where the file-level rule made them
// unreachable (three sections of c linking to a are three edges but one link
// from c), so each rail de-duplicates.
//
// The behaviors webui.Neighbors guaranteed are preserved here and pinned by
// this package's tests: only incident edges are fetched (never a full scan), far
// ends resolve in one batched ListByIDs lookup rather than a Get per edge,
// self-loops are emitted once as "out", dangling far ends are skipped, and the
// order reproduces ListAll's global (source_id, type, target_id).
//
// Structural ("contains") edges are excluded: they are index plumbing rather
// than a link a reader follows, and containment is what the Contents pane
// expresses. The filter is on Edge.Kind, not on Type == "contains", so a
// user-authored `contains:` frontmatter ref (which lands as Kind "direct")
// still reaches the rails. It matters more after rollup than before: a file
// focus's own contains edge points at a sub-unit that now rolls back up to the
// focus itself, so without the Kind filter every note would link to itself.
//
// graphview deliberately keeps both the sub-unit drop and the structural edges,
// which is why all of this policy lives in bookview and webui.Neighbors stays
// policy-free.
func (srv *Server) linksOf(nodeID string) (nodeLinks, error) {
	adjacent, adjErr := srv.adjacentEdges(nodeID)

	if adjErr != nil {
		return nodeLinks{}, adjErr
	}

	farFiles, resolveErr := srv.resolveFarFiles(adjacent)

	if resolveErr != nil {
		return nodeLinks{}, resolveErr
	}

	out, in := newLinkRail(), newLinkRail()

	for _, adj := range adjacent {
		if adj.edge.Kind == "structural" {
			continue
		}

		far, found := farFiles[adj.farID]

		if !found {
			continue // dangling far end, or a sub-unit whose file row is gone
		}

		// The far end was a sub-unit of the focus itself, and rolling it up
		// landed back on the focus. Emitting it would invent a self-link the
		// file-level rails never had, and on the "in" side it would contradict
		// the rule that a self-loop appears exactly once, as "out". The focus
		// node's own sub-units are a separate reading affordance; this is the
		// far-end projection only. A genuine file-level self-loop is untouched:
		// nothing was rolled up, so farID still equals nodeID.
		if far.ID == nodeID && adj.farID != nodeID {
			continue
		}

		ref := LinkRef{
			ID:       far.ID,
			Title:    far.Title,
			Type:     far.Type,
			EdgeType: adj.edge.Type,
		}

		if adj.direction == "out" {
			out.add(ref)

			continue
		}

		in.add(ref)
	}

	return nodeLinks{out: out.refs, in: in.refs}, nil
}

// adjacentEdges collects the edges incident to nodeID (ListBySource +
// ListByTarget, never a full scan) and re-sorts them into ListAll's global
// (source_id, type, target_id) order, which neither per-method ordering
// reproduces on its own.
func (srv *Server) adjacentEdges(nodeID string) ([]adjacentEdge, error) {
	outEdges, outErr := srv.deps.Edges.ListBySource(nodeID)

	if outErr != nil {
		return nil, outErr
	}

	inEdges, inErr := srv.deps.Edges.ListByTarget(nodeID)

	if inErr != nil {
		return nil, inErr
	}

	adjacent := make([]adjacentEdge, 0, len(outEdges)+len(inEdges))

	for _, row := range outEdges {
		adjacent = append(adjacent, adjacentEdge{edge: row, farID: row.TargetID, direction: "out"})
	}

	for _, row := range inEdges {
		// A self-loop (source_id == target_id == nodeID) is returned by both
		// ListBySource and ListByTarget; ListAll yields it once, classified as
		// "out" (the source case wins). Skip it here to avoid a double count.
		if row.SourceID == nodeID {
			continue
		}

		adjacent = append(adjacent, adjacentEdge{edge: row, farID: row.SourceID, direction: "in"})
	}

	sort.SliceStable(adjacent, func(left, right int) bool {
		lhs, rhs := adjacent[left].edge, adjacent[right].edge

		if lhs.SourceID != rhs.SourceID {
			return lhs.SourceID < rhs.SourceID
		}

		if lhs.Type != rhs.Type {
			return lhs.Type < rhs.Type
		}

		return lhs.TargetID < rhs.TargetID
	})

	return adjacent, nil
}

// fileIDOf projects any node id onto the id of the file it belongs to. A
// sub-unit id is "<fileID>#<address>", so everything before the first separator
// is the file; a file id has no separator to cut on and comes back unchanged.
// That is guaranteed rather than incidental — index.ReservedIDReason rejects "#"
// in a file id at both the reindex walk and the node write surface, precisely so
// a file id can never be mistaken for a sub-unit of another file (#683).
//
// This lands on the file in ONE step at ANY nesting depth, which is why the
// rails do not walk parent_id. Hopping parent_id is what the first cut of this
// code did, on the theory that the schema pins a sub-unit's parent to a file
// row. It does not: nodes' CHECK constrains only whether parent_id is NULL, never
// what it points at, and subunit sync sets a nested unit's parent to its
// enclosing SECTION, not to its file (parentRowID, internal/subunit/sync.go —
// it returns the file id only for units at the document root). A paragraph under
// a heading has parent_id "<file>#<section>", so one hop lands on another
// sub-unit; deeper nesting is further still. Since prose lives under headings,
// that is where most real links are authored.
//
// internal/subunit's addressFromID is the exact inverse of this cut, which is
// what makes it the established way to read a sub-unit id rather than a new
// assumption about the id format.
func fileIDOf(nodeID string) string {
	fileID, _, _ := strings.Cut(nodeID, index.SubUnitIDSeparator)

	return fileID
}

// resolveFarFiles maps each distinct far-end id to the file-level node row the
// rails should show for it: the row itself when it is already a file, the file it
// belongs to when it is a sub-unit at any depth.
//
// An id absent from the returned map is one the rails must skip: the file behind
// it has no row, so the edge is dangling (ListByIDs silently omits ids with no
// row rather than erroring). Emitting it would render a dead rail entry — a
// zero-value LinkRef with an empty title pointing at an id that 404s.
func (srv *Server) resolveFarFiles(adjacent []adjacentEdge) (map[string]index.NodeRow, error) {
	farIDs := make([]string, 0, len(adjacent))

	for _, adj := range adjacent {
		farIDs = append(farIDs, adj.farID)
	}

	return srv.fileRowsFor(farIDs)
}

// fileRowsFor maps each id in ids to the file-level node row it belongs to: the
// row itself when the id already names a file, the file it belongs to when it
// names a sub-unit at any depth (see fileIDOf). Ids are de-duplicated, so
// repeats cost nothing and callers need not pre-filter.
//
// It costs one batched ListByIDs call regardless of how many ids are asked for,
// rather than a Get per id: an id's file id comes from the id alone, so there is
// never a need to fetch a sub-unit row just to read a parent pointer off it.
//
// An id whose file has no row is ABSENT from the result rather than mapped to a
// zero row — ListByIDs silently omits ids it cannot find rather than erroring,
// so this is the only signal a caller gets that nothing navigable stands behind
// the id. Note what is deliberately NOT verified: the sub-unit row itself. The
// file is the authority, which is what lets a link authored in c#S1 resolve in a
// workspace that does not index sub-units at all.
//
// Shared by the reading rails (far-end projection) and the body's wikilink
// resolution, which roll up by the same rule and must agree: a rail entry and an
// inline link pointing at the same section have to reach the same note.
func (srv *Server) fileRowsFor(ids []string) (map[string]index.NodeRow, error) {
	fileIDByID := make(map[string]string, len(ids))
	fileIDs := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))

	for _, id := range ids {
		fileID := fileIDOf(id)
		fileIDByID[id] = fileID

		if _, ok := seen[fileID]; ok {
			continue // asked for already: the batched lookup wants each id once
		}

		seen[fileID] = struct{}{}
		fileIDs = append(fileIDs, fileID)
	}

	fileRows, fileErr := srv.deps.Nodes.ListByIDs(fileIDs)

	if fileErr != nil {
		return nil, fileErr
	}

	byID := make(map[string]index.NodeRow, len(fileRows))

	for _, row := range fileRows {
		byID[row.ID] = row
	}

	resolved := make(map[string]index.NodeRow, len(fileIDByID))

	for id, fileID := range fileIDByID {
		if row, found := byID[fileID]; found {
			resolved[id] = row
		}
	}

	return resolved, nil
}
