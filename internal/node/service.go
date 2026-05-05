package node

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/germanamz/tusk/internal/index"
)

// ErrAlreadyExists is returned by Create when the target file already exists.
var ErrAlreadyExists = errors.New("node: file already exists")

// CreateInput configures Service.Create.
type CreateInput struct {
	RelPath    string         // workspace-relative target path including extension (e.g. "tickets/foo.md")
	Type       string         // required type
	Title      string         // optional title; if empty, no title key is written
	Properties map[string]any // additional frontmatter properties (excluding type and title)
	Body       []byte         // markdown body
}

// ListFilter narrows Service.List. Plan 1b supports type only.
type ListFilter struct {
	Type string
}

// Service orchestrates filesystem and index for nodes.
type Service struct {
	root string
	repo *index.NodeRepo
}

// NewService constructs a Service for workspace at workspaceRoot.
func NewService(workspaceRoot string, repo *index.NodeRepo) *Service {
	return &Service{root: workspaceRoot, repo: repo}
}

// Create writes the node file and upserts the index row in one operation.
func (service *Service) Create(input CreateInput) (*Node, error) {
	absPath := filepath.Join(service.root, input.RelPath)

	if _, statErr := os.Stat(absPath); statErr == nil {
		return nil, ErrAlreadyExists
	}

	properties := map[string]any{"type": input.Type}

	if input.Title != "" {
		properties["title"] = input.Title
	}

	for key, value := range input.Properties {
		properties[key] = value
	}

	rendered, renderErr := renderMarkdown(properties, input.Body)

	if renderErr != nil {
		return nil, renderErr
	}

	if mkErr := os.MkdirAll(filepath.Dir(absPath), 0o755); mkErr != nil {
		return nil, fmt.Errorf("node: mkdir %s: %w", filepath.Dir(absPath), mkErr)
	}

	if writeErr := os.WriteFile(absPath, rendered, 0o644); writeErr != nil {
		return nil, fmt.Errorf("node: write %s: %w", absPath, writeErr)
	}

	stat, statErr := os.Stat(absPath)

	if statErr != nil {
		return nil, fmt.Errorf("node: stat %s: %w", absPath, statErr)
	}

	parsed, parseErr := ParseFile(input.RelPath, rendered)

	if parseErr != nil {
		return nil, parseErr
	}

	checksum := sha256Hex(rendered)
	propertiesJSON, marshalErr := json.Marshal(parsed.Properties)

	if marshalErr != nil {
		return nil, fmt.Errorf("node: marshal properties: %w", marshalErr)
	}

	if upsertErr := service.repo.Upsert(index.NodeRow{
		ID:             parsed.ID,
		Type:           parsed.Type,
		Path:           parsed.Path,
		Title:          parsed.Title,
		PropertiesJSON: string(propertiesJSON),
		LastMtime:      stat.ModTime().UnixNano(),
		LastSize:       stat.Size(),
		LastChecksum:   checksum,
	}); upsertErr != nil {
		return nil, upsertErr
	}

	return parsed, nil
}

// Get loads a node by id, reading the file from disk.
func (service *Service) Get(nodeID string) (*Node, error) {
	row, getErr := service.repo.Get(nodeID)

	if getErr != nil {
		return nil, getErr
	}

	content, readErr := os.ReadFile(filepath.Join(service.root, row.Path))

	if readErr != nil {
		return nil, fmt.Errorf("node: read %s: %w", row.Path, readErr)
	}

	return ParseFile(row.Path, content)
}

// List returns nodes from the index matching filter. Bodies are not loaded.
func (service *Service) List(filter ListFilter) ([]Node, error) {
	rows, listErr := service.repo.List(index.ListFilter{Type: filter.Type})

	if listErr != nil {
		return nil, listErr
	}

	results := make([]Node, 0, len(rows))

	for _, row := range rows {
		results = append(results, Node{
			ID:    row.ID,
			Path:  row.Path,
			Type:  row.Type,
			Title: row.Title,
		})
	}

	return results, nil
}

// renderMarkdown serializes properties as YAML frontmatter and concatenates body.
func renderMarkdown(properties map[string]any, body []byte) ([]byte, error) {
	var builder strings.Builder

	builder.WriteString("---\n")

	// Render `type` first, then `title`, then remaining keys in insertion order
	// for stable output. We rely on the small property set in v1; a sorted-by-key
	// pass is added if/when ordering becomes meaningful for diffs.
	if typeValue, hasType := properties["type"].(string); hasType {
		builder.WriteString("type: ")
		builder.WriteString(typeValue)
		builder.WriteString("\n")
	}

	if titleValue, hasTitle := properties["title"].(string); hasTitle && titleValue != "" {
		builder.WriteString("title: ")
		builder.WriteString(titleValue)
		builder.WriteString("\n")
	}

	for key, value := range properties {
		if key == "type" || key == "title" {
			continue
		}

		switch typed := value.(type) {
		case string:
			builder.WriteString(key)
			builder.WriteString(": ")
			builder.WriteString(typed)
			builder.WriteString("\n")
		case int:
			builder.WriteString(key)
			builder.WriteString(": ")
			fmt.Fprintf(&builder, "%d\n", typed)
		case bool:
			builder.WriteString(key)
			builder.WriteString(": ")
			fmt.Fprintf(&builder, "%t\n", typed)
		default:
			return nil, fmt.Errorf("node: unsupported frontmatter type for %s: %T (Plan 1b supports string/int/bool only)", key, value)
		}
	}

	builder.WriteString("---\n\n")
	builder.Write(body)

	if !strings.HasSuffix(string(body), "\n") {
		builder.WriteString("\n")
	}

	return []byte(builder.String()), nil
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)

	return hex.EncodeToString(sum[:])
}
