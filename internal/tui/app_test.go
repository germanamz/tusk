package tui

import "testing"

func TestNewApp_NotNil(t *testing.T) {
	// Pass nil dependencies — we only check that New() builds the command tree
	app := New(nil, nil, nil, nil, nil, VersionInfo{})
	if app == nil {
		t.Fatal("expected non-nil App")
	}
	if app.root == nil {
		t.Fatal("expected non-nil root command")
	}
}

func TestApp_SubcommandsRegistered(t *testing.T) {
	app := New(nil, nil, nil, nil, nil, VersionInfo{})
	want := []string{"add", "list", "info", "modify", "start", "done", "delete", "annotate", "link", "unlink", "version"}
	cmds := app.root.Commands()
	names := make(map[string]bool)
	for _, c := range cmds {
		names[c.Name()] = true
	}
	for _, name := range want {
		if !names[name] {
			t.Errorf("expected subcommand %q to be registered", name)
		}
	}
}
