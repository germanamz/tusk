package tui

import "strings"

// ParsedArgs holds the result of parsing CLI arguments.
type ParsedArgs struct {
	Title    string            // non-key:value, non-tag args joined with spaces
	Fields   map[string]string // key:value pairs
	Tags     []string          // +tag inclusions
	ExclTags []string          // -tag exclusions
}

// parseArgs classifies each arg in the slice into title words, key:value fields,
// +tags, or -tags.
//
// Rules:
//   - "key:value" -> Fields["key"] = "value" (first colon splits; value may contain colons)
//   - "+word"     -> Tags = append(Tags, "word")
//   - "-word"     -> ExclTags = append(ExclTags, "word")
//   - everything else is joined with spaces as Title
func parseArgs(args []string) ParsedArgs {
	p := ParsedArgs{Fields: make(map[string]string)}
	var titleParts []string

	for _, arg := range args {
		switch {
		case strings.Contains(arg, ":") && !strings.HasPrefix(arg, "+") && !strings.HasPrefix(arg, "-"):
			key, value, _ := strings.Cut(arg, ":")
			p.Fields[key] = value
		case strings.HasPrefix(arg, "+") && len(arg) > 1:
			p.Tags = append(p.Tags, arg[1:])
		case strings.HasPrefix(arg, "-") && len(arg) > 1:
			p.ExclTags = append(p.ExclTags, arg[1:])
		default:
			titleParts = append(titleParts, arg)
		}
	}

	p.Title = strings.Join(titleParts, " ")
	return p
}
