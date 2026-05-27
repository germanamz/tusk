package leaseconfig_test

import (
	"os"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/leaseconfig"
)

// unsetEnv removes TUSK_LEASE_TTL_SECONDS for the duration of the test.
// t.Setenv only sets values; this helper handles the "absent" case the
// resolver treats specially. The Cleanup restores whatever the host
// process had set so tests stay isolated.
func unsetEnv(test *testing.T) {
	test.Helper()

	original, present := os.LookupEnv(leaseconfig.EnvVar)

	if unsetErr := os.Unsetenv(leaseconfig.EnvVar); unsetErr != nil {
		test.Fatalf("unset env: %v", unsetErr)
	}

	test.Cleanup(func() {
		if present {
			_ = os.Setenv(leaseconfig.EnvVar, original)

			return
		}

		_ = os.Unsetenv(leaseconfig.EnvVar)
	})
}

func TestResolve_EnvUnsetManifestZero_UsesDefault(test *testing.T) {
	unsetEnv(test)

	got := leaseconfig.Resolve(0)
	want := time.Duration(leaseconfig.DefaultTTLSeconds) * time.Second

	if got != want {
		test.Errorf("Resolve(0) = %v, want %v", got, want)
	}
}

func TestResolve_EnvUnsetManifestPositive_UsesManifest(test *testing.T) {
	unsetEnv(test)

	got := leaseconfig.Resolve(120)

	if got != 120*time.Second {
		test.Errorf("Resolve(120) = %v, want 120s", got)
	}
}

func TestResolve_EnvSetOverridesManifest(test *testing.T) {
	test.Setenv(leaseconfig.EnvVar, "30")

	got := leaseconfig.Resolve(120)

	if got != 30*time.Second {
		test.Errorf("Resolve(120) with env=30 = %v, want 30s", got)
	}
}

func TestResolve_EnvGarbageFallsThroughToManifest(test *testing.T) {
	test.Setenv(leaseconfig.EnvVar, "garbage")

	got := leaseconfig.Resolve(45)

	if got != 45*time.Second {
		test.Errorf("Resolve(45) with env=garbage = %v, want 45s", got)
	}
}

func TestResolve_EnvGarbageManifestAbsentFallsThroughToDefault(test *testing.T) {
	test.Setenv(leaseconfig.EnvVar, "not-a-number")

	got := leaseconfig.Resolve(0)
	want := time.Duration(leaseconfig.DefaultTTLSeconds) * time.Second

	if got != want {
		test.Errorf("Resolve(0) with env=garbage = %v, want %v", got, want)
	}
}

func TestResolve_EnvZeroFallsThrough(test *testing.T) {
	test.Setenv(leaseconfig.EnvVar, "0")

	got := leaseconfig.Resolve(45)

	if got != 45*time.Second {
		test.Errorf("Resolve(45) with env=0 = %v, want 45s", got)
	}
}

func TestResolve_EnvNegativeFallsThrough(test *testing.T) {
	test.Setenv(leaseconfig.EnvVar, "-5")

	got := leaseconfig.Resolve(45)

	if got != 45*time.Second {
		test.Errorf("Resolve(45) with env=-5 = %v, want 45s", got)
	}
}
