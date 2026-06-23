// Package graphview serves a read-only 3D graph view of the vault over a
// loopback HTTP server. It receives an already-open workspace handle via Deps
// and never opens the workspace or imports internal/mcp itself.
package graphview

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/germanamz/tusk/internal/index"
)

// DefaultAddr is the loopback bind address for `tusk graph`. No prior
// precedent existed (MCP SSE defaults to :8765 over an opt-in transport); 7373
// is chosen as an unused loopback port.
const DefaultAddr = "127.0.0.1:7373"

// Signal is the dual change signal that drives live updates. Generation comes
// from the SQLite meta key "reindex_gen" (advances on every content reindex);
// Epoch comes from .tusk/epoch (advances only on reset/rebuild).
type Signal struct {
	Generation int64 `json:"generation"`
	Epoch      int64 `json:"epoch"`
}

// GraphNode is one file-level node in the snapshot.
type GraphNode struct {
	ID     string   `json:"id"`
	Type   string   `json:"type"`
	Title  string   `json:"title"`
	Path   string   `json:"path"`
	Tags   []string `json:"tags"`
	Degree int      `json:"degree"`
}

// GraphEdge is one edge between two file-level nodes. Kind is the index
// edge-kind: "direct" (user frontmatter edges), "derived" (wikilinks), or
// "structural" (sub-unit contains).
type GraphEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"`
	Kind   string `json:"kind"`
}

// Graph is the /api/graph snapshot payload.
type Graph struct {
	Generation int64       `json:"generation"`
	Epoch      int64       `json:"epoch"`
	Nodes      []GraphNode `json:"nodes"`
	Edges      []GraphEdge `json:"edges"`
}

// Neighbor is an adjacent node in a NodeDetail.
type Neighbor struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	EdgeType  string `json:"edge_type"`
	Kind      string `json:"kind"`
	Direction string `json:"direction"` // "out" (id is source) or "in" (id is target)
}

// NodeDetail is the /api/node/{id} inspect payload.
type NodeDetail struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Title      string          `json:"title"`
	Path       string          `json:"path"`
	Properties json.RawMessage `json:"properties"`
	Rendered   string          `json:"rendered"`
	Neighbors  []Neighbor      `json:"neighbors"`
}

// SubunitGraph is the /api/node/{id}/subunits drill-down payload.
type SubunitGraph struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

// QueryInput is the /api/query request.
type QueryInput struct {
	Filter   string `json:"filter"`
	Semantic string `json:"q"`
	Limit    int    `json:"limit"`
}

// Match is one ranked result.
type Match struct {
	ID    string  `json:"id"`
	Score float64 `json:"score"`
}

// NodeSource lists and fetches nodes. Satisfied by *index.NodeRepo.
type NodeSource interface {
	ListFileNodes() ([]index.NodeRow, error)
	Get(nodeID string) (*index.NodeRow, error)
	ListByParent(parentID string) ([]index.NodeRow, error)
}

// EdgeSource lists edges. Satisfied by *index.EdgeRepo.
type EdgeSource interface {
	ListAll() ([]index.EdgeRow, error)
}

// NodeRenderer renders a node id to plain text for the inspect panel.
type NodeRenderer interface {
	Render(nodeID string) (string, error)
}

// Querier runs structural+semantic queries, returning ranked ids.
type Querier interface {
	Run(ctx context.Context, in QueryInput) ([]Match, error)
}

// ChangeSource reports the current dual change signal.
type ChangeSource interface {
	Signal() (Signal, error)
}

// Deps bundles everything the server needs. The command layer builds the
// concrete implementations (see graphview.NewQuerier / NewRenderer /
// NewChangeSource and the *index repos) from an open runtime.
type Deps struct {
	Root         string
	Nodes        NodeSource
	Edges        EdgeSource
	Render       NodeRenderer
	Query        Querier
	Changes      ChangeSource
	PollInterval time.Duration // SSE change-poll cadence; defaults to 2s
	Logger       *slog.Logger  // optional; nil silences
}
