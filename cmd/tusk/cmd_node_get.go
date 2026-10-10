package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/query"
	"github.com/germanamz/tusk/internal/render"
	"github.com/spf13/cobra"
)

func newNodeGetCmd() *cobra.Command {
	var (
		includeFlag []string
		fieldsFlag  []string
		formatFlag  string
		emitJSON    bool
	)

	getCmd := &cobra.Command{
		Use:   "get <node-id>",
		Short: "Print the source file (markdown or HTML) for a node by id",
		Long: `Print the source file (frontmatter/markup + body) for a node by id.

The node id is the workspace-relative path: markdown nodes drop the
extension (notes/hello.md has id "notes/hello"), while HTML nodes retain
it (page.html has id "page.html").

By default (no flags) the command prints the raw file to stdout
verbatim — useful for piping into editors, less, or another tusk command.
When --include, --fields, --format, or --json is passed the command emits
structured output instead (compact for TTY, JSON otherwise).

--include paths lists the workspace paths the page names (in inline code,
links, or a paths-only frontmatter value) under edge types with
paths = true, each with its lines and a live exists flag, the way
tusk query --include paths does. It is never part of the default shape.`,
		Example: `  # Print the raw file
  tusk node get notes/hello

  # Structured JSON envelope with only the body
  tusk node get notes/hello --include body --format json

  # The workspace paths a page names
  tusk node get technical/ledger --include paths

  # Open in $EDITOR (round-trip through a temp file)
  tusk node get notes/hello > /tmp/hello.md && $EDITOR /tmp/hello.md`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, loaded, resolveErr := resolveWorkspace()

			if resolveErr != nil {
				return resolveErr
			}

			store, openErr := openStore(cmd, ws.Root, ws.IndexPath, loaded)

			if openErr != nil {
				return openErr
			}

			defer store.Close()

			service := node.NewService(ws.Root, index.NewNodeRepo(store))

			result, runErr := node.GetRun(service, node.GetRequest{
				ID:      args[0],
				Include: includeFlag,
				Fields:  fieldsFlag,
			})

			if runErr != nil {
				return runErr
			}

			// Back-compat: when the user passed no flags, preserve the
			// raw-file output the historical `tusk node get` emitted.
			if !result.HasIncludeFilter && !emitJSON && formatFlag == "" {
				rendered, renderErr := os.ReadFile(filepath.Join(ws.Root, result.Node.Path))

				if renderErr != nil {
					return renderErr
				}

				_, _ = fmt.Fprint(cmd.OutOrStdout(), string(rendered))

				return nil
			}

			hasShapeFlags := len(includeFlag) > 0 || len(fieldsFlag) > 0
			format, formatErr := resolveFormat(emitJSON, formatFlag, hasShapeFlags)

			if formatErr != nil {
				return formatErr
			}

			payload := buildNodeGetPayload(result)

			// node.GetRun reads only the nodes table, so hydrate edges from the
			// edges table the way `query --include edges` does (both directions,
			// with titles) rather than emitting the always-nil parse-path map.
			if result.IncludeEdges {
				edges, edgeErr := query.LoadEdgesForNode(store.DB(), result.Node.ID)

				if edgeErr != nil {
					return edgeErr
				}

				payload.Edges = edges
			}

			if result.IncludePaths {
				paths, pathsErr := query.LoadPathsForNode(store.DB(), ws.Root, result.Node.ID)

				if pathsErr != nil {
					return pathsErr
				}

				payload.Paths = paths
			}

			if format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), payload)
			}

			// Both formatCompact and formatLegacy render through the
			// compact renderer for `tusk node get` — the historical
			// raw-file output already short-circuited above when no
			// flags were passed.
			return renderNodeGetCompact(cmd.OutOrStdout(), payload, fieldsFlag)
		},
	}

	getCmd.Flags().StringSliceVar(&includeFlag, "include", nil, "expand returned shape: body|edges|properties|paths (comma-separated; paths lists the workspace paths the page names)")
	getCmd.Flags().StringSliceVar(&fieldsFlag, "fields", nil, "project returned shape to these fields (comma-separated)")
	getCmd.Flags().StringVar(&formatFlag, "format", "", "output format: compact|json (default: compact for TTY, json otherwise)")
	getCmd.Flags().BoolVar(&emitJSON, "json", false, "emit structured JSON (sugar for --format json)")

	return getCmd
}

// nodeGetPayload is the renderable shape `tusk node get` returns in
// structured mode. It is a struct around a map so JSON marshalling preserves
// the include filter exactly (empty strings still appear when the caller
// requested body but the body is empty).
type nodeGetPayload struct {
	ID         string
	Type       string
	Path       string
	Title      string
	Body       string
	Properties map[string]any
	Edges      []query.EdgeRef
	Paths      []query.PathRef

	includeBody       bool
	includeEdges      bool
	includeProperties bool
	includePaths      bool
}

// MarshalJSON honors the include filter: requested fields appear in the
// envelope even when empty, and unrequested fields are omitted entirely.
func (payload nodeGetPayload) MarshalJSON() ([]byte, error) {
	envelope := map[string]any{
		"id":    payload.ID,
		"type":  payload.Type,
		"path":  payload.Path,
		"title": payload.Title,
	}

	if payload.includeBody {
		envelope["body"] = payload.Body
	}

	if payload.includeProperties {
		envelope["properties"] = payload.Properties
	}

	if payload.includeEdges {
		envelope["edges"] = payload.Edges
	}

	if payload.includePaths {
		envelope["paths"] = payload.Paths
	}

	return json.Marshal(envelope)
}

// buildNodeGetPayload converts node.GetResult to nodeGetPayload, honoring the
// IncludeBody / IncludeEdges / IncludeProperties / IncludePaths flags computed
// by GetRun. Edges and paths are left empty here — node.GetRun reads only the
// nodes table, so the caller hydrates them (query.LoadEdgesForNode,
// query.LoadPathsForNode) when requested, mirroring `query --include`.
func buildNodeGetPayload(result *node.GetResult) nodeGetPayload {
	loaded := result.Node

	return nodeGetPayload{
		ID:                loaded.ID,
		Type:              loaded.Type,
		Path:              loaded.Path,
		Title:             loaded.Title,
		Body:              string(loaded.Body),
		Properties:        loaded.Properties,
		includeBody:       result.IncludeBody,
		includeEdges:      result.IncludeEdges,
		includeProperties: result.IncludeProperties,
		includePaths:      result.IncludePaths,
	}
}

// renderNodeGetCompact emits a compact single-row view of a node-get result.
// Reuses the shared compact renderer; edges carry their in/out direction so the
// §4.4 form (`  → <type> <target>` / `  ← <type> <source>`) renders both sides.
// Body / Properties / Edges are dropped from the rendered row when the
// caller's Include filter excluded them — keeps `--include body` from
// silently bringing back edges and properties.
func renderNodeGetCompact(out io.Writer, payload nodeGetPayload, fields []string) error {
	row := render.CompactRow{
		ID:    payload.ID,
		Type:  payload.Type,
		Title: payload.Title,
	}

	if payload.includeEdges {
		row.Edges = payload.Edges
	}

	if payload.includeBody {
		row.Body = payload.Body
	}

	if payload.includeProperties {
		row.Properties = payload.Properties
	}

	if payload.includePaths {
		row.Paths = payload.Paths
	}

	rows := []render.CompactRow{row}

	return render.CompactNodeRows(out, rows, render.CompactOpts{Fields: fields})
}
