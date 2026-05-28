package embedconfig_test

import (
	"os"
	"runtime"
	"testing"

	"github.com/germanamz/tusk/internal/embedconfig"
)

// unsetEnv removes TUSK_EMBED_WORKERS for the duration of the test.
// t.Setenv only sets values; this helper handles the "absent" case the
// resolver treats specially.
func unsetEnv(test *testing.T) {
	test.Helper()

	original, present := os.LookupEnv(embedconfig.EnvVar)

	if unsetErr := os.Unsetenv(embedconfig.EnvVar); unsetErr != nil {
		test.Fatalf("unset env: %v", unsetErr)
	}

	test.Cleanup(func() {
		if present {
			_ = os.Setenv(embedconfig.EnvVar, original)

			return
		}

		_ = os.Unsetenv(embedconfig.EnvVar)
	})
}

func intPtr(value int) *int {
	return &value
}

func TestResolveWorkers_EnvUnsetManifestNil_UsesDefault(test *testing.T) {
	unsetEnv(test)

	got := embedconfig.ResolveWorkers(nil)
	want := max(1, runtime.NumCPU()/2)

	if got != want {
		test.Errorf("ResolveWorkers(nil) = %d, want %d", got, want)
	}
}

func TestResolveWorkers_EnvUnsetManifestPositive_UsesManifest(test *testing.T) {
	unsetEnv(test)

	got := embedconfig.ResolveWorkers(intPtr(4))

	if got != 4 {
		test.Errorf("ResolveWorkers(*4) = %d, want 4", got)
	}
}

func TestResolveWorkers_EnvUnsetManifestZero_HonorsZero(test *testing.T) {
	unsetEnv(test)

	got := embedconfig.ResolveWorkers(intPtr(0))

	if got != 0 {
		test.Errorf("ResolveWorkers(*0) = %d, want 0", got)
	}
}

func TestResolveWorkers_EnvSetOverridesManifest(test *testing.T) {
	test.Setenv(embedconfig.EnvVar, "8")

	got := embedconfig.ResolveWorkers(intPtr(4))

	if got != 8 {
		test.Errorf("ResolveWorkers(*4) with env=8 = %d, want 8", got)
	}
}

func TestResolveWorkers_EnvZeroOverridesManifest(test *testing.T) {
	test.Setenv(embedconfig.EnvVar, "0")

	got := embedconfig.ResolveWorkers(intPtr(4))

	if got != 0 {
		test.Errorf("ResolveWorkers(*4) with env=0 = %d, want 0", got)
	}
}

func TestResolveWorkers_EnvGarbageFallsThroughToManifest(test *testing.T) {
	test.Setenv(embedconfig.EnvVar, "garbage")

	got := embedconfig.ResolveWorkers(intPtr(4))

	if got != 4 {
		test.Errorf("ResolveWorkers(*4) with env=garbage = %d, want 4", got)
	}
}

func TestResolveWorkers_EnvNegativeFallsThroughToManifest(test *testing.T) {
	test.Setenv(embedconfig.EnvVar, "-1")

	got := embedconfig.ResolveWorkers(intPtr(4))

	if got != 4 {
		test.Errorf("ResolveWorkers(*4) with env=-1 = %d, want 4", got)
	}
}

func TestResolveWorkers_EnvGarbageManifestNilFallsThroughToDefault(test *testing.T) {
	test.Setenv(embedconfig.EnvVar, "garbage")

	got := embedconfig.ResolveWorkers(nil)
	want := max(1, runtime.NumCPU()/2)

	if got != want {
		test.Errorf("ResolveWorkers(nil) with env=garbage = %d, want %d", got, want)
	}
}
