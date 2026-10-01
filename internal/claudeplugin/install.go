package claudeplugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ErrForeignPlugin reports a .claude/skills/tusk folder that tusk did not
// write. Install refuses it unless forced; Uninstall always refuses it.
var ErrForeignPlugin = errors.New("claudeplugin: .claude/skills/tusk is not a tusk-installed plugin")

// ErrNotInstalled reports that the workspace has no plugin folder to remove.
var ErrNotInstalled = errors.New("claudeplugin: the tusk plugin is not installed in this workspace")

// InstallOptions configures Install.
type InstallOptions struct {
	// Root is the workspace root (the folder holding tusk.toml).
	Root string
	// Bin is the binary the plugin runs; empty means DefaultBin.
	Bin string
	// Version is the running binary's version, baked into the plugin so a
	// session can spot drift after an upgrade.
	Version string
	// Force replaces a plugin folder tusk did not write.
	Force bool
	// KeepMCPJSON leaves a .mcp.json `tusk` entry in place.
	KeepMCPJSON bool
}

// Report lists what an Install or Uninstall changed (Notes) and what the user
// should look at (Warnings), one human-readable line each.
type Report struct {
	PluginDir string
	Notes     []string
	Warnings  []string
}

// StatusReport describes the plugin installed in a workspace.
type StatusReport struct {
	PluginDir string
	Installed bool
	// Foreign is true when the folder exists but tusk did not write it.
	Foreign bool
	// Version is the installed plugin's tusk version, without the leading "v".
	Version string
	// Bin is the binary the installed plugin runs.
	Bin string
	// MCPApproved is true when settings.local.json approves the plugin's MCP
	// server.
	MCPApproved bool
	// DuplicateMCPJSON is true when .mcp.json still runs its own tusk server.
	DuplicateMCPJSON bool
}

// PluginDir returns the plugin folder for a workspace root.
func PluginDir(root string) string {
	return filepath.Join(root, ".claude", "skills", PluginName)
}

func settingsLocalPath(root string) string {
	return filepath.Join(root, ".claude", "settings.local.json")
}

func mcpJSONPath(root string) string {
	return filepath.Join(root, ".mcp.json")
}

// Install writes the plugin into root, approves its MCP server for this user,
// and (unless KeepMCPJSON) moves a .mcp.json tusk server into the plugin.
// Re-running it is safe: it rewrites the files it owns.
func Install(options InstallOptions) (*Report, error) {
	bin := options.Bin

	if bin == "" {
		bin = DefaultBin
	}

	dir := PluginDir(options.Root)
	report := &Report{PluginDir: dir}

	manifest, exists, readErr := readPluginManifest(dir)

	if exists && (readErr != nil || manifest.Name != PluginName) {
		if !options.Force {
			return nil, ErrForeignPlugin
		}

		if removeErr := os.RemoveAll(dir); removeErr != nil {
			return nil, fmt.Errorf("claudeplugin: replace %s: %w", dir, removeErr)
		}

		report.Notes = append(report.Notes, fmt.Sprintf("replaced a plugin folder tusk did not write: %s", dir))
	}

	for _, file := range pluginFiles {
		body, assetErr := assets.ReadFile(file.asset)

		if assetErr != nil {
			return nil, assetErr
		}

		target := filepath.Join(dir, filepath.FromSlash(file.path))

		if mkdirErr := os.MkdirAll(filepath.Dir(target), 0o755); mkdirErr != nil {
			return nil, mkdirErr
		}

		if writeErr := os.WriteFile(target, []byte(renderAsset(body, bin, options.Version)), 0o644); writeErr != nil {
			return nil, fmt.Errorf("claudeplugin: write %s: %w", target, writeErr)
		}
	}

	report.Notes = append(report.Notes, fmt.Sprintf("wrote the plugin to %s (runs %s)", dir, bin))

	approved, approveErr := setMCPApproval(options.Root, true)

	if approveErr != nil {
		return nil, fmt.Errorf("claudeplugin: approve MCP server: %w", approveErr)
	}

	if approved {
		report.Notes = append(report.Notes, fmt.Sprintf("approved the plugin's MCP server (%s) in .claude/settings.local.json", MCPServerID))
	}

	if options.KeepMCPJSON {
		return report, nil
	}

	migration, migrateErr := migrateMCPJSON(options.Root)

	if migrateErr != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("left .mcp.json unchanged: %v", migrateErr))

		return report, nil
	}

	switch migration {
	case migrationRemovedEntry:
		report.Notes = append(report.Notes, "removed the tusk server from .mcp.json; the plugin provides it now")
	case migrationRemovedFile:
		report.Notes = append(report.Notes, "removed .mcp.json: its only server was tusk, which the plugin provides now")
	case migrationUnrecognized:
		report.Warnings = append(report.Warnings, ".mcp.json has a \"tusk\" server that does not run `tusk mcp`; left it alone")
	}

	return report, nil
}

// Uninstall removes the plugin folder and its MCP approval. It does not
// restore a .mcp.json entry Install migrated.
func Uninstall(root string) (*Report, error) {
	dir := PluginDir(root)
	manifest, exists, readErr := readPluginManifest(dir)

	if !exists {
		return nil, ErrNotInstalled
	}

	if readErr != nil || manifest.Name != PluginName {
		return nil, ErrForeignPlugin
	}

	if removeErr := os.RemoveAll(dir); removeErr != nil {
		return nil, fmt.Errorf("claudeplugin: remove %s: %w", dir, removeErr)
	}

	report := &Report{PluginDir: dir, Notes: []string{fmt.Sprintf("removed %s", dir)}}

	removed, approveErr := setMCPApproval(root, false)

	if approveErr != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("could not update .claude/settings.local.json: %v", approveErr))
	} else if removed {
		report.Notes = append(report.Notes, fmt.Sprintf("removed the %s approval from .claude/settings.local.json", MCPServerID))
	}

	return report, nil
}

// Status inspects root's plugin folder, its MCP approval and .mcp.json.
func Status(root string) (*StatusReport, error) {
	dir := PluginDir(root)
	report := &StatusReport{PluginDir: dir}
	manifest, exists, readErr := readPluginManifest(dir)

	if exists {
		report.Installed = true
		report.Foreign = readErr != nil || manifest.Name != PluginName
		report.Version = manifest.Version
		report.Bin = manifest.MCPServers[PluginName].Command
	}

	approved, approvalErr := mcpApproved(root)

	if approvalErr != nil {
		return nil, approvalErr
	}

	report.MCPApproved = approved

	duplicate, duplicateErr := mcpJSONRunsTusk(root)

	if duplicateErr != nil {
		return nil, duplicateErr
	}

	report.DuplicateMCPJSON = duplicate

	return report, nil
}

// pluginManifest is the subset of plugin.json Status and the ownership check
// read.
type pluginManifest struct {
	Name       string                     `json:"name"`
	Version    string                     `json:"version"`
	MCPServers map[string]mcpServerConfig `json:"mcpServers"`
}

type mcpServerConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// readPluginManifest reads dir's plugin.json. exists reports whether dir
// itself exists, so a folder with a missing or broken manifest reads as
// (exists, error).
func readPluginManifest(dir string) (pluginManifest, bool, error) {
	var manifest pluginManifest

	if _, statErr := os.Stat(dir); errors.Is(statErr, os.ErrNotExist) {
		return manifest, false, nil
	}

	raw, readErr := os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))

	if readErr != nil {
		return manifest, true, readErr
	}

	if decodeErr := json.Unmarshal(raw, &manifest); decodeErr != nil {
		return manifest, true, decodeErr
	}

	return manifest, true, nil
}

// setMCPApproval adds MCPServerID to (approve) or removes it from (!approve)
// enabledMcpjsonServers in .claude/settings.local.json, preserving every other
// setting. It reports whether the file changed. Removing the last entry drops
// the key, and a file left empty is deleted.
func setMCPApproval(root string, approve bool) (bool, error) {
	path := settingsLocalPath(root)
	object, exists, readErr := readJSONObject(path)

	if readErr != nil {
		return false, readErr
	}

	if !exists {
		if !approve {
			return false, nil
		}

		object = &jsonObject{}
	}

	servers, listErr := enabledServers(object)

	if listErr != nil {
		return false, listErr
	}

	has := slices.Contains(servers, MCPServerID)

	if has == approve {
		return false, nil
	}

	if approve {
		servers = append(servers, MCPServerID)
	} else {
		servers = slices.DeleteFunc(servers, func(name string) bool { return name == MCPServerID })
	}

	if len(servers) == 0 {
		object.remove("enabledMcpjsonServers")
	} else {
		encoded, encodeErr := json.Marshal(servers)

		if encodeErr != nil {
			return false, encodeErr
		}

		object.set("enabledMcpjsonServers", encoded)
	}

	if object.isEmpty() {
		return true, os.Remove(path)
	}

	return true, writeJSONObject(path, object)
}

func mcpApproved(root string) (bool, error) {
	object, exists, readErr := readJSONObject(settingsLocalPath(root))

	if readErr != nil || !exists {
		return false, readErr
	}

	servers, listErr := enabledServers(object)

	if listErr != nil {
		return false, listErr
	}

	return slices.Contains(servers, MCPServerID), nil
}

func enabledServers(object *jsonObject) ([]string, error) {
	raw, present := object.get("enabledMcpjsonServers")

	if !present {
		return nil, nil
	}

	var servers []string

	if decodeErr := json.Unmarshal(raw, &servers); decodeErr != nil {
		return nil, fmt.Errorf("enabledMcpjsonServers is not a list of names: %w", decodeErr)
	}

	return servers, nil
}

type mcpMigration int

const (
	migrationNone mcpMigration = iota
	migrationRemovedEntry
	migrationRemovedFile
	migrationUnrecognized
)

// migrateMCPJSON removes a .mcp.json `tusk` server that runs `<…tusk> mcp`,
// since the plugin now provides it. Other servers and keys keep their
// order; a file whose only content was that server is deleted. A `tusk` entry
// of any other shape is reported, not touched.
func migrateMCPJSON(root string) (mcpMigration, error) {
	path := mcpJSONPath(root)
	object, exists, readErr := readJSONObject(path)

	if readErr != nil || !exists {
		return migrationNone, readErr
	}

	servers, entry, found, entryErr := mcpJSONTuskEntry(object)

	if entryErr != nil || !found {
		return migrationNone, entryErr
	}

	if !isTuskServer(entry) {
		return migrationUnrecognized, nil
	}

	servers.remove(PluginName)

	if servers.isEmpty() {
		object.remove("mcpServers")
	} else {
		encoded, encodeErr := servers.marshal()

		if encodeErr != nil {
			return migrationNone, encodeErr
		}

		object.set("mcpServers", encoded)
	}

	if object.isEmpty() {
		return migrationRemovedFile, os.Remove(path)
	}

	return migrationRemovedEntry, writeJSONObject(path, object)
}

func mcpJSONRunsTusk(root string) (bool, error) {
	object, exists, readErr := readJSONObject(mcpJSONPath(root))

	if readErr != nil || !exists {
		return false, readErr
	}

	_, entry, found, entryErr := mcpJSONTuskEntry(object)

	if entryErr != nil {
		return false, entryErr
	}

	return found && isTuskServer(entry), nil
}

// mcpJSONTuskEntry returns .mcp.json's mcpServers object and its `tusk`
// entry, when there is one.
func mcpJSONTuskEntry(object *jsonObject) (*jsonObject, mcpServerConfig, bool, error) {
	var entry mcpServerConfig

	rawServers, hasServers := object.get("mcpServers")

	if !hasServers {
		return nil, entry, false, nil
	}

	servers, serversErr := parseJSONObject(rawServers)

	if serversErr != nil {
		return nil, entry, false, fmt.Errorf("mcpServers: %w", serversErr)
	}

	rawEntry, hasEntry := servers.get(PluginName)

	if !hasEntry {
		return servers, entry, false, nil
	}

	// An entry that does not decode as a server config reads as an
	// unrecognized one: isTuskServer rejects its zero value.
	_ = json.Unmarshal(rawEntry, &entry)

	return servers, entry, true, nil
}

// isTuskServer reports whether an MCP server config runs the tusk stdio
// server, through any path to a binary named tusk: `tusk mcp`, or the
// `tusk mcp serve` spelling older configs carry (mcp ignores the extra word).
func isTuskServer(entry mcpServerConfig) bool {
	base := strings.TrimSuffix(filepath.Base(entry.Command), ".exe")
	args := strings.Join(entry.Args, " ")

	return base == "tusk" && (args == "mcp" || args == "mcp serve")
}
