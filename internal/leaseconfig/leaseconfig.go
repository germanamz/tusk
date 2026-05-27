// Package leaseconfig resolves the lease TTL used by every lease-taking
// path in the codebase (today: embed_queue Drain; later: file_state
// Claim). The resolution is read once at process start.
package leaseconfig

import (
	"log/slog"
	"os"
	"strconv"
	"time"
)

// DefaultTTLSeconds is the spec-mandated fallback lease window in seconds
// when neither the env var nor the manifest provides a value.
const DefaultTTLSeconds = 60

// EnvVar is the environment variable consulted at process start. Set to an
// integer number of seconds; non-positive or malformed values are ignored
// (with a warning) and the resolver falls through to the manifest then the
// default.
const EnvVar = "TUSK_LEASE_TTL_SECONDS"

// Resolve returns the effective lease TTL. Resolution order, highest
// precedence first:
//
//  1. TUSK_LEASE_TTL_SECONDS environment variable (positive integer seconds).
//  2. manifestTTL (positive integer seconds) — pass 0 when the manifest does
//     not declare a value.
//  3. DefaultTTLSeconds.
//
// A malformed or non-positive env value emits a slog.Warn and falls
// through; it never refuses to return a value.
func Resolve(manifestTTL int) time.Duration {
	raw, present := os.LookupEnv(EnvVar)

	if present {
		seconds, parseErr := strconv.Atoi(raw)

		switch {
		case parseErr != nil:
			slog.Warn("leaseconfig: env value not an integer; falling back",
				"env", EnvVar,
				"value", raw,
				"err", parseErr.Error(),
			)

		case seconds <= 0:
			slog.Warn("leaseconfig: env value must be > 0; falling back",
				"env", EnvVar,
				"value", seconds,
			)

		default:
			return time.Duration(seconds) * time.Second
		}
	}

	if manifestTTL > 0 {
		return time.Duration(manifestTTL) * time.Second
	}

	return DefaultTTLSeconds * time.Second
}
