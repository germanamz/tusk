package tui

import "testing"

func TestParseProjectCreate_Basic(t *testing.T) {
	proj, err := parseProjectCreate([]string{"workflow=kanban", "db-path=/tmp/b.db"})
	if err != nil {
		t.Fatalf("parseProjectCreate: %v", err)
	}
	if proj.Workflow != "kanban" || proj.DBPath != "/tmp/b.db" {
		t.Fatalf("unexpected: %+v", proj)
	}
}

func TestParseProjectCreate_AutoCompleteAndUrgency(t *testing.T) {
	proj, err := parseProjectCreate([]string{
		"workflow=kanban",
		"auto-complete.trigger=completed",
		"auto-complete.target=completed",
		"urgency.blocking-weight=15",
	})
	if err != nil {
		t.Fatalf("parseProjectCreate: %v", err)
	}
	if proj.Settings.AutoCompleteParent == nil ||
		proj.Settings.AutoCompleteParent.TriggerStatus != "completed" {
		t.Fatalf("auto-complete: %+v", proj.Settings.AutoCompleteParent)
	}
	if proj.Settings.Urgency == nil ||
		proj.Settings.Urgency.BlockingWeight == nil ||
		*proj.Settings.Urgency.BlockingWeight != 15 {
		t.Fatalf("urgency: %+v", proj.Settings.Urgency)
	}
}

func TestParseProjectCreate_RejectsModifier(t *testing.T) {
	_, err := parseProjectCreate([]string{"+workflow=kanban"})
	if err == nil {
		t.Fatal("expected modifier rejection")
	}
}

func TestParseProjectCreate_UnknownField(t *testing.T) {
	_, err := parseProjectCreate([]string{"ghost=value"})
	if err == nil {
		t.Fatal("expected unknown-field error")
	}
}

func TestParseProjectModify_BareSet(t *testing.T) {
	mut, err := parseProjectModify([]string{"workflow=sprint", "urgency.blocking-weight=10"})
	if err != nil {
		t.Fatalf("parseProjectModify: %v", err)
	}
	if mut.Workflow == nil || *mut.Workflow != "sprint" {
		t.Fatalf("workflow: %+v", mut.Workflow)
	}
	if mut.UrgencySet["blocking_weight"] != 10 {
		t.Fatalf("urgency set: %+v", mut.UrgencySet)
	}
}

func TestParseProjectModify_Delta(t *testing.T) {
	mut, err := parseProjectModify([]string{"+urgency.blocking-weight=2", "-urgency.due-weight=1"})
	if err != nil {
		t.Fatalf("parseProjectModify: %v", err)
	}
	if mut.UrgencyDelta["blocking_weight"] != 2 {
		t.Fatalf("add delta: %+v", mut.UrgencyDelta)
	}
	if mut.UrgencyDelta["due_weight"] != -1 {
		t.Fatalf("sub delta: %+v", mut.UrgencyDelta)
	}
}

func TestParseProjectModify_DeltaOnNonUrgencyRejected(t *testing.T) {
	_, err := parseProjectModify([]string{"+workflow=sprint"})
	if err == nil {
		t.Fatal("expected rejection of modifier on workflow")
	}
}

func TestParseProjectModify_ClearDBPath(t *testing.T) {
	mut, err := parseProjectModify([]string{"db-path="})
	if err != nil {
		t.Fatalf("parseProjectModify: %v", err)
	}
	if mut.DBPath == nil || *mut.DBPath != "" {
		t.Fatalf("expected DBPath pointer to empty, got %+v", mut.DBPath)
	}
}
