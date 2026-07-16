package bookview

import (
	"sort"

	"github.com/germanamz/tusk/internal/index"
)

// fakeNodes is a NodeSource test double over a fixed set of file-level rows.
// Every method has a value receiver, so a bare literal (fakeNodes{file: ...})
// satisfies NodeSource without needing to be constructed behind a pointer —
// there is no mutable state to guard, so this stays goroutine-safe by
// construction.
type fakeNodes struct {
	file []index.NodeRow
}

// ListFileNodes returns the fixture verbatim, mirroring *index.NodeRepo's
// contract of already being filtered and ordered by the caller (or, here, the
// test) rather than re-deriving that in the fake.
func (fake fakeNodes) ListFileNodes() ([]index.NodeRow, error) {
	return fake.file, nil
}

// Get mirrors *index.NodeRepo.Get: ErrNodeNotFound, the bare sentinel, for a
// missing id.
func (fake fakeNodes) Get(nodeID string) (*index.NodeRow, error) {
	for _, row := range fake.file {
		if row.ID == nodeID {
			found := row

			return &found, nil
		}
	}

	return nil, index.ErrNodeNotFound
}

// ListByIDs mirrors *index.NodeRepo.ListByIDs: empty input returns (nil, nil)
// with no lookup performed, ids with no matching row are silently omitted
// rather than erroring, and the result is ordered by id ASC regardless of the
// order ids were requested in.
func (fake fakeNodes) ListByIDs(ids []string) ([]index.NodeRow, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	byID := make(map[string]index.NodeRow, len(fake.file))

	for _, row := range fake.file {
		byID[row.ID] = row
	}

	out := make([]index.NodeRow, 0, len(ids))

	for _, id := range ids {
		if row, ok := byID[id]; ok {
			out = append(out, row)
		}
	}

	sort.Slice(out, func(left, right int) bool { return out[left].ID < out[right].ID })

	return out, nil
}

// FindByTitle mirrors *index.NodeRepo.FindByTitle: an absent title returns
// (nil, nil), never an error, targetType "*" matches any type, and the result
// is ordered by id ASC.
func (fake fakeNodes) FindByTitle(targetType, title string) ([]string, error) {
	var ids []string

	for _, row := range fake.file {
		if row.Title != title {
			continue
		}

		if targetType != "*" && row.Type != targetType {
			continue
		}

		ids = append(ids, row.ID)
	}

	sort.Strings(ids)

	return ids, nil
}

// fakeEdges is an EdgeSource test double over a fixed edge set. Like fakeNodes
// it uses value receivers, so a bare literal satisfies EdgeSource and there is
// no mutable state to guard.
//
// It reproduces *index.EdgeRepo's per-method ordering rather than returning the
// fixture verbatim: ListBySource orders by (type, target_id) and ListByTarget by
// (type, source_id) (edge_repo.go:172,177). Neither is ListAll's global
// (source_id, type, target_id) order, which is what webui.Neighbors re-sorts to
// — a fake that returned rows in fixture order would let a caller depending on
// that re-sort pass here and reorder in production.
type fakeEdges struct {
	all []index.EdgeRow
}

// ListBySource mirrors *index.EdgeRepo.ListBySource: every edge whose source_id
// matches, ordered by type then target_id. It does not filter by Kind —
// structural contains rows come back alongside direct and derived ones.
func (fake fakeEdges) ListBySource(sourceID string) ([]index.EdgeRow, error) {
	out := make([]index.EdgeRow, 0)

	for _, row := range fake.all {
		if row.SourceID == sourceID {
			out = append(out, row)
		}
	}

	sort.SliceStable(out, func(left, right int) bool {
		if out[left].Type != out[right].Type {
			return out[left].Type < out[right].Type
		}

		return out[left].TargetID < out[right].TargetID
	})

	return out, nil
}

// ListByTarget mirrors *index.EdgeRepo.ListByTarget: every edge whose target_id
// matches, ordered by type then source_id. Like ListBySource it does not filter
// by Kind.
func (fake fakeEdges) ListByTarget(targetID string) ([]index.EdgeRow, error) {
	out := make([]index.EdgeRow, 0)

	for _, row := range fake.all {
		if row.TargetID == targetID {
			out = append(out, row)
		}
	}

	sort.SliceStable(out, func(left, right int) bool {
		if out[left].Type != out[right].Type {
			return out[left].Type < out[right].Type
		}

		return out[left].SourceID < out[right].SourceID
	})

	return out, nil
}
