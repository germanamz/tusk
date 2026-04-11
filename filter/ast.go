package filter

import "github.com/germanamz/tusk/syntax"

// Re-export shared AST types so existing consumers compile unchanged.
type FilterSet = syntax.FilterSet
type FieldFilter = syntax.FieldFilter
type TagFilter = syntax.TagFilter
