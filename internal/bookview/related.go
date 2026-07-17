package bookview

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// handleRelated serves the Related rail: GET /api/related/{id...}. Unlike
// search it is embedder-free — Deps.Related walks only the edge graph, so the
// rail keeps working when Ollama is down. A nil Deps.Related (the CLI hasn't
// wired the Task 3.4 adapter yet, or the workspace has none configured)
// reports an empty rail rather than an error: the rail is a supplementary
// panel, not something the reader depends on to view a node.
//
// hops and weight are optional query params. Absence must reach RelatedSource
// as nil — "inherit the manifest's [query.graph-expansion] default" — not as
// a pointer to zero, so presence is detected with Query().Has before parsing,
// never collapsed into a zero-defaulted parse.
func (srv *Server) handleRelated(writer http.ResponseWriter, request *http.Request) {
	if srv.deps.Related == nil {
		writeJSON(writer, RelatedResponse{Related: make([]RelatedNode, 0)})

		return
	}

	nodeID := request.PathValue("id")

	hops, hopsErr := queryIntParam(request, "hops")

	if hopsErr != nil {
		http.Error(writer, hopsErr.Error(), http.StatusBadRequest)

		return
	}

	weight, weightErr := queryFloatParam(request, "weight")

	if weightErr != nil {
		http.Error(writer, weightErr.Error(), http.StatusBadRequest)

		return
	}

	var edgeTypes []string

	if raw := request.URL.Query().Get("edge_types"); raw != "" {
		edgeTypes = strings.Split(raw, ",")
	}

	resp, relatedErr := srv.deps.Related.Related(request.Context(), nodeID, hops, edgeTypes, weight)

	if relatedErr != nil {
		http.Error(writer, relatedErr.Error(), http.StatusServiceUnavailable)

		return
	}

	// A RelatedSource that found nothing is free to leave Related nil (append
	// onto a nil slice never allocates); guard it here rather than trust every
	// current and future implementation to seed an empty slice, since a nil
	// slice marshals to "null" and this package has bitten on that trap before.
	if resp.Related == nil {
		resp.Related = make([]RelatedNode, 0)
	}

	writeJSON(writer, resp)
}

// queryIntParam reads an optional integer query parameter. An absent param
// returns (nil, nil) so the caller forwards nil unchanged. A present but
// unparseable value is reported rather than silently ignored — a typo'd
// frontend (?hops=tow) gets a 400 with a signal instead of quietly falling
// back to the manifest default.
func queryIntParam(request *http.Request, name string) (*int, error) {
	if !request.URL.Query().Has(name) {
		return nil, nil
	}

	raw := request.URL.Query().Get(name)

	parsed, parseErr := strconv.Atoi(raw)

	if parseErr != nil {
		return nil, fmt.Errorf("%s must be an integer, got %q", name, raw)
	}

	return &parsed, nil
}

// queryFloatParam is queryIntParam's float64 counterpart, for weight.
func queryFloatParam(request *http.Request, name string) (*float64, error) {
	if !request.URL.Query().Has(name) {
		return nil, nil
	}

	raw := request.URL.Query().Get(name)

	parsed, parseErr := strconv.ParseFloat(raw, 64)

	if parseErr != nil {
		return nil, fmt.Errorf("%s must be a number, got %q", name, raw)
	}

	return &parsed, nil
}
