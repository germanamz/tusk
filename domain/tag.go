package domain

import "github.com/google/uuid"

type Tag struct {
	ID    uuid.UUID
	Name  string
	Color *string
}

// TagWithUsage pairs a Tag with the number of tasks it is assigned to.
type TagWithUsage struct {
	Tag       Tag
	TaskCount int
}
