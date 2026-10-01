package claudeplugin

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(test *testing.T, path, body string) {
	test.Helper()

	if mkdirErr := os.MkdirAll(filepath.Dir(path), 0o755); mkdirErr != nil {
		test.Fatal(mkdirErr)
	}

	if writeErr := os.WriteFile(path, []byte(body), 0o644); writeErr != nil {
		test.Fatal(writeErr)
	}
}

func readFile(test *testing.T, path string) string {
	test.Helper()

	body, readErr := os.ReadFile(path)

	if readErr != nil {
		test.Fatal(readErr)
	}

	return string(body)
}

func install(test *testing.T, options InstallOptions) *Report {
	test.Helper()

	report, installErr := Install(options)

	if installErr != nil {
		test.Fatalf("Install: %v", installErr)
	}

	return report
}

func TestInstall_WritesPluginWithSubstitutions(test *testing.T) {
	root := test.TempDir()
	bin := `/opt/my "tusk"/bin/tusk`

	install(test, InstallOptions{Root: root, Bin: bin, Version: "v2.5.0"})

	dir := PluginDir(root)

	var manifest pluginManifest

	if decodeErr := json.Unmarshal([]byte(readFile(test, filepath.Join(dir, ".claude-plugin", "plugin.json"))), &manifest); decodeErr != nil {
		test.Fatalf("plugin.json is not valid JSON: %v", decodeErr)
	}

	if manifest.Name != PluginName || manifest.Version != "2.5.0" {
		test.Errorf("manifest name/version = %q/%q", manifest.Name, manifest.Version)
	}

	server := manifest.MCPServers[PluginName]

	if server.Command != bin || strings.Join(server.Args, " ") != "mcp" {
		test.Errorf("mcp server = %+v", server)
	}

	var hooks struct {
		Modules []string `json:"modules"`
	}

	if decodeErr := json.Unmarshal([]byte(readFile(test, filepath.Join(dir, "hooks", "hooks.json"))), &hooks); decodeErr != nil || len(hooks.Modules) != 1 {
		test.Errorf("hooks.json = %+v (%v)", hooks, decodeErr)
	}

	module := readFile(test, filepath.Join(dir, "hooks", "register.ts"))

	for _, want := range []string{`const BIN = "/opt/my \"tusk\"/bin/tusk"`, `const VERSION = "v2.5.0"`} {
		if !strings.Contains(module, want) {
			test.Errorf("register.ts missing %s", want)
		}
	}

	if strings.Contains(module, "__TUSK_") {
		test.Errorf("register.ts kept a placeholder")
	}

	if !strings.Contains(readFile(test, filepath.Join(dir, ".gitignore")), ".claude-plugin/types/") {
		test.Errorf(".gitignore does not exclude generated types")
	}
}

func TestInstall_DefaultBinAndRerunIsSafe(test *testing.T) {
	root := test.TempDir()

	install(test, InstallOptions{Root: root, Version: "v1.0.0"})
	report := install(test, InstallOptions{Root: root, Version: "v1.1.0"})

	status, statusErr := Status(root)

	if statusErr != nil {
		test.Fatal(statusErr)
	}

	if !status.Installed || status.Foreign || status.Version != "1.1.0" || status.Bin != DefaultBin {
		test.Errorf("status after rerun = %+v", status)
	}

	for _, note := range report.Notes {
		if strings.Contains(note, "approved") {
			test.Errorf("rerun re-approved an already approved server: %q", note)
		}
	}
}

func TestInstall_RefusesForeignPluginUnlessForced(test *testing.T) {
	root := test.TempDir()
	dir := PluginDir(root)
	writeFile(test, filepath.Join(dir, ".claude-plugin", "plugin.json"), `{"name": "someone-else"}`)
	writeFile(test, filepath.Join(dir, "keep.txt"), "mine")

	if _, installErr := Install(InstallOptions{Root: root, Version: "v1.0.0"}); !errors.Is(installErr, ErrForeignPlugin) {
		test.Fatalf("Install error = %v, want ErrForeignPlugin", installErr)
	}

	if readFile(test, filepath.Join(dir, "keep.txt")) != "mine" {
		test.Fatalf("refused install touched the foreign folder")
	}

	install(test, InstallOptions{Root: root, Version: "v1.0.0", Force: true})

	if _, statErr := os.Stat(filepath.Join(dir, "keep.txt")); !errors.Is(statErr, os.ErrNotExist) {
		test.Errorf("forced install kept the foreign folder's files")
	}
}

func TestInstall_ApprovesMCPServerPreservingSettings(test *testing.T) {
	root := test.TempDir()
	settings := settingsLocalPath(root)
	writeFile(test, settings, `{
  "permissions": {"allow": ["Bash(ls:*)"]},
  "enabledMcpjsonServers": ["other"]
}
`)

	install(test, InstallOptions{Root: root, Version: "v1.0.0"})

	want := `{
  "permissions": {
    "allow": [
      "Bash(ls:*)"
    ]
  },
  "enabledMcpjsonServers": [
    "other",
    "plugin:tusk:tusk"
  ]
}
`

	if got := readFile(test, settings); got != want {
		test.Errorf("settings.local.json =\n%s\nwant\n%s", got, want)
	}
}

func TestInstall_MigratesMCPJSON(test *testing.T) {
	cases := []struct {
		name     string
		mcpJSON  string
		keep     bool
		want     string
		removed  bool
		warnings int
	}{
		{
			name:    "only tusk: file removed",
			mcpJSON: `{"mcpServers": {"tusk": {"command": "tusk", "args": ["mcp", "serve"], "type": "stdio"}}}`,
			removed: true,
		},
		{
			name:    "plain tusk mcp: file removed",
			mcpJSON: `{"mcpServers": {"tusk": {"command": "/usr/local/bin/tusk", "args": ["mcp"]}}}`,
			removed: true,
		},
		{
			name:    "tusk among others: entry removed, order kept",
			mcpJSON: `{"mcpServers": {"zeta": {"command": "z"}, "tusk": {"command": "./bin/tusk", "args": ["mcp", "serve"]}, "alpha": {"command": "a"}}}`,
			want: `{
  "mcpServers": {
    "zeta": {
      "command": "z"
    },
    "alpha": {
      "command": "a"
    }
  }
}
`,
		},
		{
			name:     "unrecognized tusk entry: left alone",
			mcpJSON:  `{"mcpServers": {"tusk": {"command": "node", "args": ["server.js"]}}}`,
			want:     `{"mcpServers": {"tusk": {"command": "node", "args": ["server.js"]}}}`,
			warnings: 1,
		},
		{
			name:    "keep flag: untouched",
			mcpJSON: `{"mcpServers": {"tusk": {"command": "tusk", "args": ["mcp", "serve"]}}}`,
			keep:    true,
			want:    `{"mcpServers": {"tusk": {"command": "tusk", "args": ["mcp", "serve"]}}}`,
		},
		{
			name:     "broken JSON: warned, untouched",
			mcpJSON:  `{"mcpServers": `,
			want:     `{"mcpServers": `,
			warnings: 1,
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			root := test.TempDir()
			path := mcpJSONPath(root)
			writeFile(test, path, testCase.mcpJSON)

			report := install(test, InstallOptions{Root: root, Version: "v1.0.0", KeepMCPJSON: testCase.keep})

			if len(report.Warnings) != testCase.warnings {
				test.Errorf("warnings = %v, want %d", report.Warnings, testCase.warnings)
			}

			if testCase.removed {
				if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
					test.Errorf(".mcp.json still exists")
				}

				return
			}

			if got := readFile(test, path); got != testCase.want {
				test.Errorf(".mcp.json =\n%s\nwant\n%s", got, testCase.want)
			}
		})
	}
}

func TestUninstall_RemovesPluginAndApproval(test *testing.T) {
	root := test.TempDir()
	settings := settingsLocalPath(root)
	writeFile(test, settings, `{"model": "opus"}`)

	install(test, InstallOptions{Root: root, Version: "v1.0.0"})

	if _, uninstallErr := Uninstall(root); uninstallErr != nil {
		test.Fatalf("Uninstall: %v", uninstallErr)
	}

	if _, statErr := os.Stat(PluginDir(root)); !errors.Is(statErr, os.ErrNotExist) {
		test.Errorf("plugin folder survived uninstall")
	}

	if got, want := readFile(test, settings), "{\n  \"model\": \"opus\"\n}\n"; got != want {
		test.Errorf("settings.local.json = %q, want %q", got, want)
	}
}

func TestUninstall_DeletesSettingsItCreated(test *testing.T) {
	root := test.TempDir()

	install(test, InstallOptions{Root: root, Version: "v1.0.0"})

	if _, uninstallErr := Uninstall(root); uninstallErr != nil {
		test.Fatalf("Uninstall: %v", uninstallErr)
	}

	if _, statErr := os.Stat(settingsLocalPath(root)); !errors.Is(statErr, os.ErrNotExist) {
		test.Errorf("settings.local.json left behind holding nothing")
	}
}

func TestUninstall_RefusesMissingAndForeign(test *testing.T) {
	root := test.TempDir()

	if _, uninstallErr := Uninstall(root); !errors.Is(uninstallErr, ErrNotInstalled) {
		test.Errorf("Uninstall on empty root = %v, want ErrNotInstalled", uninstallErr)
	}

	writeFile(test, filepath.Join(PluginDir(root), ".claude-plugin", "plugin.json"), `{"name": "other"}`)

	if _, uninstallErr := Uninstall(root); !errors.Is(uninstallErr, ErrForeignPlugin) {
		test.Errorf("Uninstall on foreign plugin = %v, want ErrForeignPlugin", uninstallErr)
	}
}

func TestStatus_ReportsDuplicateAndApproval(test *testing.T) {
	root := test.TempDir()
	writeFile(test, mcpJSONPath(root), `{"mcpServers": {"tusk": {"command": "tusk", "args": ["mcp", "serve"]}}}`)

	status, statusErr := Status(root)

	if statusErr != nil {
		test.Fatal(statusErr)
	}

	if status.Installed || status.MCPApproved || !status.DuplicateMCPJSON {
		test.Errorf("status before install = %+v", status)
	}

	install(test, InstallOptions{Root: root, Version: "v1.0.0", KeepMCPJSON: true})

	status, statusErr = Status(root)

	if statusErr != nil {
		test.Fatal(statusErr)
	}

	if !status.Installed || !status.MCPApproved || !status.DuplicateMCPJSON {
		test.Errorf("status after install = %+v", status)
	}
}
