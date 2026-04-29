package tui

import (
	"testing"

	"github.com/germanamz/tusk/config"
)

func TestNewApp_NotNil(test *testing.T) {
	// Pass nil dependencies — we only check that New() builds the command tree
	app := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, VersionInfo{}, config.TUIConfig{}, config.MCPConfig{}, config.InlineConfig{}, nil)
	if app == nil {
		test.Fatal("expected non-nil App")
	}
	if app.root == nil {
		test.Fatal("expected non-nil root command")
	}
}

func TestApp_SubcommandsRegistered(test *testing.T) {
	app := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, VersionInfo{}, config.TUIConfig{}, config.MCPConfig{}, config.InlineConfig{}, nil)
	want := []string{"add", "list", "info", "modify", "start", "done", "delete", "annotate", "link", "unlink", "version", "export", "import"}
	cmds := app.root.Commands()
	names := make(map[string]bool)
	for _, cmd := range cmds {
		names[cmd.Name()] = true
	}
	for _, name := range want {
		if !names[name] {
			test.Errorf("expected subcommand %q to be registered", name)
		}
	}
}
