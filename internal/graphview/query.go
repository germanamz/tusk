package graphview

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/query"
)

const defaultQueryLimit = 50

func (srv *Server) handleQuery(writer http.ResponseWriter, request *http.Request) {
	var input QueryInput

	if decodeErr := json.NewDecoder(request.Body).Decode(&input); decodeErr != nil {
		http.Error(writer, "bad request body: "+decodeErr.Error(), http.StatusBadRequest)

		return
	}

	if input.Limit <= 0 {
		input.Limit = defaultQueryLimit
	}

	matches, runErr := srv.deps.Query.Run(request.Context(), input)
	if runErr != nil {
		// A missing/unreachable embedder is a client-actionable condition, not a
		// server fault: 422 so the page shows "semantic search unavailable".
		if isSemanticUnavailable(runErr) {
			http.Error(writer, runErr.Error(), http.StatusUnprocessableEntity)

			return
		}

		http.Error(writer, runErr.Error(), http.StatusServiceUnavailable)

		return
	}

	writeJSON(writer, struct {
		Matches []Match `json:"matches"`
	}{Matches: matches})
}

// isSemanticUnavailable reports whether err indicates the embedder is missing
// (query.ErrSemanticUnavailable) or unreachable (embed.TransportError: the
// backend is down, timed out, or returned a 5xx) — so semantic ranking can't
// run, as distinct from a real index fault or a caller-fixable embedder error
// (a 4xx, a dimension mismatch). Matched by error identity rather than message
// text so a reworded error never silently changes the HTTP status.
func isSemanticUnavailable(err error) bool {
	return errors.Is(err, query.ErrSemanticUnavailable) || embed.IsTransportError(err)
}
