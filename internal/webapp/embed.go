package webapp

import "embed"

// distFS holds the built unified frontend. The dir lives under this package
// directory (go:embed cannot cross package boundaries); the Vite build in web/
// emits into it. The all: prefix includes any dotfiles Vite emits.
//
//go:embed all:dist
var distFS embed.FS
