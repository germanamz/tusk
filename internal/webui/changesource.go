package webui

import (
	"strconv"

	"github.com/germanamz/tusk/internal/epoch"
)

// Signal is the current vault state for change-detection: the reindex
// generation (from manifest) and the index epoch (from .tusk/epoch).
type Signal struct {
	Generation int64 `json:"generation"`
	Epoch      int64 `json:"epoch"`
}

// MetaReader is the subset of *index.MetaRepo the change source needs.
type MetaReader interface {
	Get(key string) (string, error)
}

// ChangeSource reports the vault's dual change signal (reindex generation +
// epoch) for SSE subscribers and status observers.
type ChangeSource interface {
	Signal() (Signal, error)
}

type changeSource struct {
	root string
	meta MetaReader
}

// NewChangeSource builds a ChangeSource from the workspace root and meta repo.
func NewChangeSource(root string, meta MetaReader) ChangeSource {
	return &changeSource{root: root, meta: meta}
}

func (source *changeSource) Signal() (Signal, error) {
	var sig Signal

	gen, err := source.meta.Get("reindex_gen")
	if err != nil {
		return sig, err
	}

	if gen != "" {
		if parsed, perr := strconv.ParseInt(gen, 10, 64); perr == nil {
			sig.Generation = parsed
		}
	}

	epochValue, eerr := epoch.Index.Read(source.root)
	if eerr != nil {
		return sig, eerr
	}

	sig.Epoch = epochValue

	return sig, nil
}
