package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Question kinds. A lexical question shares distinctive words with the
// section that answers it, which favors grep; a paraphrase question asks
// in other words, which favors semantic search. The report breaks results
// down by kind so neither arm's home ground hides the other's.
const (
	KindLexical    = "lexical"
	KindParaphrase = "paraphrase"
)

// Target is a section that answers a task: a corpus file and one of its
// headings.
type Target struct {
	File    string `json:"file"`
	Heading string `json:"heading"`
}

// Task is one benchmark question with the sections that answer it.
type Task struct {
	ID       string   `json:"id"`
	Question string   `json:"question"`
	Kind     string   `json:"kind"`
	Targets  []Target `json:"targets"`
}

// LoadTasks reads a JSONL task file, one task per line. Blank lines are
// skipped.
func LoadTasks(path string) ([]Task, error) {
	file, openErr := os.Open(path)

	if openErr != nil {
		return nil, fmt.Errorf("opening tasks: %w", openErr)
	}

	defer file.Close()

	var tasks []Task

	scanner := bufio.NewScanner(file)
	lineNumber := 0

	for scanner.Scan() {
		lineNumber++

		line := strings.TrimSpace(scanner.Text())

		if line == "" {
			continue
		}

		var task Task

		decoder := json.NewDecoder(strings.NewReader(line))
		decoder.DisallowUnknownFields()

		if decodeErr := decoder.Decode(&task); decodeErr != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, lineNumber, decodeErr)
		}

		tasks = append(tasks, task)
	}

	if scanErr := scanner.Err(); scanErr != nil {
		return nil, fmt.Errorf("reading tasks: %w", scanErr)
	}

	return tasks, nil
}

// ValidateTasks checks every task against the corpus: ids are unique, the
// kind is known, and each target names a corpus file and a heading that
// file has. A target that doesn't resolve would make every citation a miss,
// so the run refuses to start rather than report a misleading score.
func ValidateTasks(tasks []Task, corpus *Corpus) error {
	if len(tasks) == 0 {
		return errors.New("the task file has no tasks")
	}

	var problems []string

	seen := make(map[string]bool)

	for _, task := range tasks {
		if task.ID == "" {
			problems = append(problems, fmt.Sprintf("task %q has no id", task.Question))

			continue
		}

		if seen[task.ID] {
			problems = append(problems, fmt.Sprintf("%s: duplicate id", task.ID))
		}

		seen[task.ID] = true

		if strings.TrimSpace(task.Question) == "" {
			problems = append(problems, fmt.Sprintf("%s: empty question", task.ID))
		}

		if task.Kind != KindLexical && task.Kind != KindParaphrase {
			problems = append(problems, fmt.Sprintf("%s: kind %q is not %q or %q", task.ID, task.Kind, KindLexical, KindParaphrase))
		}

		if len(task.Targets) == 0 {
			problems = append(problems, fmt.Sprintf("%s: no targets", task.ID))
		}

		for _, target := range task.Targets {
			switch {
			case !corpus.Has(target.File):
				problems = append(problems, fmt.Sprintf("%s: target file %s is not in the corpus", task.ID, target.File))
			case len(corpus.Sections(target.File, target.Heading)) == 0:
				problems = append(problems, fmt.Sprintf("%s: %s has no heading %q", task.ID, target.File, target.Heading))
			}
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("invalid tasks:\n  %s", strings.Join(problems, "\n  "))
	}

	return nil
}
