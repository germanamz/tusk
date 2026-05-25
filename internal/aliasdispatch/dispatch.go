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
	"sort"

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

// ListAliases returns the alias names known to the underlying manifest in
// sorted order. Helpful for `tusk run --list`.
func (dispatcher *Dispatcher) ListAliases() []manifest.Alias {
	if dispatcher.deps.Manifest == nil {
		return nil
	}

	names := make([]string, 0, len(dispatcher.deps.Manifest.Aliases))

	for name := range dispatcher.deps.Manifest.Aliases {
		names = append(names, name)
	}

	sort.Strings(names)

	aliases := make([]manifest.Alias, 0, len(names))

	for _, name := range names {
		aliases = append(aliases, dispatcher.deps.Manifest.Aliases[name])
	}

	return aliases
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
	req := query.ListRequest{
		WorkspaceRoot: deps.WorkspaceRoot,
	}

	filterVal, filterErr := optionalString(args, "filter")

	if filterErr != nil {
		return nil, filterErr
	}

	req.Filter = filterVal

	sortVal, sortErr := optionalString(args, "sort")

	if sortErr != nil {
		return nil, sortErr
	}

	req.Sort = sortVal

	takeVal, takeErr := optionalInt(args, "take")

	if takeErr != nil {
		return nil, takeErr
	}

	req.Take = takeVal

	skipVal, skipErr := optionalInt(args, "skip")

	if skipErr != nil {
		return nil, skipErr
	}

	req.Skip = skipVal

	includeVal, includeErr := optionalStringSlice(args, "include")

	if includeErr != nil {
		return nil, includeErr
	}

	req.Include = includeVal

	fieldsVal, fieldsErr := optionalStringSlice(args, "fields")

	if fieldsErr != nil {
		return nil, fieldsErr
	}

	req.Fields = fieldsVal

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
	req := node.GetRequest{}

	idVal, idErr := optionalString(args, "id")

	if idErr != nil {
		return nil, idErr
	}

	if idVal == "" {
		return nil, fmt.Errorf("node get adapter: args.id is required")
	}

	req.ID = idVal

	includeVal, includeErr := optionalStringSlice(args, "include")

	if includeErr != nil {
		return nil, includeErr
	}

	req.Include = includeVal

	fieldsVal, fieldsErr := optionalStringSlice(args, "fields")

	if fieldsErr != nil {
		return nil, fieldsErr
	}

	req.Fields = fieldsVal

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
	req := query.Request{
		WorkspaceRoot:       deps.WorkspaceRoot,
		SemanticDefaultTake: deps.SemanticDefaultTake,
	}

	filterVal, filterErr := optionalString(args, "filter")

	if filterErr != nil {
		return nil, filterErr
	}

	req.Filter = filterVal

	sortVal, sortErr := optionalString(args, "sort")

	if sortErr != nil {
		return nil, sortErr
	}

	req.Sort = sortVal

	takeVal, takeErr := optionalInt(args, "take")

	if takeErr != nil {
		return nil, takeErr
	}

	req.Take = takeVal

	skipVal, skipErr := optionalInt(args, "skip")

	if skipErr != nil {
		return nil, skipErr
	}

	req.Skip = skipVal

	semanticVal, semanticErr := optionalString(args, "semantic")

	if semanticErr != nil {
		return nil, semanticErr
	}

	req.Semantic = semanticVal

	minScoreVal, minScoreErr := optionalFloat(args, "min-score")

	if minScoreErr != nil {
		return nil, minScoreErr
	}

	req.MinScore = minScoreVal

	includeVal, includeErr := optionalStringSlice(args, "include")

	if includeErr != nil {
		return nil, includeErr
	}

	req.Include = includeVal

	fieldsVal, fieldsErr := optionalStringSlice(args, "fields")

	if fieldsErr != nil {
		return nil, fieldsErr
	}

	req.Fields = fieldsVal

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
	req := index.EdgeListRequest{}

	fromVal, fromErr := optionalString(args, "from")

	if fromErr != nil {
		return nil, fromErr
	}

	req.From = fromVal

	toVal, toErr := optionalString(args, "to")

	if toErr != nil {
		return nil, toErr
	}

	req.To = toVal

	typeVal, typeErr := optionalString(args, "type")

	if typeErr != nil {
		return nil, typeErr
	}

	req.Type = typeVal

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
	noMigrate, noMigrateErr := optionalBool(args, "no-migrate")

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

// --- arg coercion helpers ---

// optionalString returns the value of args[key] as a string, or "" if absent.
// Returns an error if the value is present but the wrong type.
func optionalString(args map[string]any, key string) (string, error) {
	raw, ok := args[key]

	if !ok {
		return "", nil
	}

	typed, isString := raw.(string)

	if !isString {
		return "", fmt.Errorf("arg %q has type %T, want string", key, raw)
	}

	return typed, nil
}

// optionalInt returns the value of args[key] as an int. BurntSushi/toml
// decodes integers as int64 when the destination is map[string]any;
// optionalInt also accepts float64 for exact-integer values so the helper is
// resilient to JSON-bridged callers.
func optionalInt(args map[string]any, key string) (int, error) {
	raw, ok := args[key]

	if !ok {
		return 0, nil
	}

	switch typed := raw.(type) {
	case int:
		return typed, nil
	case int64:
		return int(typed), nil
	case float64:
		if typed != float64(int64(typed)) {
			return 0, fmt.Errorf("arg %q has type %T (non-integer float), want int", key, raw)
		}

		return int(typed), nil
	}

	return 0, fmt.Errorf("arg %q has type %T, want int", key, raw)
}

// optionalFloat returns the value of args[key] as a float64.
func optionalFloat(args map[string]any, key string) (float64, error) {
	raw, ok := args[key]

	if !ok {
		return 0, nil
	}

	switch typed := raw.(type) {
	case float64:
		return typed, nil
	case int:
		return float64(typed), nil
	case int64:
		return float64(typed), nil
	}

	return 0, fmt.Errorf("arg %q has type %T, want float64", key, raw)
}

// optionalBool returns the value of args[key] as a bool.
func optionalBool(args map[string]any, key string) (bool, error) {
	raw, ok := args[key]

	if !ok {
		return false, nil
	}

	typed, isBool := raw.(bool)

	if !isBool {
		return false, fmt.Errorf("arg %q has type %T, want bool", key, raw)
	}

	return typed, nil
}

// optionalStringSlice returns the value of args[key] as a []string. A bare
// string is also accepted (single value), matching Cobra's StringSlice
// semantics.
func optionalStringSlice(args map[string]any, key string) ([]string, error) {
	raw, ok := args[key]

	if !ok {
		return nil, nil
	}

	if str, isString := raw.(string); isString {
		return []string{str}, nil
	}

	typed, isSlice := raw.([]any)

	if !isSlice {
		return nil, fmt.Errorf("arg %q has type %T, want []string", key, raw)
	}

	out := make([]string, 0, len(typed))

	for index, item := range typed {
		str, isString := item.(string)

		if !isString {
			return nil, fmt.Errorf("arg %q element %d has type %T, want string", key, index, item)
		}

		out = append(out, str)
	}

	return out, nil
}
