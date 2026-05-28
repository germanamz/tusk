// Package embedconfig resolves the embed/reindex worker pool size used by
// every drain-taking path in the codebase. The resolution is read once at
// process start.
package embedconfig

import (
	"log/slog"
	"os"
	"runtime"
	"strconv"
)

// EnvVar is the environment variable consulted at process start. Set to a
// non-negative integer; malformed or negative values are ignored (with a
// warning) and the resolver falls through to the manifest then the default.
const EnvVar = "TUSK_EMBED_WORKERS"

// ResolveWorkers returns the effective embed/reindex worker pool size.
// Resolution order, highest precedence first:
//
//  1. TUSK_EMBED_WORKERS environment variable (non-negative integer).
//  2. manifestWorkers when non-nil (an explicit 0 is honored).
//  3. max(1, runtime.NumCPU() / 2).
//
// A value of 0 means "opt out of the worker pool in this instance" — no
// goroutines are spawned. Operators are responsible for ensuring another
// instance (or a scheduled `tusk reindex`) keeps the index fresh.
//
// A malformed or negative env value emits a slog.Warn and falls through;
// it never refuses to return a value.
func ResolveWorkers(manifestWorkers *int) int {
	raw, present := os.LookupEnv(EnvVar)

	if present {
		parsed, parseErr := strconv.Atoi(raw)

		switch {
		case parseErr != nil:
			slog.Warn("embedconfig: env value not an integer; falling back",
				"env", EnvVar,
				"value", raw,
				"err", parseErr.Error(),
			)

		case parsed < 0:
			slog.Warn("embedconfig: env value must be >= 0; falling back",
				"env", EnvVar,
				"value", parsed,
			)

		default:
			return parsed
		}
	}

	if manifestWorkers != nil {
		return *manifestWorkers
	}

	return max(1, runtime.NumCPU()/2)
}
