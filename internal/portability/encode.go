package portability

import (
	"encoding/json"
	"io"
)

// Encode writes ws as pretty-printed JSON to writer. The output is UTF-8 with
// 2-space indentation; consumers can re-pipe through `jq -c` for a
// compact form. Returns any I/O or encoding error from the underlying
// json.Encoder.
//
// SetEscapeHTML(false) preserves `<`, `>`, `&` literally — tusk dumps are
// not HTML, so escaping them as `<`, `>`, `&` produces
// round-trip-valid but visually confusing output for descriptions and
// note bodies.
func Encode(writer io.Writer, ws *PortableWorkspace) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(ws)
}
