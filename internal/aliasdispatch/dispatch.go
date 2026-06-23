// Package aliasdispatch invokes manifest-declared aliases against the
// canonical read-verb service layer. Each alias maps to a verb (per
// cliregistry.ReadOnly) and a set of args; the dispatcher builds the typed
// per-verb Request and calls the matching service entry point.
//
// Dispatcher is explicit rather than reflective: every supported verb has a
// hand-written adapter so the call site is debuggable and the request
// translation rules are visible in one file.
package aliasdispatch

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/germanamz/tusk/internal/argval"
	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/query"
	"github.com/germanamz/tusk/internal/status"
)

// Result kinds — also used as the "kind" field on DispatchResult so callers
// can switch on the typed payload in Result.Result.
const (
	KindNodeList = "node-list"
	KindNodeGet  = "node-get"
	KindQuery    = "query"
	KindEdgeList = "edge-list"
	KindDoctor   = "doctor"
	KindStatus   = "status"
)

// DispatchResult is the typed payload returned by Dispatcher.Run.
//
// Result holds the underlying *<Verb>Result (e.g. *query.ListResult) — the
// caller switches on Kind to recover the concrete shape.
type DispatchResult struct {
	Alias   string
	Command string
	Kind    string
	Result  any
}

// Deps bundles the primitives every adapter may need. Individual adapters
// only read the fields they require; unused fields may be left zero.
type Deps struct {
	Database      *sql.DB
	Manifest      *manifest.Manifest
	WorkspaceRoot string

	NodeService *node.Service

	Nodes         *index.NodeRepo
	Edges         *index.EdgeRepo
	EmbedQueue    *index.EmbedQueueRepo
	WorkflowDrift *index.WorkflowDriftRepo
	PropertyDrift *index.PropertyDriftRepo
	Embeddings    *index.EmbeddingRepo
	Meta          *index.MetaRepo

	Embedder embed.Embedder

	// SemanticDefaultTake is the default semantic page size passed into
	// query.Run when the alias does not set take. CLI callers pass 0
	// (unlimited); MCP callers pass 10.
	SemanticDefaultTake int
}

// VerbAdapter binds one CLI/MCP verb to a build-then-run pair. Build turns
// the alias's args map into the typed request the service expects; Run
// invokes the service and returns its typed result.
type VerbAdapter struct {
	Kind  string
	Build func(args map[string]any, deps Deps) (any, error)
	Run   func(ctx context.Context, deps Deps, request any) (any, error)
}

// Dispatcher invokes aliases. It is safe for concurrent use as long as the
// shared Deps are safe for concurrent use.
type Dispatcher struct {
	adapters map[string]VerbAdapter
	deps     Deps
}

// NewDispatcher constructs a Dispatcher with the built-in adapters for the
// six read verbs.
func NewDispatcher(deps Deps) *Dispatcher {
	return &Dispatcher{
		deps:     deps,
		adapters: builtinAdapters(),
	}
}

// Run dispatches an alias and returns the typed result. Errors propagate
// from either the request build step (bad arg types not caught by
// ValidateAliases) or the service call.
func (dispatcher *Dispatcher) Run(ctx context.Context, alias manifest.Alias) (*DispatchResult, error) {
	if alias.Command == "" {
		return nil, fmt.Errorf("aliasdispatch: alias %q has empty command", alias.Name)
	}

	adapter, ok := dispatcher.adapters[alias.Command]

	if !ok {
		return nil, fmt.Errorf("aliasdispatch: no adapter for verb %q (alias %q)", alias.Command, alias.Name)
	}

	request, buildErr := adapter.Build(alias.Args, dispatcher.deps)

	if buildErr != nil {
		return nil, fmt.Errorf("aliasdispatch: build %s request for alias %q: %w", alias.Command, alias.Name, buildErr)
	}

	result, runErr := adapter.Run(ctx, dispatcher.deps, request)

	if runErr != nil {
		return nil, fmt.Errorf("aliasdispatch: run alias %q: %w", alias.Name, runErr)
	}

	return &DispatchResult{
		Alias:   alias.Name,
		Command: alias.Command,
		Kind:    adapter.Kind,
		Result:  result,
	}, nil
}

// builtinAdapters returns the canonical adapter map. Exposed via
// NewDispatcher; the function is package-internal so the table is in one
// place.
func builtinAdapters() map[string]VerbAdapter {
	return map[string]VerbAdapter{
		"node list": {
			Kind:  KindNodeList,
			Build: buildNodeListRequest,
			Run:   runNodeList,
		},
		"node get": {
			Kind:  KindNodeGet,
			Build: buildNodeGetRequest,
			Run:   runNodeGet,
		},
		"query": {
			Kind:  KindQuery,
			Build: buildQueryRequest,
			Run:   runQuery,
		},
		"edge list": {
			Kind:  KindEdgeList,
			Build: buildEdgeListRequest,
			Run:   runEdgeList,
		},
		"doctor": {
			Kind:  KindDoctor,
			Build: buildDoctorRequest,
			Run:   runDoctor,
		},
		"status": {
			Kind:  KindStatus,
			Build: buildStatusRequest,
			Run:   runStatus,
		},
	}
}

// --- node list adapter ---

func buildNodeListRequest(args map[string]any, deps Deps) (any, error) {
	reader := &argReader{args: args}

	req := query.ListRequest{WorkspaceRoot: deps.WorkspaceRoot}
	req.Filter = reader.String("filter")
	req.Sort = reader.String("sort")
	req.Take = reader.Int("take")
	req.Skip = reader.Int("skip")
	req.Include = reader.StringSlice("include")
	req.Fields = reader.StringSlice("fields")

	if reader.err != nil {
		return nil, reader.err
	}

	return req, nil
}

func runNodeList(_ context.Context, deps Deps, request any) (any, error) {
	typedRequest, ok := request.(query.ListRequest)

	if !ok {
		return nil, fmt.Errorf("node list adapter: bad request type %T", request)
	}

	if deps.Database == nil {
		return nil, fmt.Errorf("node list adapter: Deps.Database is nil")
	}

	if deps.Manifest == nil {
		return nil, fmt.Errorf("node list adapter: Deps.Manifest is nil")
	}

	return query.ListRun(deps.Database, deps.Manifest, typedRequest)
}

// --- node get adapter ---

func buildNodeGetRequest(args map[string]any, _ Deps) (any, error) {
	reader := &argReader{args: args}

	id := reader.String("id")

	// A type error on id wins over the required-check; the required-check then
	// fires before include/fields are read — matching the original ordering.
	if reader.err != nil {
		return nil, reader.err
	}

	if id == "" {
		return nil, fmt.Errorf("node get adapter: args.id is required")
	}

	req := node.GetRequest{ID: id}
	req.Include = reader.StringSlice("include")
	req.Fields = reader.StringSlice("fields")

	if reader.err != nil {
		return nil, reader.err
	}

	return req, nil
}

func runNodeGet(_ context.Context, deps Deps, request any) (any, error) {
	typedRequest, ok := request.(node.GetRequest)

	if !ok {
		return nil, fmt.Errorf("node get adapter: bad request type %T", request)
	}

	if deps.NodeService == nil {
		return nil, fmt.Errorf("node get adapter: Deps.NodeService is nil")
	}

	return node.GetRun(deps.NodeService, typedRequest)
}

// --- query adapter ---

func buildQueryRequest(args map[string]any, deps Deps) (any, error) {
	reader := &argReader{args: args}

	req := query.Request{
		WorkspaceRoot:       deps.WorkspaceRoot,
		SemanticDefaultTake: deps.SemanticDefaultTake,
	}
	req.Filter = reader.String("filter")
	req.Sort = reader.String("sort")
	req.Take = reader.Int("take")
	req.Skip = reader.Int("skip")
	req.Semantic = reader.String("semantic")
	req.MinScore = reader.Float("min-score")
	req.Include = reader.StringSlice("include")
	req.Fields = reader.StringSlice("fields")

	if reader.err != nil {
		return nil, reader.err
	}

	return req, nil
}

func runQuery(ctx context.Context, deps Deps, request any) (any, error) {
	typedRequest, ok := request.(query.Request)

	if !ok {
		return nil, fmt.Errorf("query adapter: bad request type %T", request)
	}

	if deps.Database == nil {
		return nil, fmt.Errorf("query adapter: Deps.Database is nil")
	}

	if deps.Manifest == nil {
		return nil, fmt.Errorf("query adapter: Deps.Manifest is nil")
	}

	return query.Run(ctx, query.Deps{
		Database:   deps.Database,
		Manifest:   deps.Manifest,
		Embedder:   deps.Embedder,
		Embeddings: deps.Embeddings,
		Nodes:      deps.Nodes,
		Edges:      deps.Edges,
	}, typedRequest)
}

// --- edge list adapter ---

func buildEdgeListRequest(args map[string]any, _ Deps) (any, error) {
	reader := &argReader{args: args}

	req := index.EdgeListRequest{}
	req.From = reader.String("from")
	req.To = reader.String("to")
	req.Type = reader.String("type")

	if reader.err != nil {
		return nil, reader.err
	}

	return req, nil
}

func runEdgeList(_ context.Context, deps Deps, request any) (any, error) {
	typedRequest, ok := request.(index.EdgeListRequest)

	if !ok {
		return nil, fmt.Errorf("edge list adapter: bad request type %T", request)
	}

	if deps.Edges == nil {
		return nil, fmt.Errorf("edge list adapter: Deps.Edges is nil")
	}

	return index.EdgeListRun(deps.Edges, typedRequest)
}

// --- doctor adapter ---

func buildDoctorRequest(args map[string]any, deps Deps) (any, error) {
	noMigrate, noMigrateErr := argval.Bool(args, "no-migrate")

	if noMigrateErr != nil {
		return nil, noMigrateErr
	}

	return doctor.Request{
		Cfg: doctor.Config{
			Nodes:         deps.Nodes,
			Edges:         deps.Edges,
			EmbedQueue:    deps.EmbedQueue,
			WorkflowDrift: deps.WorkflowDrift,
			PropertyDrift: deps.PropertyDrift,
			Embeddings:    deps.Embeddings,
			Manifest:      deps.Manifest,
			Root:          deps.WorkspaceRoot,
		},
		NoMigrate: noMigrate,
	}, nil
}

func runDoctor(_ context.Context, _ Deps, request any) (any, error) {
	typedRequest, ok := request.(doctor.Request)

	if !ok {
		return nil, fmt.Errorf("doctor adapter: bad request type %T", request)
	}

	return doctor.RunWithMigration(typedRequest)
}

// --- status adapter ---

func buildStatusRequest(_ map[string]any, deps Deps) (any, error) {
	return status.Request{
		Nodes:      deps.Nodes,
		Edges:      deps.Edges,
		EmbedQueue: deps.EmbedQueue,
		Meta:       deps.Meta,
	}, nil
}

func runStatus(_ context.Context, _ Deps, request any) (any, error) {
	typedRequest, ok := request.(status.Request)

	if !ok {
		return nil, fmt.Errorf("status adapter: bad request type %T", request)
	}

	return status.Run(typedRequest)
}

// --- arg coercion ---

// argReader coerces fields off an alias args map, accumulating the first
// coercion error so a builder can assign every field and check once at the end.
// Once an error is recorded every subsequent read is a no-op returning the zero
// value — observably identical to the short-circuit-on-first-error builders this
// replaced (the coercers are pure and the built request is discarded on error).
// It delegates to internal/argval for the actual coercion + error text.
type argReader struct {
	args map[string]any
	err  error
}

func (reader *argReader) String(key string) string {
	if reader.err != nil {
		return ""
	}

	value, err := argval.String(reader.args, key)

	if err != nil {
		reader.err = err

		return ""
	}

	return value
}

func (reader *argReader) Int(key string) int {
	if reader.err != nil {
		return 0
	}

	value, err := argval.Int(reader.args, key)

	if err != nil {
		reader.err = err

		return 0
	}

	return value
}

func (reader *argReader) Float(key string) float64 {
	if reader.err != nil {
		return 0
	}

	value, err := argval.Float(reader.args, key)

	if err != nil {
		reader.err = err

		return 0
	}

	return value
}

func (reader *argReader) StringSlice(key string) []string {
	if reader.err != nil {
		return nil
	}

	value, err := argval.StringSlice(reader.args, key)

	if err != nil {
		reader.err = err

		return nil
	}

	return value
}
