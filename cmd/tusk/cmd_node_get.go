package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/query"
	"github.com/germanamz/tusk/internal/reindex"
	"github.com/germanamz/tusk/internal/render"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/germanamz/tusk/internal/workspace/indexopen"
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
		Short: "Print the markdown file for a node by id",
		Long: `Print the markdown file (frontmatter + body) for a node by id.

The node id is the workspace-relative path without extension (e.g. a node
file at notes/hello.md has id "notes/hello").

By default (no flags) the command prints the raw markdown file to stdout
verbatim — useful for piping into editors, less, or another tusk command.
When --include, --fields, --format, or --json is passed the command emits
structured output instead (compact for TTY, JSON otherwise).`,
		Example: `  # Print the raw file
  tusk node get notes/hello

  # Structured JSON envelope with only the body
  tusk node get notes/hello --include body --format json

  # Open in $EDITOR (round-trip through a temp file)
  tusk node get notes/hello > /tmp/hello.md && $EDITOR /tmp/hello.md`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, cwdErr := os.Getwd()

			if cwdErr != nil {
				return cwdErr
			}

			ws, findErr := workspace.Find(cwd)

			if findErr != nil {
				return fmt.Errorf("workspace: %w", findErr)
			}

			loaded, loadErr := manifest.Load(ws.ManifestPath)

			if loadErr != nil {
				return loadErr
			}

			manifest.MergeBuiltinPacks(loaded)

			store, openErr := indexopen.OpenOrRebuild(cmd.Context(), indexopen.Config{
				IndexPath: ws.IndexPath,
				ReindexFactory: func(idx *index.Index) reindex.Config {
					return reindex.Config{
						Root:       ws.Root,
						Repo:       index.NewNodeRepo(idx),
						Edges:      index.NewEdgeRepo(idx),
						EdgeTypes:  loaded.EdgeTypes,
						Meta:       index.NewMetaRepo(idx),
						FileStates: index.NewFileStateRepo(idx),
						EmbedQueue: index.NewEmbedQueueRepo(idx),
						Workers:    resolveEmbedWorkers(loaded),
					}
				},
				Logger: func(msg string) {
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), msg)
				},
			})

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

	getCmd.Flags().StringSliceVar(&includeFlag, "include", nil, "expand returned shape: body|edges|properties (comma-separated)")
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
	Edges      map[string][]string

	includeBody       bool
	includeEdges      bool
	includeProperties bool
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

	return json.Marshal(envelope)
}

// buildNodeGetPayload converts node.GetResult to nodeGetPayload, honoring the
// IncludeBody / IncludeEdges / IncludeProperties flags computed by GetRun.
func buildNodeGetPayload(result *node.GetResult) nodeGetPayload {
	loaded := result.Node

	return nodeGetPayload{
		ID:                loaded.ID,
		Type:              loaded.Type,
		Path:              loaded.Path,
		Title:             loaded.Title,
		Body:              string(loaded.Body),
		Properties:        loaded.Properties,
		Edges:             loaded.Edges,
		includeBody:       result.IncludeBody,
		includeEdges:      result.IncludeEdges,
		includeProperties: result.IncludeProperties,
	}
}

// renderNodeGetCompact emits a compact single-row view of a node-get result.
// Reuses the shared compact renderer; edges are flattened to []EdgeRef with
// direction="out" so the §4.4 form (`  → <type> <target>`) renders.
// Body / Properties / Edges are dropped from the rendered row when the
// caller's Include filter excluded them — keeps `--include body` from
// silently bringing back edges and properties.
func renderNodeGetCompact(out io.Writer, payload nodeGetPayload, fields []string) error {
	var edgeRefs []query.EdgeRef

	if payload.includeEdges {
		for edgeType, targets := range payload.Edges {
			for _, target := range targets {
				edgeRefs = append(edgeRefs, query.EdgeRef{
					Type:      edgeType,
					Direction: "out",
					TargetID:  target,
				})
			}
		}
	}

	row := render.CompactRow{
		ID:    payload.ID,
		Type:  payload.Type,
		Title: payload.Title,
		Edges: edgeRefs,
	}

	if payload.includeBody {
		row.Body = payload.Body
	}

	if payload.includeProperties {
		row.Properties = payload.Properties
	}

	rows := []render.CompactRow{row}

	return render.CompactNodeRows(out, rows, render.CompactOpts{Fields: fields})
}
