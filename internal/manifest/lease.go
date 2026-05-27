package manifest

import "fmt"

// LeaseSection holds the `[lease]` block of tusk.toml.
//
// The single value covers every lease-taking path in the codebase
// (today: embed_queue Drain; Phase 4 onwards: file_state Claim). One
// config, one value.
//
// The TTL is read once at process start by the leaseconfig resolver;
// changing it requires a restart.
type LeaseSection struct {
	// TTLSeconds is the lease window in seconds. Zero (absent) means the
	// resolver falls back to the env var then the default. A non-zero
	// value must be positive — the loader rejects 0-or-negative explicit
	// values.
	TTLSeconds int `toml:"ttl_seconds"`
}

// validateLease enforces the per-field rules for the `[lease]` block.
// Called from Validate after the primary decode populates Manifest.Lease.
func validateLease(loaded *Manifest) error {
	if loaded.Meta == nil {
		return nil
	}

	if !loaded.Meta.IsDefined("lease", "ttl_seconds") {
		return nil
	}

	if loaded.Lease.TTLSeconds <= 0 {
		return fmt.Errorf("manifest: lease.ttl_seconds must be > 0 (got %d)", loaded.Lease.TTLSeconds)
	}

	return nil
}
