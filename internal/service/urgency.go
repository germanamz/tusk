package service

import (
	"math"
	"sort"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/google/uuid"
)

// UrgencyWeights holds the weight for each scoring factor.
type UrgencyWeights struct {
	Priority    float64
	Due         float64
	Age         float64
	Active      float64
	Blocking    float64
	Blocked     float64
	Tags        float64
	Project     float64
	Annotations float64
	Waiting     float64
}

// ScoringContext holds preloaded batch data needed for urgency scoring.
// All maps use task ID as key. Tasks missing from a map are treated as zero.
type ScoringContext struct {
	BlockingCount   map[uuid.UUID]int
	BlockedByCount  map[uuid.UUID]int
	AnnotationCount map[uuid.UUID]int
	TagCount        map[uuid.UUID]int
	ProjectWeights  map[string]*UrgencyWeights // per-project weight overrides (fully merged)
}

// UrgencyEngine computes urgency scores for tasks.
type UrgencyEngine struct {
	defaults UrgencyWeights
}

// NewUrgencyEngine creates an engine with the given default weights.
func NewUrgencyEngine(defaults UrgencyWeights) *UrgencyEngine {
	return &UrgencyEngine{defaults: defaults}
}

// Score computes the urgency score for a single task.
func (e *UrgencyEngine) Score(task *domain.Task, ctx ScoringContext) float64 {
	w := e.weightsFor(task.ProjectID, ctx)

	var score float64

	// Priority: priority / 4.0 * weight
	if task.Priority > 0 {
		score += (float64(task.Priority) / 4.0) * w.Priority
	}

	// Due date: sigmoid curve
	if task.DueAt != nil {
		score += dueDateCoefficient(*task.DueAt) * w.Due
	}

	// Age: min(days / 365, 1.0) * weight
	age := time.Since(task.CreatedAt).Hours() / 24.0
	score += math.Min(age/365.0, 1.0) * w.Age

	// Active status
	if task.Status == "active" {
		score += w.Active
	}

	// Blocking
	if ctx.BlockingCount[task.ID] > 0 {
		score += w.Blocking
	}

	// Blocked
	if ctx.BlockedByCount[task.ID] > 0 {
		score += w.Blocked
	}

	// Tags
	tagCount := ctx.TagCount[task.ID]
	if tagCount > 0 {
		score += math.Min(float64(tagCount)/3.0, 1.0) * w.Tags
	}

	// Project
	if task.ProjectID != "" {
		score += w.Project
	}

	// Annotations
	annCount := ctx.AnnotationCount[task.ID]
	if annCount > 0 {
		score += math.Min(float64(annCount)/2.0, 1.0) * w.Annotations
	}

	// Waiting
	if task.WaitUntil != nil && task.WaitUntil.After(time.Now()) {
		score += w.Waiting
	}

	return score
}

// ScoreAndSort computes urgency for all tasks and sorts them descending.
func (e *UrgencyEngine) ScoreAndSort(tasks []*domain.Task, ctx ScoringContext) {
	for _, t := range tasks {
		t.Urgency = e.Score(t, ctx)
	}
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].Urgency > tasks[j].Urgency
	})
}

// weightsFor returns the effective weights for a task's project.
// If per-project overrides exist in the context, those are used; otherwise defaults.
func (e *UrgencyEngine) weightsFor(projectID string, ctx ScoringContext) UrgencyWeights {
	if pw, ok := ctx.ProjectWeights[projectID]; ok {
		return *pw
	}
	return e.defaults
}

// dueDateCoefficient returns a value between 0 and 1 based on how close the due date is.
// Uses a sigmoid curve: coefficient = 1 / (1 + e^(-k * (midpoint - days_until_due)))
// k = 0.5 (steepness), midpoint = 14 (days, inflection point).
// Past-due tasks approach 1.0. Far-future tasks approach 0.0.
func dueDateCoefficient(dueAt time.Time) float64 {
	daysUntilDue := time.Until(dueAt).Hours() / 24.0
	const k = 0.5
	const midpoint = 14.0
	return 1.0 / (1.0 + math.Exp(-k*(midpoint-daysUntilDue)))
}
