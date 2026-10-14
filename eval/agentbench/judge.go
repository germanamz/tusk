package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

const judgeSystemPrompt = `You grade answers to questions about software documentation.

You get a question, the reference section or sections of the documentation that answer it, and a candidate answer. The answer is correct when it gives the information the reference gives for the question and contradicts nothing in the reference. Extra detail the reference doesn't mention is fine; don't mark an answer wrong only because it says more. An answer that says the documentation doesn't cover the question, or that misses the point the reference makes, is incorrect.

Judge only against the reference, not against your own knowledge. Give a one-sentence reason.`

const verdictSchema = `{"type":"object","properties":{"correct":{"type":"boolean"},"reason":{"type":"string"}},"required":["correct","reason"]}`

// referenceCap bounds the reference text handed to the judge. Target
// sections are a few hundred to a few thousand bytes; the cap only guards
// against a target heading that turns out to cover most of a file.
const referenceCap = 24 << 10

// Verdict is the judge's grade of one answer.
type Verdict struct {
	Correct bool   `json:"correct"`
	Reason  string `json:"reason"`
}

// Judge grades answers with a small model and no tools.
type Judge struct {
	Claude    string
	Model     string
	MaxBudget float64
	Timeout   time.Duration
}

// Grade asks the judge whether answer answers task, given the text of the
// task's target sections. It returns the verdict and what the call cost.
func (judge *Judge) Grade(ctx context.Context, task Task, corpus *Corpus, answer string) (Verdict, float64, error) {
	var verdict Verdict

	emptyDir, tempErr := os.MkdirTemp("", "agentbench-judge-")

	if tempErr != nil {
		return verdict, 0, tempErr
	}

	defer func() { _ = os.RemoveAll(emptyDir) }()

	args := []string{
		"--model", judge.Model,
		"--system-prompt", judgeSystemPrompt,
		"--json-schema", verdictSchema,
		"--tools", "",
	}

	prompt := judgePrompt(task, corpus, answer)

	summary, claudeErr := runClaude(ctx, judge.Claude, emptyDir, prompt, args, judge.MaxBudget, judge.Timeout, "")

	cost := 0.0

	if summary.Result != nil {
		cost = summary.Result.TotalCostUSD
	}

	if claudeErr != nil {
		return verdict, cost, claudeErr
	}

	if decodeErr := json.Unmarshal(summary.Result.StructuredOutput, &verdict); decodeErr != nil {
		return verdict, cost, fmt.Errorf("decoding verdict: %w", decodeErr)
	}

	return verdict, cost, nil
}

func judgePrompt(task Task, corpus *Corpus, answer string) string {
	var reference strings.Builder

	for _, target := range task.Targets {
		for _, section := range corpus.Sections(target.File, target.Heading) {
			fmt.Fprintf(&reference, "From %s:\n\n%s\n\n", target.File, corpus.Text(target.File, section))
		}
	}

	text := reference.String()

	if len(text) > referenceCap {
		text = text[:referenceCap] + "\n[reference truncated]"
	}

	return fmt.Sprintf("Question:\n%s\n\nReference:\n%s\nCandidate answer:\n%s\n", task.Question, text, answer)
}
