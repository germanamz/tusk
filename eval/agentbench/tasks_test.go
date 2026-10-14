package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadTasks(test *testing.T) {
	path := filepath.Join(test.TempDir(), "tasks.jsonl")
	content := `{"id":"one","question":"Q1?","kind":"lexical","targets":[{"file":"docs/embed.md","heading":"Notes"}]}

{"id":"two","question":"Q2?","kind":"paraphrase","targets":[{"file":"docs/embed.md","heading":"Public surface"}]}
`

	if writeErr := os.WriteFile(path, []byte(content), 0o644); writeErr != nil {
		test.Fatal(writeErr)
	}

	tasks, loadErr := LoadTasks(path)

	if loadErr != nil {
		test.Fatal(loadErr)
	}

	if len(tasks) != 2 || tasks[1].Targets[0].Heading != "Public surface" {
		test.Errorf("tasks = %+v", tasks)
	}

	corpus, _ := writeCorpus(test, map[string]string{"docs/embed.md": sampleDoc})

	if validateErr := ValidateTasks(tasks, corpus); validateErr != nil {
		test.Errorf("valid tasks rejected: %v", validateErr)
	}
}

func TestLoadTasksRejectsUnknownFields(test *testing.T) {
	path := filepath.Join(test.TempDir(), "tasks.jsonl")

	if writeErr := os.WriteFile(path, []byte(`{"id":"one","question":"Q?","kind":"lexical","target":[]}`), 0o644); writeErr != nil {
		test.Fatal(writeErr)
	}

	if _, loadErr := LoadTasks(path); loadErr == nil {
		test.Error("a misspelled field was accepted")
	}
}

func TestValidateTasksReportsEveryProblem(test *testing.T) {
	corpus, _ := writeCorpus(test, map[string]string{"docs/embed.md": sampleDoc})

	tasks := []Task{
		{ID: "dup", Question: "Q?", Kind: KindLexical, Targets: []Target{{File: "docs/embed.md", Heading: "Notes"}}},
		{ID: "dup", Question: "Q?", Kind: KindLexical, Targets: []Target{{File: "docs/embed.md", Heading: "Notes"}}},
		{ID: "kind", Question: "Q?", Kind: "semantic", Targets: []Target{{File: "docs/embed.md", Heading: "Notes"}}},
		{ID: "file", Question: "Q?", Kind: KindParaphrase, Targets: []Target{{File: "docs/missing.md", Heading: "Notes"}}},
		{ID: "heading", Question: "Q?", Kind: KindParaphrase, Targets: []Target{{File: "docs/embed.md", Heading: "Nope"}}},
		{ID: "targets", Question: "Q?", Kind: KindParaphrase},
	}

	validateErr := ValidateTasks(tasks, corpus)

	if validateErr == nil {
		test.Fatal("expected problems")
	}

	for _, fragment := range []string{"dup: duplicate id", `kind: kind "semantic"`, "docs/missing.md is not in the corpus", `has no heading "Nope"`, "targets: no targets"} {
		if !strings.Contains(validateErr.Error(), fragment) {
			test.Errorf("missing %q in:\n%v", fragment, validateErr)
		}
	}
}
