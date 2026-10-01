// Package claudeplugin installs, inspects and removes the Claude Code plugin
// ("mod") tusk ships, and composes the context block that plugin hands a
// session.
//
// The plugin lives at <workspace>/.claude/skills/tusk/, where Claude Code
// auto-loads it as tusk@skills-dir when a session starts in the workspace
// root. It carries two things: the tusk MCP server (`<bin> mcp`) and a
// hooks module whose prompt.context hook runs `<bin> claude context` and
// appends its output to the first message's context blocks. The binary owns
// the content; the hooks module only moves bytes.
package claudeplugin

import (
	"embed"
	"encoding/json"
	"strings"
)

// PluginName is the plugin's manifest name; Claude Code lists it as
// tusk@skills-dir.
const PluginName = "tusk"

// MCPServerID is the name Claude Code gives the plugin's MCP server. Project
// plugins' servers wait for approval until settings list this name under
// enabledMcpjsonServers.
const MCPServerID = "plugin:tusk:tusk"

// DefaultBin is the binary the plugin runs when the installer is not told
// otherwise: whatever `tusk` resolves to on PATH.
const DefaultBin = "tusk"

//go:embed assets/plugin.json assets/hooks.json assets/register.ts assets/gitignore
var assets embed.FS

// pluginFile maps one embedded asset to its path inside the plugin folder.
type pluginFile struct {
	asset string
	path  string
}

var pluginFiles = []pluginFile{
	{asset: "assets/plugin.json", path: ".claude-plugin/plugin.json"},
	{asset: "assets/hooks.json", path: "hooks/hooks.json"},
	{asset: "assets/register.ts", path: "hooks/register.ts"},
	{asset: "assets/gitignore", path: ".gitignore"},
}

// renderAsset fills an asset's placeholders. Each value is written as a JSON
// string literal, which is valid in both the manifest and the hooks module,
// so a binary path with quotes or backslashes cannot break either file.
func renderAsset(body []byte, bin, version string) string {
	replacer := strings.NewReplacer(
		"__TUSK_BIN__", jsonString(bin),
		"__TUSK_VERSION__", jsonString(version),
		"__TUSK_PLUGIN_VERSION__", jsonString(pluginVersion(version)),
	)

	return replacer.Replace(string(body))
}

// pluginVersion is the manifest's version field: the tusk version without its
// leading "v", the shape plugin manifests use.
func pluginVersion(version string) string {
	return strings.TrimPrefix(version, "v")
}

func jsonString(value string) string {
	encoded, _ := json.Marshal(value)

	return string(encoded)
}
