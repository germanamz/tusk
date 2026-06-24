package graphview

import (
	"sort"
	"sync"

	"github.com/germanamz/tusk/internal/index"
)

type fakeNodes struct {
	files    []index.NodeRow
	byID     map[string]index.NodeRow
	children map[string][]index.NodeRow
}

func (fake *fakeNodes) ListFileNodes() ([]index.NodeRow, error) { return fake.files, nil }
func (fake *fakeNodes) CountFileNodes() (int, error)            { return len(fake.files), nil }

func (fake *fakeNodes) Get(nodeID string) (*index.NodeRow, error) {
	row, ok := fake.byID[nodeID]
	if !ok {
		return nil, index.ErrNodeNotFound
	}

	return &row, nil
}

func (fake *fakeNodes) ListByParent(parentID string) ([]index.NodeRow, error) {
	return fake.children[parentID], nil
}

// ListByIDs mirrors *index.NodeRepo.ListByIDs: returns matching rows ordered by
// id ASC, silently omitting ids with no row.
func (fake *fakeNodes) ListByIDs(ids []string) ([]index.NodeRow, error) {
	out := make([]index.NodeRow, 0, len(ids))

	for _, id := range ids {
		if row, ok := fake.byID[id]; ok {
			out = append(out, row)
		}
	}

	sort.SliceStable(out, func(left, right int) bool {
		return out[left].ID < out[right].ID
	})

	return out, nil
}

type fakeEdges struct {
	all []index.EdgeRow
}

func (fake *fakeEdges) ListAll() ([]index.EdgeRow, error) { return fake.all, nil }

// ListBySource mirrors *index.EdgeRepo.ListBySource: edges with the given
// source_id ordered by type, then target_id.
func (fake *fakeEdges) ListBySource(sourceID string) ([]index.EdgeRow, error) {
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

// ListByTarget mirrors *index.EdgeRepo.ListByTarget: edges with the given
// target_id ordered by type, then source_id.
func (fake *fakeEdges) ListByTarget(targetID string) ([]index.EdgeRow, error) {
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

type fakeChanges struct {
	mu  sync.Mutex
	sig Signal
}

func (fake *fakeChanges) Signal() (Signal, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()

	return fake.sig, nil
}

func (fake *fakeChanges) setSig(sig Signal) {
	fake.mu.Lock()
	fake.sig = sig
	fake.mu.Unlock()
}

// fileRow builds a file-level NodeRow (parent_id NULL).
func fileRow(id, nodeType, title, propsJSON string) index.NodeRow {
	return index.NodeRow{ID: id, Type: nodeType, Title: title, Path: id + ".md", PropertiesJSON: propsJSON}
}

// edge builds an EdgeRow with the given kind.
func edge(edgeType, source, target, kind string) index.EdgeRow {
	return index.EdgeRow{Type: edgeType, SourceID: source, TargetID: target, Kind: kind}
}
