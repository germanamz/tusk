// Package graphview provides the read-only 3D graph-view API and SSE stream for
// the vault. It is a pure API/SSE provider: a parent server mounts its routes
// with RegisterRoutes and owns the host guard, CSP, healthz, and static
// frontend. It receives an already-open workspace handle via Deps and never
// opens the workspace or imports internal/mcp itself.
package graphview

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/webui"
)

// DefaultAddr is the loopback bind address for `tusk graph`. No prior
// precedent existed (MCP SSE defaults to :8765 over an opt-in transport); 7373
// is chosen as an unused loopback port.
const DefaultAddr = "127.0.0.1:7373"

// Signal is the dual change signal that drives live updates. Generation comes
// from the SQLite meta key "reindex_gen" (advances on every content reindex);
// Epoch comes from .tusk/epoch (advances only on reset/rebuild).
//
// It is an alias for webui.Signal: change detection is shared with the other
// local web views, so a graph Deps feeds webui's hub without conversion.
type Signal = webui.Signal

// GraphNode is one file-level node in the snapshot.
type GraphNode struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Group    string   `json:"group"`
	Title    string   `json:"title"`
	Path     string   `json:"path"`
	Tags     []string `json:"tags"`
	Degree   int      `json:"degree"`
	InDegree int      `json:"in_degree"`
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

// ClusterMeta describes the active cluster lens configuration so the client
// can label the legend, toggle layout forces, and display the active
// dimension without guessing. Phase 2 fills By, Property, and Huddle;
// Phase 4 makes Huddle meaningful; Phase 7 adds Hull.
type ClusterMeta struct {
	By       string `json:"by"`
	Property string `json:"property,omitempty"`
	Huddle   bool   `json:"huddle"`
	Hull     bool   `json:"hull"`
}

// Graph is the /api/graph snapshot payload.
type Graph struct {
	Generation int64       `json:"generation"`
	Epoch      int64       `json:"epoch"`
	Nodes      []GraphNode `json:"nodes"`
	Edges      []GraphEdge `json:"edges"`
	Cluster    ClusterMeta `json:"cluster"`
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

// NodeDetail is the /api/graph/node/{id} inspect payload.
type NodeDetail struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Title      string          `json:"title"`
	Path       string          `json:"path"`
	Properties json.RawMessage `json:"properties"`
	Rendered   string          `json:"rendered"`
	Neighbors  []Neighbor      `json:"neighbors"`
}

// SubunitGraph is the /api/graph/subunits/{id...} drill-down payload.
type SubunitGraph struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

// QueryInput is the /api/graph/query request.
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
	ListByIDs(ids []string) ([]index.NodeRow, error)
}

// EdgeSource lists edges. Satisfied by *index.EdgeRepo.
type EdgeSource interface {
	ListAll() ([]index.EdgeRow, error)
	ListBySource(sourceID string) ([]index.EdgeRow, error)
	ListByTarget(targetID string) ([]index.EdgeRow, error)
}

// EmbeddingSource fetches stored embedding vectors for nodes. Satisfied by
// *index.EmbeddingRepo. Used by the /api/graph/embeddings endpoint (semantic layout).
type EmbeddingSource interface {
	ListByNodeIDs(nodeIDs []string) ([]index.EmbeddingRow, error)
}

// NodeRenderer renders a node id to plain text for the inspect panel.
type NodeRenderer interface {
	Render(nodeID string) (string, error)
}

// Querier runs structural+semantic queries, returning ranked ids.
type Querier interface {
	Run(ctx context.Context, in QueryInput) ([]Match, error)
}

// ChangeSource reports the current dual change signal. An alias for
// webui.ChangeSource; build one with webui.NewChangeSource.
type ChangeSource = webui.ChangeSource

// Deps bundles everything the server needs. The command layer builds the
// concrete implementations (see graphview.NewQuerier / NewRenderer,
// webui.NewChangeSource, and the *index repos) from an open runtime.
type Deps struct {
	Root         string
	Nodes        NodeSource
	Edges        EdgeSource
	Render       NodeRenderer
	Query        Querier
	Changes      ChangeSource
	Manifest     *manifest.Manifest // optional; nil tolerates as by = "type"
	Embeddings   EmbeddingSource    // optional; nil disables /api/graph/embeddings (returns empty)
	PollInterval time.Duration      // SSE change-poll cadence; defaults to 2s
	Logger       *slog.Logger       // optional; nil silences
}
