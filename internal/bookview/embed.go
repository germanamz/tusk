package bookview

import "embed"

// distFS holds the built web-book frontend. The directory is committed and
// embedded so the binary serves the reading UI with no build step at run time.
//
//go:embed all:dist
var distFS embed.FS
