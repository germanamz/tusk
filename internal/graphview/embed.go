package graphview

import "embed"

// distFS holds the built frontend. The dir must live under this package
// directory (go:embed cannot cross package boundaries); Vite builds into it.
// The `all:` prefix includes dotfiles Vite may emit.
//
//go:embed all:dist
var distFS embed.FS
