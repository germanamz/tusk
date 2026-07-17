package bookview

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/query"
)

// defaultSearchLimit is applied when a request specifies no positive Limit,
// mirroring graphview's handleQuery.
const defaultSearchLimit = 50

// handleSearch runs a reading-UI search via Deps.Search. Browse and read must
// keep working when search is down, so a missing/unreachable embedder is
// classified as the expected condition it is (the user's Ollama may simply be
// off), not a server fault.
func (srv *Server) handleSearch(writer http.ResponseWriter, request *http.Request) {
	if srv.deps.Search == nil {
		http.Error(writer, "search unavailable", http.StatusServiceUnavailable)

		return
	}

	var req SearchRequest

	if decodeErr := json.NewDecoder(request.Body).Decode(&req); decodeErr != nil {
		http.Error(writer, "bad request body: "+decodeErr.Error(), http.StatusBadRequest)

		return
	}

	if req.Limit <= 0 {
		req.Limit = defaultSearchLimit
	}

	resp, searchErr := srv.deps.Search.Search(request.Context(), req)

	if searchErr != nil {
		if isSemanticUnavailable(searchErr) {
			http.Error(writer, searchErr.Error(), http.StatusUnprocessableEntity)

			return
		}

		http.Error(writer, searchErr.Error(), http.StatusServiceUnavailable)

		return
	}

	// A Searcher that found nothing is free to leave Matches nil (append onto a
	// nil slice never allocates); guard it here rather than trust every current
	// and future implementation to seed an empty slice, since a nil slice
	// marshals to "null" and this package has bitten on that trap before.
	if resp.Matches == nil {
		resp.Matches = make([]Match, 0)
	}

	writeJSON(writer, resp)
}

// isSemanticUnavailable reports whether err indicates the embedder is missing
// (query.ErrSemanticUnavailable) or unreachable (embed.IsTransportError: the
// backend is down, timed out, or returned a 5xx) — matched by error identity,
// never message text, so a reworded error never silently changes the HTTP
// status. Mirrors graphview's handleQuery classification.
func isSemanticUnavailable(err error) bool {
	return errors.Is(err, query.ErrSemanticUnavailable) || embed.IsTransportError(err)
}
