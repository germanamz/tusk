package graphview

import (
	"encoding/json"
	"net/http"
	"strings"
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
// or unreachable (so semantic ranking can't run), distinguishing it from a real
// index fault. Matches the query layer's "requires [embeddings]" message and
// the embedder's "ollama:" wrapped errors.
func isSemanticUnavailable(err error) bool {
	msg := err.Error()

	return strings.Contains(msg, "requires [embeddings]") || strings.Contains(msg, "ollama:")
}
