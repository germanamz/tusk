package service

import (
	"context"
	"fmt"
	"sort"

	"github.com/germanamz/tusk/domain"
)

// applyUrgencyOverridesUpdate mutates task.UrgencyOverrides according to the
// urgency-related fields on upd. Order of operations: full-replace OR
// (merge-patch then delta). Mutual exclusion between full-replace and the
// other two is enforced earlier in TaskService.Update.
func (s *TaskService) applyUrgencyOverridesUpdate(
	ctx context.Context,
	bundle *RepoBundle,
	task *domain.Task,
	upd domain.TaskUpdate,
) error {
	if upd.UrgencyOverrides != nil {
		task.UrgencyOverrides = *upd.UrgencyOverrides
		return nil
	}

	if upd.UrgencyMergePatch != nil {
		if err := applyUrgencyMergePatch(task, upd.UrgencyMergePatch); err != nil {
			return err
		}
		normalizeUrgencyOverrides(task)
	}

	if len(upd.UrgencyDelta) > 0 {
		if err := s.applyUrgencyDelta(ctx, bundle, task, upd.UrgencyDelta); err != nil {
			return err
		}
		normalizeUrgencyOverrides(task)
	}

	return nil
}

// applyUrgencyMergePatch applies an RFC 7396-style merge patch to
// task.UrgencyOverrides. ClearAll runs first, then Clear, then Set.
func applyUrgencyMergePatch(task *domain.Task, patch *domain.UrgencyOverridesPatch) error {
	if patch.ClearAll {
		task.UrgencyOverrides = nil
	}
	if task.UrgencyOverrides == nil && (len(patch.Clear) > 0 || len(patch.Set) > 0) {
		task.UrgencyOverrides = &domain.UrgencyOverrides{}
	}
	for key := range patch.Clear {
		fp := domain.UrgencyOverrideFieldPtr(task.UrgencyOverrides, key)
		if fp == nil {
			return fmt.Errorf("urgency_overrides patch: unknown key %q", key)
		}
		*fp = nil
	}
	for key, value := range patch.Set {
		fp := domain.UrgencyOverrideFieldPtr(task.UrgencyOverrides, key)
		if fp == nil {
			return fmt.Errorf("urgency_overrides patch: unknown key %q", key)
		}
		v := value
		*fp = &v
	}
	return nil
}

// applyUrgencyDelta adds signed deltas to the resolved baseline weight for
// each named key, writing the result back to task.UrgencyOverrides. The
// baseline is computed from the post-patch in-memory task state, so a delta
// applied after a merge-patch sees the patched value.
func (s *TaskService) applyUrgencyDelta(
	ctx context.Context,
	bundle *RepoBundle,
	task *domain.Task,
	delta map[string]float64,
) error {
	for key := range delta {
		if domain.UrgencyOverrideFieldPtr(&domain.UrgencyOverrides{}, key) == nil {
			return fmt.Errorf("urgency_overrides delta: unknown key %q", key)
		}
	}

	baseline, err := s.resolveEffectiveWeightsFromTask(ctx, bundle, task)
	if err != nil {
		return fmt.Errorf("resolving baseline for urgency delta: %w", err)
	}

	keys := make([]string, 0, len(delta))
	for k := range delta {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	if task.UrgencyOverrides == nil {
		task.UrgencyOverrides = &domain.UrgencyOverrides{}
	}
	for _, key := range keys {
		base, ok := WeightByKey(baseline, key)
		if !ok {
			return fmt.Errorf("urgency_overrides delta: unknown key %q", key)
		}
		newValue := base + delta[key]
		fp := domain.UrgencyOverrideFieldPtr(task.UrgencyOverrides, key)
		v := newValue
		*fp = &v
	}
	return nil
}

// normalizeUrgencyOverrides drops a fully-empty *UrgencyOverrides struct so
// the persisted column is NULL rather than `{}`. Callers that have just
// cleared every key rely on this to keep the storage invariant: nil means
// "nothing here", non-nil means "at least one field is set".
func normalizeUrgencyOverrides(task *domain.Task) {
	if task.UrgencyOverrides == nil {
		return
	}
	o := task.UrgencyOverrides
	if o.PriorityWeight == nil && o.DueWeight == nil && o.AgeWeight == nil &&
		o.ActiveWeight == nil && o.BlockingWeight == nil && o.BlockedWeight == nil &&
		o.TagsWeight == nil && o.ProjectWeight == nil && o.AnnotationsWeight == nil &&
		o.WaitingWeight == nil {
		task.UrgencyOverrides = nil
	}
}
