package manifest_test

import (
	"strings"
	"testing"
)

func TestLease_AbsentDefaultsToZero(test *testing.T) {
	loaded := loadInlineManifest(test, `
[workspace]
name = "x"
`)

	if loaded.Lease.TTLSeconds != 0 {
		test.Errorf("Lease.TTLSeconds = %d, want 0 (absent)", loaded.Lease.TTLSeconds)
	}
}

func TestLease_ValidTTLLoadsCleanly(test *testing.T) {
	loaded := loadInlineManifest(test, `
[workspace]
name = "x"

[lease]
ttl_seconds = 90
`)

	if loaded.Lease.TTLSeconds != 90 {
		test.Errorf("Lease.TTLSeconds = %d, want 90", loaded.Lease.TTLSeconds)
	}
}

func TestLease_RejectsZero(test *testing.T) {
	_, loadErr := loadInlineManifestAllowError(test, `
[workspace]
name = "x"

[lease]
ttl_seconds = 0
`)

	if loadErr == nil {
		test.Fatalf("Load: want error for ttl_seconds = 0, got nil")
	}

	if !strings.Contains(loadErr.Error(), "lease.ttl_seconds must be > 0") {
		test.Errorf("Load error = %q, want it to mention 'lease.ttl_seconds must be > 0'", loadErr.Error())
	}
}

func TestLease_RejectsNegative(test *testing.T) {
	_, loadErr := loadInlineManifestAllowError(test, `
[workspace]
name = "x"

[lease]
ttl_seconds = -5
`)

	if loadErr == nil {
		test.Fatalf("Load: want error for ttl_seconds = -5, got nil")
	}

	if !strings.Contains(loadErr.Error(), "lease.ttl_seconds must be > 0") {
		test.Errorf("Load error = %q, want it to mention 'lease.ttl_seconds must be > 0'", loadErr.Error())
	}
}
