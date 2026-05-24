// Package contextcompose composes the warm-context digest exposed through
// `tusk context` and `tusk_context`. It blends three sections:
//
//   - Pinned nodes (manifest.Context.Pinned): always-present nodes the agent
//     should hold in context, loaded via the read-verb service layer with
//     body and edges expanded by default.
//   - Recent activity (manifest.Context.Recent): the result of a single
//     manifest-declared alias dispatched through aliasdispatch.
//   - Named aliases (manifest.Context.Include): a fan-out of alias
//     dispatches whose results are folded into the digest under their
//     alias name.
//
// Compose dispatches the include aliases in parallel via errgroup; pinned
// and recent are loaded sequentially first because they tend to be small
// and benefit from running before the parallel section starts contending on
// the SQLite WAL.
package contextcompose

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/germanamz/tusk/internal/aliasdispatch"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/query"
)

// Result is the typed payload Compose returns. Pinned and Recent are
// node-shaped rows so the CLI / MCP renderer can reuse the compact node
// renderer. Aliases is keyed by alias name; each entry carries the
// underlying DispatchResult so the renderer can dispatch on Kind.
type Result struct {
	Pinned        []query.ListRow                          `json:"pinned,omitempty"`
	Recent        []query.ListRow                          `json:"recent,omitempty"`
	Aliases       map[string]*aliasdispatch.DispatchResult `json:"aliases,omitempty"`
	MissingPinned []string                                 `json:"missing_pinned,omitempty"`
}

// Deps bundles every primitive the composer touches. Pinned rows are
// loaded directly via query.ListRun (hence Database + WorkspaceRoot);
// recent and include sections go through the alias Dispatcher.
type Deps struct {
	Manifest      *manifest.Manifest
	Dispatcher    *aliasdispatch.Dispatcher
	WorkspaceRoot string
	Database      *sql.DB
}

// Request narrows the per-call shape of the composer.
type Request struct {
	// Include overrides the per-node include set applied to pinned and
	// recent rows. When empty, Compose defaults to [body, edges].
	Include []string
}

// defaultInclude is the per-node expansion set Compose applies when the
// caller does not set Request.Include. Pinned nodes are loaded with body
// and edges so the agent can reason without a follow-up tool call.
var defaultInclude = []string{"body", "edges"}

// Compose runs the digest. Pinned and recent are loaded sequentially; the
// include aliases are dispatched in parallel via errgroup. The first
// error from any parallel dispatch cancels the others (errgroup
// semantics) and is returned to the caller.
//
// Pinned IDs that do not resolve in the index are silently omitted from
// Result.Pinned and instead surfaced through Result.MissingPinned for
// doctor reporting. The caller decides how to render them.
func Compose(ctx context.Context, deps Deps, req Request) (*Result, error) {
	if deps.Manifest == nil {
		return nil, fmt.Errorf("contextcompose: Deps.Manifest is nil")
	}

	if deps.Database == nil {
		return nil, fmt.Errorf("contextcompose: Deps.Database is nil")
	}

	if deps.Dispatcher == nil {
		return nil, fmt.Errorf("contextcompose: Deps.Dispatcher is nil")
	}

	cfg := deps.Manifest.Context

	if cfg == nil {
		// No [context] block declared — return an empty digest. The CLI /
		// MCP renderer prints a friendly "no context configured" line.
		return &Result{}, nil
	}

	include := req.Include

	if len(include) == 0 {
		include = defaultInclude
	}

	result := &Result{}

	if len(cfg.Pinned) > 0 {
		pinned, missing, pinnedErr := loadPinned(deps, cfg.Pinned, include)

		if pinnedErr != nil {
			return nil, pinnedErr
		}

		result.Pinned = pinned
		result.MissingPinned = missing
	}

	if cfg.Recent != nil {
		recentRows, recentErr := loadRecent(ctx, deps, cfg.Recent, include)

		if recentErr != nil {
			return nil, recentErr
		}

		result.Recent = recentRows
	}

	if len(cfg.Include) > 0 {
		aliasResults, aliasErr := dispatchIncludes(ctx, deps, cfg.Include)

		if aliasErr != nil {
			return nil, aliasErr
		}

		result.Aliases = aliasResults
	}

	return result, nil
}

// loadPinned fetches the pinned nodes via a single ListRequest with an
// `id=foo OR id=bar` filter so the expansion pipeline runs once per call
// rather than per-node. The returned MissingPinned slice carries any IDs
// the filter did not match (the underlying file may have been deleted or
// renamed since the manifest was last edited).
func loadPinned(deps Deps, ids []string, include []string) ([]query.ListRow, []string, error) {
	if len(ids) == 0 {
		return nil, nil, nil
	}

	parts := make([]string, 0, len(ids))

	for _, id := range ids {
		// Wrap the id in single quotes so a path with hyphens or slashes
		// parses as a literal string. The filter grammar treats single
		// quotes as the string delimiter.
		parts = append(parts, fmt.Sprintf("id='%s'", id))
	}

	filterExpr := strings.Join(parts, " OR ")

	req := query.ListRequest{
		WorkspaceRoot: deps.WorkspaceRoot,
		Filter:        filterExpr,
		Include:       include,
	}

	listResult, listErr := query.ListRun(deps.Database, deps.Manifest, req)

	if listErr != nil {
		return nil, nil, fmt.Errorf("contextcompose: pinned: %w", listErr)
	}

	// Build the missing set by diffing the requested IDs against the rows
	// the filter returned. Iterate the original `ids` slice so the
	// surfaced missing list preserves manifest order.
	resolved := make(map[string]struct{}, len(listResult.Rows))

	for _, row := range listResult.Rows {
		resolved[row.ID] = struct{}{}
	}

	var missing []string

	for _, id := range ids {
		if _, ok := resolved[id]; !ok {
			missing = append(missing, id)
		}
	}

	// Stable order for the pinned rows: respect the manifest's declared
	// order rather than whatever the SQL engine returned.
	orderedRows := make([]query.ListRow, 0, len(listResult.Rows))

	for _, id := range ids {
		for _, row := range listResult.Rows {
			if row.ID == id {
				orderedRows = append(orderedRows, row)

				break
			}
		}
	}

	return orderedRows, missing, nil
}

// loadRecent dispatches the recent alias and returns its node-list rows.
// Recent aliases targeting verbs other than `node list` fall back to an
// empty slice — the spec calls them "unlikely but allowed" and the
// renderer treats an empty Recent as "no recent activity".
//
// The defaultInclude argument carries the effective per-node expansion set
// (Request.Include, or [body, edges] when the caller did not override).
// It is injected into the alias's args ONLY when the alias did not declare
// its own args.include — explicit alias config wins over the digest-level
// default so authors can still trim or expand recent independently.
func loadRecent(ctx context.Context, deps Deps, alias *manifest.Alias, defaultInclude []string) ([]query.ListRow, error) {
	effective := withIncludeFallback(*alias, defaultInclude)

	dispatched, runErr := deps.Dispatcher.Run(ctx, effective)

	if runErr != nil {
		return nil, fmt.Errorf("contextcompose: recent: %w", runErr)
	}

	switch typed := dispatched.Result.(type) {
	case *query.ListResult:
		return typed.Rows, nil
	case *query.Result:
		if typed.Semantic != nil {
			// Translate a semantic result back into the ListRow shape so
			// the renderer can treat both forms uniformly. Drop the
			// score; recent is a chronological view, not a ranking.
			rows := make([]query.ListRow, 0, len(typed.Semantic.Ranked))

			for _, scored := range typed.Semantic.Ranked {
				rows = append(rows, query.ListRow{
					ID:         scored.ID,
					Type:       scored.Type,
					Path:       scored.Path,
					Title:      scored.Title,
					Body:       scored.Body,
					Properties: scored.Properties,
					Edges:      scored.Edges,
				})
			}

			return rows, nil
		}

		rows := make([]query.ListRow, 0, len(typed.Rows))

		for _, row := range typed.Rows {
			rows = append(rows, query.ListRow{
				ID:            row.ID,
				Type:          row.Type,
				Path:          row.Path,
				Title:         row.Title,
				PropertiesRaw: row.PropertiesRaw,
				Body:          row.Body,
				Properties:    row.Properties,
				Edges:         row.Edges,
			})
		}

		return rows, nil
	}

	// Unknown shape — return nil. The composer caller still gets the
	// pinned + alias sections, so the digest is partially useful even
	// when the recent alias targets something we cannot reshape.
	return nil, nil
}

// dispatchIncludes runs each named alias in parallel via errgroup. The
// concurrency bound is the slice length: the workspace lock plus
// SQLite WAL handle contention between readers. Per-alias errors cancel
// the group and surface to the caller.
func dispatchIncludes(ctx context.Context, deps Deps, names []string) (map[string]*aliasdispatch.DispatchResult, error) {
	group, groupCtx := errgroup.WithContext(ctx)

	var (
		mu  sync.Mutex
		out = make(map[string]*aliasdispatch.DispatchResult, len(names))
	)

	for _, name := range names {
		alias, ok := deps.Manifest.Aliases[name]

		if !ok {
			// Defensive: ValidateContext already dropped unknown names,
			// but a manual []string passed at runtime might still miss.
			return nil, fmt.Errorf("contextcompose: include alias %q not declared", name)
		}

		group.Go(func() error {
			dispatched, runErr := deps.Dispatcher.Run(groupCtx, alias)

			if runErr != nil {
				return fmt.Errorf("contextcompose: alias %q: %w", name, runErr)
			}

			mu.Lock()
			out[name] = dispatched
			mu.Unlock()

			return nil
		})
	}

	if waitErr := group.Wait(); waitErr != nil {
		return nil, waitErr
	}

	return out, nil
}

// withIncludeFallback returns a copy of alias with args["include"] set to
// fallback when the alias does not declare an include key. When the alias
// already carries args.include, the copy is returned unchanged so the
// author's explicit choice wins. fallback==nil is also a no-op.
//
// The injected slice uses []any (not []string) so the dispatcher's
// optionalStringSlice helper accepts it; that helper coerces []any
// elements back to []string before the underlying ListRequest is built.
func withIncludeFallback(alias manifest.Alias, fallback []string) manifest.Alias {
	if len(fallback) == 0 {
		return alias
	}

	if _, hasInclude := alias.Args["include"]; hasInclude {
		return alias
	}

	args := make(map[string]any, len(alias.Args)+1)

	for key, value := range alias.Args {
		args[key] = value
	}

	asAny := make([]any, len(fallback))

	for index, token := range fallback {
		asAny[index] = token
	}

	args["include"] = asAny
	alias.Args = args

	return alias
}

// SortedIncludeNames returns the alias names from result.Aliases in
// deterministic order. Used by renderers that need a stable section
// ordering.
func SortedIncludeNames(result *Result) []string {
	if result == nil || len(result.Aliases) == 0 {
		return nil
	}

	names := make([]string, 0, len(result.Aliases))

	for name := range result.Aliases {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}
