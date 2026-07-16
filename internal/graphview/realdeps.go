package graphview

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/query"
	"github.com/germanamz/tusk/internal/render"
)

// --- NodeRenderer ---

type nodeRenderer struct {
	root  string
	nodes NodeSource
}

// NewRenderer renders a node id to plain text via NodeRepo.Get + file read +
// render.NodeText (the same path as `tusk node render`).
func NewRenderer(root string, nodes NodeSource) NodeRenderer {
	return &nodeRenderer{root: root, nodes: nodes}
}

func (renderer *nodeRenderer) Render(nodeID string) (string, error) {
	row, getErr := renderer.nodes.Get(nodeID)
	if getErr != nil {
		return "", getErr
	}

	body, readErr := os.ReadFile(filepath.Join(renderer.root, row.Path))
	if readErr != nil {
		return "", readErr
	}

	return render.NodeText(row.Path, body), nil
}

// --- Querier ---

type runtimeQuerier struct {
	deps query.Deps
	root string
}

// NewQuerier wraps query.Run. db/manifest/nodes/edges are required; embedder +
// embeddings are optional (semantic mode only).
func NewQuerier(db *sql.DB, loaded *manifest.Manifest, embedder embed.Embedder, embeddings *index.EmbeddingRepo, nodes *index.NodeRepo, edges *index.EdgeRepo, root string) Querier {
	return &runtimeQuerier{
		deps: query.Deps{
			Database:   db,
			Manifest:   loaded,
			Embedder:   embedder,
			Embeddings: embeddings,
			Nodes:      nodes,
			Edges:      edges,
		},
		root: root,
	}
}

func (rq *runtimeQuerier) Run(ctx context.Context, in QueryInput) ([]Match, error) {
	req := query.Request{
		Filter:              in.Filter,   // empty => match-all (compiles to WHERE 1 = 1)
		Semantic:            in.Semantic, // empty => structural only, embedder untouched
		Take:                in.Limit,
		SemanticDefaultTake: in.Limit,
		WorkspaceRoot:       rq.root,
	}

	result, runErr := query.Run(ctx, rq.deps, req)
	if runErr != nil {
		return nil, runErr
	}

	if in.Semantic != "" && result.Semantic != nil {
		matches := make([]Match, 0, len(result.Semantic.Ranked))
		for _, ranked := range result.Semantic.Ranked {
			matches = append(matches, Match{ID: ranked.ID, Score: ranked.Score})
		}

		return matches, nil
	}

	matches := make([]Match, 0, len(result.Rows))
	for _, row := range result.Rows {
		matches = append(matches, Match{ID: row.ID, Score: 1})
	}

	return matches, nil
}
