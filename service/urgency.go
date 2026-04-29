package service

import (
	"math"
	"sort"
	"sync"
	"time"

	"github.com/germanamz/tusk/domain"
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
	ProjectWeights  map[uuid.UUID]*UrgencyWeights // per-project weight overrides (fully merged)
	// EffectiveWeights holds per-task fully-resolved weights (project + ancestor + self).
	// Populated only for tasks whose chain contributes at least one non-default value;
	// callers still fall through to ProjectWeights / defaults when a task ID is absent.
	EffectiveWeights map[uuid.UUID]*UrgencyWeights
}

const (
	// maxPriority is the highest priority value, used to normalize priority to [0, 1].
	maxPriority = 4.0

	// ageCeilingDays is the number of days after which age contribution is capped at 1.0.
	ageCeilingDays = 365.0

	// hoursPerDay converts hours to days for age and due-date calculations.
	hoursPerDay = 24.0

	// maxTagsForFullWeight is the tag count at which the tags factor reaches 1.0.
	maxTagsForFullWeight = 3.0

	// maxAnnotationsForFullWeight is the annotation count at which the annotations factor reaches 1.0.
	maxAnnotationsForFullWeight = 2.0

	// dueSigmoidSteepness controls how sharply the due-date coefficient transitions.
	dueSigmoidSteepness = 0.5

	// dueSigmoidMidpointDays is the inflection point (in days until due) of the sigmoid curve.
	dueSigmoidMidpointDays = 14.0
)

// UrgencyEngine computes urgency scores for tasks.
type UrgencyEngine struct {
	mu       sync.RWMutex
	defaults UrgencyWeights
}

// NewUrgencyEngine creates an engine with the given default weights.
func NewUrgencyEngine(defaults UrgencyWeights) *UrgencyEngine {
	return &UrgencyEngine{defaults: defaults}
}

// Reload atomically replaces the default weights used when a task's project
// has no per-project overrides. Safe for concurrent readers.
func (engine *UrgencyEngine) Reload(defaults UrgencyWeights) {
	engine.mu.Lock()
	engine.defaults = defaults
	engine.mu.Unlock()
}

// Score computes the urgency score for a single task.
func (engine *UrgencyEngine) Score(task *domain.Task, ctx ScoringContext) float64 {
	weights := engine.weightsFor(task, ctx)

	var score float64

	// Priority: normalized to [0, 1] then scaled by weight.
	if task.Priority > 0 {
		score += (float64(task.Priority) / maxPriority) * weights.Priority
	}

	// Due date: sigmoid curve maps proximity to [0, 1].
	if task.DueAt != nil {
		score += dueDateCoefficient(*task.DueAt) * weights.Due
	}

	// Age: days since creation capped at ageCeilingDays, scaled by weight.
	age := time.Since(task.CreatedAt).Hours() / hoursPerDay
	score += math.Min(age/ageCeilingDays, 1.0) * weights.Age

	// Active status
	if task.Status == "active" {
		score += weights.Active
	}

	// Blocking
	if ctx.BlockingCount[task.ID] > 0 {
		score += weights.Blocking
	}

	// Blocked
	if ctx.BlockedByCount[task.ID] > 0 {
		score += weights.Blocked
	}

	// Tags
	tagCount := ctx.TagCount[task.ID]
	if tagCount > 0 {
		score += math.Min(float64(tagCount)/maxTagsForFullWeight, 1.0) * weights.Tags
	}

	// Project
	if task.ProjectID != uuid.Nil {
		score += weights.Project
	}

	// Annotations
	annCount := ctx.AnnotationCount[task.ID]
	if annCount > 0 {
		score += math.Min(float64(annCount)/maxAnnotationsForFullWeight, 1.0) * weights.Annotations
	}

	// Waiting
	if task.WaitUntil != nil && task.WaitUntil.After(time.Now()) {
		score += weights.Waiting
	}

	return score
}

// ScoreAndSort computes urgency for all tasks and sorts them descending.
func (engine *UrgencyEngine) ScoreAndSort(tasks []*domain.Task, ctx ScoringContext) {
	for _, task := range tasks {
		task.Urgency = engine.Score(task, ctx)
	}
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].Urgency > tasks[j].Urgency
	})
}

// weightsFor returns the effective weights for a task. Resolution order:
// 1. ctx.EffectiveWeights[task.ID] (project + ancestor + self chain), 2.
// ctx.ProjectWeights[task.ProjectID], 3. engine defaults.
func (engine *UrgencyEngine) weightsFor(task *domain.Task, ctx ScoringContext) UrgencyWeights {
	if ctx.EffectiveWeights != nil {
		if resolved, ok := ctx.EffectiveWeights[task.ID]; ok {
			return *resolved
		}
	}
	if ctx.ProjectWeights != nil {
		if projectWeights, ok := ctx.ProjectWeights[task.ProjectID]; ok {
			return *projectWeights
		}
	}
	engine.mu.RLock()
	defer engine.mu.RUnlock()
	return engine.defaults
}

// Defaults returns a copy of the engine's default weights, using the
// internal RW mutex for safe concurrent access.
func (engine *UrgencyEngine) Defaults() UrgencyWeights {
	engine.mu.RLock()
	defer engine.mu.RUnlock()
	return engine.defaults
}

// WeightByKey returns the named weight's resolved value using the snake_case
// keys listed in domain.ValidUrgencyWeightKeys. Returns (0, false) for
// unknown keys.
func WeightByKey(weights UrgencyWeights, key string) (float64, bool) {
	switch key {
	case "priority_weight":
		return weights.Priority, true
	case "due_weight":
		return weights.Due, true
	case "age_weight":
		return weights.Age, true
	case "active_weight":
		return weights.Active, true
	case "blocking_weight":
		return weights.Blocking, true
	case "blocked_weight":
		return weights.Blocked, true
	case "tags_weight":
		return weights.Tags, true
	case "project_weight":
		return weights.Project, true
	case "annotations_weight":
		return weights.Annotations, true
	case "waiting_weight":
		return weights.Waiting, true
	}
	return 0, false
}

// Resolved converts an internal UrgencyWeights to the domain-level,
// JSON-friendly ResolvedUrgencyWeights shape used by renderers.
func (weights UrgencyWeights) Resolved() domain.ResolvedUrgencyWeights {
	return domain.ResolvedUrgencyWeights{
		PriorityWeight:    weights.Priority,
		DueWeight:         weights.Due,
		AgeWeight:         weights.Age,
		ActiveWeight:      weights.Active,
		BlockingWeight:    weights.Blocking,
		BlockedWeight:     weights.Blocked,
		TagsWeight:        weights.Tags,
		ProjectWeight:     weights.Project,
		AnnotationsWeight: weights.Annotations,
		WaitingWeight:     weights.Waiting,
	}
}

// MergeWeights returns a copy of defaults with any non-nil overrides applied.
func MergeWeights(defaults UrgencyWeights, overrides *domain.UrgencyOverrides) UrgencyWeights {
	if overrides == nil {
		return defaults
	}
	merged := defaults
	if overrides.PriorityWeight != nil {
		merged.Priority = *overrides.PriorityWeight
	}
	if overrides.DueWeight != nil {
		merged.Due = *overrides.DueWeight
	}
	if overrides.AgeWeight != nil {
		merged.Age = *overrides.AgeWeight
	}
	if overrides.ActiveWeight != nil {
		merged.Active = *overrides.ActiveWeight
	}
	if overrides.BlockingWeight != nil {
		merged.Blocking = *overrides.BlockingWeight
	}
	if overrides.BlockedWeight != nil {
		merged.Blocked = *overrides.BlockedWeight
	}
	if overrides.TagsWeight != nil {
		merged.Tags = *overrides.TagsWeight
	}
	if overrides.ProjectWeight != nil {
		merged.Project = *overrides.ProjectWeight
	}
	if overrides.AnnotationsWeight != nil {
		merged.Annotations = *overrides.AnnotationsWeight
	}
	if overrides.WaitingWeight != nil {
		merged.Waiting = *overrides.WaitingWeight
	}
	return merged
}

// dueDateCoefficient returns a value between 0 and 1 based on how close the due date is.
// Uses a sigmoid curve: coefficient = 1 / (1 + e^(-k * (midpoint - days_until_due)))
// k = 0.5 (steepness), midpoint = 14 (days, inflection point).
// Past-due tasks approach 1.0. Far-future tasks approach 0.0.
func dueDateCoefficient(dueAt time.Time) float64 {
	daysUntilDue := time.Until(dueAt).Hours() / hoursPerDay
	return 1.0 / (1.0 + math.Exp(-dueSigmoidSteepness*(dueSigmoidMidpointDays-daysUntilDue)))
}
