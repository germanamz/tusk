// Package version exposes the Tusk binary's release version as a single
// mutable string. Release builds override String via
// -ldflags "-X github.com/germanamz/tusk/internal/version.String=vX.Y.Z";
// dev builds keep the fallback below.
package version

var String = "v1.0.0-dev"
