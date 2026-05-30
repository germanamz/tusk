package main

import "testing"

// goldenPackBody is a minimal file:// pack: one node type, no edge types.
const goldenPackBody = `[node-types.widget]
description = "A widget"
properties = []
`

// packArgs builds the `pack add file://<root>/mypack.toml` invocation (optionally
// with --force); the file:// scheme keeps the fetch offline and deterministic.
func packArgs(force bool) func(root string) []string {
	return func(root string) []string {
		args := []string{"pack", "add", "file://" + root + "/mypack.toml"}

		if force {
			args = append(args, "--force")
		}

		return args
	}
}

// seedPack writes the pack file and applies it once, so a follow-up add collides.
func seedPack(test *testing.T, root string) {
	test.Helper()

	writeFile(test, root, "mypack.toml", goldenPackBody)

	if _, stderr, ok := runCLISplit(root, packArgs(false)(root)...); !ok {
		test.Fatalf("seed pack add: %s", stderr.String())
	}
}

// TestGoldenCLI_PackAdd pins the fast-tier pack add paths: a successful file://
// apply, the collision guard (exit 1), and --force re-apply. The os.Exit(2|3)
// fetch/decode paths are deferred to the wire tier (they would kill the test
// binary in-process).
func TestGoldenCLI_PackAdd(test *testing.T) {
	runGoldenCLICases(test, []goldenCLICase{
		{
			name: "pack add applies a file:// pack",
			setup: func(test *testing.T, root string) {
				writeFile(test, root, "mypack.toml", goldenPackBody)
			},
			argsFunc:   packArgs(false),
			wantStdout: "pack add: applied \"file://<WS>/mypack.toml\" to tusk.toml\n",
		},
		{
			name:     "pack add collides without --force",
			setup:    seedPack,
			argsFunc: packArgs(false),
			wantErr:  true,
			wantStderr: "pack add: cannot apply pack from file://<WS>/mypack.toml: 1 colliding sections in tusk.toml:\n" +
				"  - [node-types.widget]\n" +
				"re-run with --force to overwrite, or remove the colliding sections by hand\n",
		},
		{
			name:       "pack add --force re-applies",
			setup:      seedPack,
			argsFunc:   packArgs(true),
			wantStdout: "pack add: applied \"file://<WS>/mypack.toml\" to tusk.toml\n",
		},
	})
}

// TestGoldenCLI_ConfigValidation pins how a malformed tusk.toml surfaces: the
// manifest decode error from any command that loads it.
func TestGoldenCLI_ConfigValidation(test *testing.T) {
	runGoldenCLICases(test, []goldenCLICase{
		{
			name: "malformed tusk.toml is rejected on load",
			setup: func(test *testing.T, root string) {
				writeFile(test, root, "tusk.toml", "this is not valid toml\n")
			},
			args:    []string{"status"},
			wantErr: true,
			// Only the Tusk-authored prefix is pinned; the trailing parse detail
			// belongs to the BurntSushi/toml library and may drift across versions.
			wantStderrContains: "manifest: decode <WS>/tusk.toml: toml:",
		},
	})
}
