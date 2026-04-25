package domain

// ResolvedUrgencyWeights is the fully-resolved 10-weight table for a task.
// Field names mirror UrgencyOverrides (and its JSON tags) to make rendering
// trivial. Populated from service.UrgencyWeights via the Resolved() adapter.
type ResolvedUrgencyWeights struct {
	PriorityWeight    float64
	DueWeight         float64
	AgeWeight         float64
	ActiveWeight      float64
	BlockingWeight    float64
	BlockedWeight     float64
	TagsWeight        float64
	ProjectWeight     float64
	AnnotationsWeight float64
	WaitingWeight     float64
}
