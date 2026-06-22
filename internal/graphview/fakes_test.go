package graphview

import (
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

type fakeEdges struct {
	all []index.EdgeRow
}

func (fake *fakeEdges) ListAll() ([]index.EdgeRow, error) { return fake.all, nil }

type fakeChanges struct {
	sig Signal
}

func (fake *fakeChanges) Signal() (Signal, error) { return fake.sig, nil }

// fileRow builds a file-level NodeRow (parent_id NULL).
func fileRow(id, nodeType, title, propsJSON string) index.NodeRow {
	return index.NodeRow{ID: id, Type: nodeType, Title: title, Path: id + ".md", PropertiesJSON: propsJSON}
}

// edge builds an EdgeRow with the given kind.
func edge(edgeType, source, target, kind string) index.EdgeRow {
	return index.EdgeRow{Type: edgeType, SourceID: source, TargetID: target, Kind: kind}
}
