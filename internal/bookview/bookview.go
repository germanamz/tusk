package bookview

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/webui"
)

// NodeSource lists and fetches nodes. Satisfied by *index.NodeRepo.
type NodeSource interface {
	Get(nodeID string) (*index.NodeRow, error)
	ListFileNodes() ([]index.NodeRow, error)
	ListByIDs(ids []string) ([]index.NodeRow, error)

	// FindByTitle resolves a title to matching node ids. targetType is a node
	// type or "*". An absent title returns (nil, nil) — no error — so callers
	// treat len(ids) == 0 as "not found". Sub-unit ids are included; filter
	// explicitly when only file nodes should match.
	FindByTitle(targetType, title string) ([]string, error)
}

// EdgeSource lists the edges incident to a node. Satisfied by *index.EdgeRepo.
// Neither method filters by Kind: structural contains/contained-by edges come
// back alongside direct and derived ones.
type EdgeSource interface {
	ListBySource(sourceID string) ([]index.EdgeRow, error)
	ListByTarget(targetID string) ([]index.EdgeRow, error)
}

// Searcher runs a reading-UI search. The concrete implementation adapts
// query.Run and is built in the command layer, so handlers stay testable with
// fakes. It returns query.Run's error unwrapped, letting the handler classify a
// semantically-unavailable embedder by error identity (errors.Is) rather than
// message text.
type Searcher interface {
	Search(ctx context.Context, req SearchRequest) (SearchResponse, error)
}

// RelatedSource walks the graph outward from one node. The concrete
// implementation adapts internal/graphexpand and is built in the command layer.
//
// hops and weight are optional: nil means "not specified — inherit the
// workspace manifest's [query.graph-expansion] default". They are pointers
// because 0 is a legitimate explicit value that must stay distinguishable from
// an absent one — a bare float64 weight cannot express "unset", so an absent
// weight would silently overwrite the configured default with 0 and flatten the
// graph term. edgeTypes needs no such treatment: nil already means absent.
type RelatedSource interface {
	Related(ctx context.Context, nodeID string, hops *int, edgeTypes []string, weight *float64) (RelatedResponse, error)
}

// Deps bundles everything the server needs. The command layer builds the
// concrete implementations from an open runtime; bookview never opens the
// workspace itself.
type Deps struct {
	Root  string
	Nodes NodeSource
	Edges EdgeSource

	// Search backs POST /api/search; nil makes the endpoint report 503.
	Search Searcher

	// Related backs GET /api/related/{id...}; nil makes it return empty.
	Related RelatedSource

	// Meta reads the reindex generation for the SSE change signal; nil reports
	// a constant zero signal (the stream stays open but never fires).
	Meta webui.MetaReader

	Logger *slog.Logger // optional; nil silences

	// AllowedHosts extends the Host-header guard beyond loopback and
	// "localhost". A confirmed non-loopback bind passes the bound hostname
	// here; a single "*" entry disables the guard (the user accepted network
	// exposure). Empty means loopback-only.
	AllowedHosts []string

	PollInterval time.Duration // SSE change-poll cadence; defaults to 2s
}

// IndexNode is one entry in the table-of-contents index. Parent is empty for a
// file node and holds the owning file's id for a sub-unit.
type IndexNode struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Title  string `json:"title"`
	Path   string `json:"path"`
	Parent string `json:"parent"`
}

// IndexResponse is the GET /api/index payload.
type IndexResponse struct {
	Nodes []IndexNode `json:"nodes"`
}

// LinkRef is one end of a link shown in the reading rails: the far node plus
// the type of the edge reaching it.
type LinkRef struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Type     string `json:"type"`
	EdgeType string `json:"edge_type"`
}

// WikilinkTarget is the resolution of one raw [[wikilink]] target found in a
// node body. Exists reports whether it resolves to a real node, letting the
// frontend render unresolved links as broken rather than navigable.
type WikilinkTarget struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Exists bool   `json:"exists"`
}

// NodeReadPayload is the GET /api/node/{id...} payload: one node as a readable
// document. Markdown is the raw body with frontmatter stripped — rendering it
// to HTML is the frontend's job. Wikilinks maps each raw link target found in
// the body to its resolution.
type NodeReadPayload struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Title      string          `json:"title"`
	Path       string          `json:"path"`
	Properties json.RawMessage `json:"properties"`
	Markdown   string          `json:"markdown"`
	Links      struct {
		Out []LinkRef `json:"out"`
		In  []LinkRef `json:"in"`
	} `json:"links"`
	Wikilinks map[string]WikilinkTarget `json:"wikilinks"`
}

// SearchRequest is the POST /api/search body.
type SearchRequest struct {
	Q         string   `json:"q"`
	Filter    string   `json:"filter"`
	Expand    bool     `json:"expand"`
	Hops      int      `json:"hops"`
	EdgeTypes []string `json:"edge_types"`
	Weight    float64  `json:"weight"`
	Limit     int      `json:"limit"`
	Explain   bool     `json:"explain"`
}

// Match is one ranked search result. The score breakdown fields are populated
// only when the request asked to explain the ranking.
type Match struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Type        string  `json:"type"`
	Score       float64 `json:"score"`
	CosineScore float64 `json:"cosine_score,omitempty"`
	GraphScore  float64 `json:"graph_score,omitempty"`
	FinalScore  float64 `json:"final_score,omitempty"`
	Distance    int     `json:"distance,omitempty"`
}

// SearchResponse is the POST /api/search payload. Model names the embedding
// model that ranked the matches.
type SearchResponse struct {
	Matches []Match `json:"matches"`
	Model   string  `json:"model"`
}

// RelatedNode is one node reached by the graph walk. Distance is its hop count
// from the focus node.
type RelatedNode struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Type       string  `json:"type"`
	GraphScore float64 `json:"graph_score"`
	Distance   int     `json:"distance"`
}

// RelatedResponse is the GET /api/related/{id...} payload.
type RelatedResponse struct {
	Related []RelatedNode `json:"related"`
}
