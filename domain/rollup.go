package domain

// StatusCount is one entry in a Rollup's status breakdown.
// Buckets are keyed by status name (case-sensitive), so two workflows
// that both define a status named "active" merge into one bucket — the
// user-visible label is what matters.
type StatusCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Rollup is the aggregated descendant state of a task subtree.
// Tasks whose status carries the `delete` role in their own workflow are
// excluded entirely (they do not contribute to Total or StatusCounts).
type Rollup struct {
	Done         int           `json:"done"`          // count of descendants whose status carries the `done` role
	Total        int           `json:"total"`         // count of descendants whose status does NOT carry the `delete` role
	Percent      float64       `json:"percent"`       // Done/Total in [0.0, 1.0]; 0.0 when Total == 0
	StatusCounts []StatusCount `json:"status_counts"` // workflow order, zeros included, delete-role buckets excluded
}

// SummaryBlock pairs a block task with its descendant Rollup.
// Used by both the CLI and the MCP layer.
type SummaryBlock struct {
	Task   *Task  `json:"task"`
	Rollup Rollup `json:"rollup"`
}

// Summary is the top-level envelope returned by SummarizeBlocks-shaped
// responses. Mode is one of "single", "filter", or "roots". Totals is
// nil in "single" mode (it would just duplicate the single block's
// Rollup); always populated in "filter" and "roots" mode (the zero
// Rollup when Blocks is empty).
type Summary struct {
	Mode   string          `json:"mode"`
	Blocks []*SummaryBlock `json:"blocks"`
	Totals *Rollup         `json:"totals,omitempty"`
}

// AggregateRollup classifies each descendant against its own workflow
// and returns the aggregated Rollup. workflowFor returns the Workflow
// that governs a task — typically a project-keyed lookup. If
// workflowFor returns nil for a given task, that task is skipped (its
// status cannot be classified).
//
// Rules:
//   - A task whose status carries the `delete` role in its workflow is
//     excluded entirely (does not contribute to Total or StatusCounts).
//   - A task whose status carries the `done` role in its workflow
//     contributes to Done, Total, and its name bucket.
//   - All other non-delete tasks contribute to Total and their name
//     bucket.
//   - StatusCounts ordering: the workflow associated with the FIRST
//     non-delete-role descendant encountered seeds the breakdown order
//     (in that workflow's status ordering, but only including
//     statuses that lack the `delete` role). Statuses appearing later
//     from other workflows are appended in first-seen order. Two
//     workflows with the same status name share one bucket.
//   - Percent is float64(Done) / float64(Total) in the range [0.0, 1.0],
//     or 0.0 when Total == 0.
func AggregateRollup(descendants []*Task, workflowFor func(*Task) *Workflow) Rollup {
	counts := make(map[string]int)
	bucketOrder := []string{}
	bucketSeen := make(map[string]bool)

	addBucket := func(name string) {
		if bucketSeen[name] {
			return
		}
		bucketSeen[name] = true
		bucketOrder = append(bucketOrder, name)
	}

	seedFromWorkflow := func(wf *Workflow) {
		for _, name := range workflowStatusOrder(wf) {
			cfg := wf.Statuses[name]
			if cfg.HasRole(RoleDelete) {
				continue
			}
			addBucket(name)
		}
	}

	var done, total int
	seeded := false
	for _, t := range descendants {
		if t == nil {
			continue
		}
		wf := workflowFor(t)
		if wf == nil {
			continue
		}
		cfg, ok := wf.Statuses[t.Status]
		if ok && cfg.HasRole(RoleDelete) {
			continue
		}
		if !seeded {
			seedFromWorkflow(wf)
			seeded = true
		}
		total++
		if ok && cfg.HasRole(RoleDone) {
			done++
		}
		addBucket(t.Status)
		counts[t.Status]++
	}

	statusCounts := make([]StatusCount, 0, len(bucketOrder))
	for _, name := range bucketOrder {
		statusCounts = append(statusCounts, StatusCount{Name: name, Count: counts[name]})
	}

	var percent float64
	if total > 0 {
		percent = float64(done) / float64(total)
	}

	return Rollup{
		Done:         done,
		Total:        total,
		Percent:      percent,
		StatusCounts: statusCounts,
	}
}

// workflowStatusOrder returns the workflow's status names in a
// deterministic order suitable for breakdown rendering. The Workflow
// type stores statuses in a map (no inherent order), so the order is
// derived from the transitions slice — walking it in the order the
// workflow declares yields a sequence consistent with the workflow's
// intended progression (e.g. for kanban: pending → active → completed
// → deleted). Statuses that do not appear in any transition are
// appended last in the deterministic StatusNames() order.
func workflowStatusOrder(wf *Workflow) []string {
	if wf == nil {
		return nil
	}
	seen := make(map[string]bool, len(wf.Statuses))
	order := make([]string, 0, len(wf.Statuses))
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		if _, ok := wf.Statuses[name]; !ok {
			return
		}
		seen[name] = true
		order = append(order, name)
	}
	for _, t := range wf.Transitions {
		add(t.FromStatus)
		add(t.ToStatus)
	}
	for _, name := range wf.StatusNames() {
		add(name)
	}
	return order
}
