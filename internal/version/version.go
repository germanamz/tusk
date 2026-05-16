// Package version exposes the Tusk binary's release version as a single
// mutable string. Release builds override Current via
// -ldflags "-X github.com/germanamz/tusk/internal/version.Current=vX.Y.Z";
// dev builds keep the fallback below.
package version

var Current = "v1.0.0-dev"
