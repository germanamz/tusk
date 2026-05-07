package node

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
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

// ModifyInput configures Service.Modify.
type ModifyInput struct {
	ID        string         // required; node id (path without extension)
	SetProps  map[string]any // properties to upsert (excluding "type"; modify rejects type changes)
	UnsetKeys []string       // top-level frontmatter keys to remove
	Body      *[]byte        // when non-nil, replaces the body; nil leaves body untouched
}

// ListFilter narrows Service.List. Plan 1b supports type only.
type ListFilter struct {
	Type string
}

// Service orchestrates filesystem and index for nodes.
type Service struct {
	root       string
	repo       *index.NodeRepo
	edges      *index.EdgeRepo
	edgeTypes  manifest.EdgeTypes
	embedQueue *index.EmbedQueueRepo

	nodeTypes     map[string]manifest.NodeType // optional; nil = untyped pass-through
	propertyDrift *index.PropertyDriftRepo     // optional; nil = no property drift persistence

	behaviors Behaviors                // optional; nil = no hook dispatch
	drift     *index.WorkflowDriftRepo // optional; nil = no drift persistence
	warnings  io.Writer                // optional; nil = io.Discard
}

// NewService constructs a Service for a workspace whose manifest has no edge
// types declared (Plan 1b behavior). Edge writes via this service are no-ops.
func NewService(workspaceRoot string, repo *index.NodeRepo) *Service {
	return &Service{
		root:       workspaceRoot,
		repo:       repo,
		edges:      nil,
		edgeTypes:  manifest.EdgeTypes{},
		embedQueue: nil,
	}
}

// NewServiceWithManifest constructs a Service that writes edges through edges
// and validates them against edgeTypes.
func NewServiceWithManifest(workspaceRoot string, repo *index.NodeRepo, edges *index.EdgeRepo, edgeTypes manifest.EdgeTypes) *Service {
	return &Service{
		root:       workspaceRoot,
		repo:       repo,
		edges:      edges,
		edgeTypes:  edgeTypes,
		embedQueue: nil,
	}
}

// NewServiceWithEmbedQueue is like NewServiceWithManifest but also enqueues
// embed jobs on Create. When embedQueue is nil, behavior matches
// NewServiceWithManifest.
func NewServiceWithEmbedQueue(workspaceRoot string, repo *index.NodeRepo, edges *index.EdgeRepo, edgeTypes manifest.EdgeTypes, embedQueue *index.EmbedQueueRepo) *Service {
	return &Service{
		root:       workspaceRoot,
		repo:       repo,
		edges:      edges,
		edgeTypes:  edgeTypes,
		embedQueue: embedQueue,
	}
}

// NewServiceWithBehaviors is the Plan 7 production constructor: like
// NewServiceWithEmbedQueue, but also wires the behavior engine, the
// drift log, and a warnings writer (defaults to io.Discard when nil).
// Plan 7.b adds nodeTypes and propertyDrift; pass nil for both until
// Tasks 14–17 wire the real values through.
func NewServiceWithBehaviors(
	workspaceRoot string,
	repo *index.NodeRepo,
	edges *index.EdgeRepo,
	edgeTypes manifest.EdgeTypes,
	embedQueue *index.EmbedQueueRepo,
	nodeTypes map[string]manifest.NodeType,
	propertyDrift *index.PropertyDriftRepo,
	behaviors Behaviors,
	drift *index.WorkflowDriftRepo,
	warnings io.Writer,
) *Service {
	if warnings == nil {
		warnings = io.Discard
	}

	return &Service{
		root:          workspaceRoot,
		repo:          repo,
		edges:         edges,
		edgeTypes:     edgeTypes,
		embedQueue:    embedQueue,
		nodeTypes:     nodeTypes,
		propertyDrift: propertyDrift,
		behaviors:     behaviors,
		drift:         drift,
		warnings:      warnings,
	}
}

// Create writes the node file and upserts the index row in one operation.
// When the service has an EdgeRepo configured, edges are also persisted.
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

	parsed, parseErr := ParseFile(input.RelPath, rendered)

	if parseErr != nil {
		return nil, parseErr
	}

	if resolveErr := ResolveEdges(parsed, service.edgeTypes); resolveErr != nil {
		return nil, resolveErr
	}

	if _, hasReferences := service.edgeTypes["references"]; hasReferences {
		for _, target := range ExtractWikilinks(parsed.Body) {
			parsed.Edges["references"] = appendUnique(parsed.Edges["references"], target)
		}
	}

	if validateErr := ValidateEdges(parsed, service.edgeTypes, EdgeContext{
		ResolveTargetType: service.resolveTargetType,
	}); validateErr != nil {
		return nil, validateErr
	}

	if cycleErr := service.detectCyclesForAcyclicEdges(parsed); cycleErr != nil {
		return nil, cycleErr
	}

	// Plan 7.b: property validation — runs before hook validate-phase.
	propResult := ValidateProperties(parsed, service.nodeTypes)

	if len(propResult.HardErrors) > 0 {
		return nil, &PropertyValidationError{
			Op:       "create",
			NodeID:   parsed.ID,
			NodeType: parsed.Type,
			Errors:   propResult.HardErrors,
		}
	}

	// Plan 7: validate-phase hook dispatch (NodeWrite then EdgeAdd per row).
	if service.behaviors != nil {
		if rejector, fireErr := service.behaviors.FireNodeWriteValidate(nil, parsed); fireErr != nil {
			return nil, fmt.Errorf("behavior %s rejected create: %w", rejector, fireErr)
		}

		for _, edgeRow := range flattenEdges(parsed) {
			if rejector, fireErr := service.behaviors.FireEdgeAddValidate(edgeRow); fireErr != nil {
				return nil, fmt.Errorf("behavior %s rejected edge add: %w", rejector, fireErr)
			}
		}
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

	edgeRows := flattenEdges(parsed)

	if service.edges != nil {
		if upsertErr := service.edges.UpsertAll(parsed.ID, parsed.Path, edgeRows); upsertErr != nil {
			return nil, upsertErr
		}
	}

	if service.embedQueue != nil {
		if enqueueErr := service.embedQueue.Enqueue(parsed.ID); enqueueErr != nil {
			return nil, enqueueErr
		}
	}

	// Plan 7: after-phase hook dispatch. Errors aggregated for telemetry;
	// do not affect control flow.
	if service.behaviors != nil {
		_ = service.behaviors.FireNodeWriteAfter(nil, parsed)

		for _, edgeRow := range edgeRows {
			_ = service.behaviors.FireEdgeAddAfter(edgeRow)
		}
	}

	// Plan 7.b: property drift surface — fires after index commits.
	if len(propResult.Drift) > 0 {
		now := time.Now().UnixNano()

		for _, drift := range propResult.Drift {
			_, _ = fmt.Fprintf(service.warnings,
				"warning: node-types: property %q is not declared on type %q; surfaces as a property-drift in tusk doctor\n",
				drift.Property, parsed.Type)

			if service.propertyDrift != nil {
				_ = service.propertyDrift.Append(index.PropertyDriftRow{
					Kind:       "undeclared-property",
					NodeID:     parsed.ID,
					NodeType:   parsed.Type,
					Property:   drift.Property,
					Details:    drift.Reason,
					ObservedAt: now,
				})
			}
		}
	} else if service.propertyDrift != nil {
		// Clean pass: no hard errors, no drift — clear any prior drift for this node.
		_ = service.propertyDrift.ClearForNode(parsed.ID)
	}

	return parsed, nil
}

// Modify reads a node from disk, applies SetProps/UnsetKeys/Body, validates
// against the manifest, atomically rewrites the file, and updates index rows.
// Modify enqueues the node for re-embedding when the service has an EmbedQueue.
func (service *Service) Modify(input ModifyInput) (*Node, error) {
	row, getErr := service.repo.Get(input.ID)

	if getErr != nil {
		return nil, getErr
	}

	absPath := filepath.Join(service.root, row.Path)

	original, readErr := os.ReadFile(absPath)

	if readErr != nil {
		return nil, fmt.Errorf("node: read %s: %w", row.Path, readErr)
	}

	beforeNode, parseBeforeErr := ParseFile(row.Path, original)

	if parseBeforeErr != nil {
		return nil, parseBeforeErr
	}

	// Resolve edges on the before-node so the diff against the after-node
	// is well-defined.
	if resolveErr := ResolveEdges(beforeNode, service.edgeTypes); resolveErr != nil {
		return nil, resolveErr
	}

	// Apply Set/Unset/Body to produce after-node.
	parsed := beforeNode.Clone()

	for _, key := range input.UnsetKeys {
		if key == "type" {
			return nil, fmt.Errorf("node: cannot unset reserved key %q", key)
		}

		delete(parsed.Properties, key)
	}

	for key, value := range input.SetProps {
		if key == "type" && value != parsed.Type {
			return nil, fmt.Errorf("node: cannot change type via Modify (current=%q, requested=%v)", parsed.Type, value)
		}

		parsed.Properties[key] = value
	}

	body := parsed.Body

	if input.Body != nil {
		body = *input.Body
		parsed.Body = body
	}

	rendered, renderErr := renderMarkdown(parsed.Properties, body)

	if renderErr != nil {
		return nil, renderErr
	}

	reparsed, reparseErr := ParseFile(row.Path, rendered)

	if reparseErr != nil {
		return nil, reparseErr
	}

	if resolveErr := ResolveEdges(reparsed, service.edgeTypes); resolveErr != nil {
		return nil, resolveErr
	}

	if validateErr := ValidateEdges(reparsed, service.edgeTypes, EdgeContext{
		ResolveTargetType: service.resolveTargetType,
	}); validateErr != nil {
		return nil, validateErr
	}

	if cycleErr := service.detectCyclesForAcyclicEdges(reparsed); cycleErr != nil {
		return nil, cycleErr
	}

	// Plan 7.b: property validation + required-unset check — runs before hook validate-phase.
	modPropResult := ValidateProperties(reparsed, service.nodeTypes)

	// In the Modify path, ErrRequiredMissing from the validator is suppressed:
	// properties that were never set on a pre-existing node are not blocked by
	// Modify (the node predates the declaration). ErrCannotUnsetRequired (below)
	// handles the case where a required property is explicitly removed.
	var modHardErrors []PropertyError

	for _, pe := range modPropResult.HardErrors {
		if pe.Kind != ErrRequiredMissing {
			modHardErrors = append(modHardErrors, pe)
		}
	}

	modPropResult.HardErrors = modHardErrors

	// Detect explicit unset of required properties. We check input.UnsetKeys
	// directly (not WhichRequiredWereUnset) so that unsetting a required key
	// that was never present is also caught.
	if nt, declared := service.nodeTypes[reparsed.Type]; declared {
		declByNameForUnset := make(map[string]manifest.PropertyDecl, len(nt.Properties))

		for _, decl := range nt.Properties {
			declByNameForUnset[decl.Name] = decl
		}

		for _, unsetKey := range input.UnsetKeys {
			if decl, found := declByNameForUnset[unsetKey]; found && decl.Required {
				modPropResult.HardErrors = append(modPropResult.HardErrors, PropertyError{
					Kind:     ErrCannotUnsetRequired,
					Property: unsetKey,
					Reason:   fmt.Sprintf("cannot unset required property %q on type %q", unsetKey, reparsed.Type),
				})
			}
		}
	}

	if len(modPropResult.HardErrors) > 0 {
		return nil, &PropertyValidationError{
			Op:       "modify",
			NodeID:   reparsed.ID,
			NodeType: reparsed.Type,
			Errors:   modPropResult.HardErrors,
		}
	}

	// Plan 7: recovery-aware validate phase + edge diff hooks.
	var fireResult FireResult

	if service.behaviors != nil {
		result, fireErr := service.behaviors.FireNodeWriteValidateWithRecovery(beforeNode, reparsed)

		if fireErr != nil {
			return nil, fmt.Errorf("behavior %s rejected modify: %w", result.Rejector, fireErr)
		}

		fireResult = result

		removed, added := diffEdgeSets(beforeNode, reparsed)

		for _, edgeRow := range removed {
			if rejector, edgeFireErr := service.behaviors.FireEdgeRemoveValidate(edgeRow); edgeFireErr != nil {
				return nil, fmt.Errorf("behavior %s rejected edge remove: %w", rejector, edgeFireErr)
			}
		}

		for _, edgeRow := range added {
			if rejector, edgeFireErr := service.behaviors.FireEdgeAddValidate(edgeRow); edgeFireErr != nil {
				return nil, fmt.Errorf("behavior %s rejected edge add: %w", rejector, edgeFireErr)
			}
		}
	}

	if writeErr := atomicWrite(absPath, rendered); writeErr != nil {
		return nil, fmt.Errorf("node: write %s: %w", absPath, writeErr)
	}

	stat, statErr := os.Stat(absPath)

	if statErr != nil {
		return nil, fmt.Errorf("node: stat %s: %w", absPath, statErr)
	}

	checksum := sha256Hex(rendered)
	propertiesJSON, marshalErr := json.Marshal(reparsed.Properties)

	if marshalErr != nil {
		return nil, fmt.Errorf("node: marshal properties: %w", marshalErr)
	}

	if upsertErr := service.repo.Upsert(index.NodeRow{
		ID:             reparsed.ID,
		Type:           reparsed.Type,
		Path:           reparsed.Path,
		Title:          reparsed.Title,
		PropertiesJSON: string(propertiesJSON),
		LastMtime:      stat.ModTime().UnixNano(),
		LastSize:       stat.Size(),
		LastChecksum:   checksum,
	}); upsertErr != nil {
		return nil, upsertErr
	}

	if service.edges != nil {
		if upsertErr := service.edges.UpsertAll(reparsed.ID, reparsed.Path, flattenEdges(reparsed)); upsertErr != nil {
			return nil, upsertErr
		}
	}

	if service.embedQueue != nil {
		if enqueueErr := service.embedQueue.Enqueue(reparsed.ID); enqueueErr != nil {
			return nil, enqueueErr
		}
	}

	// Plan 7: after-phase + recovery surface.
	if service.behaviors != nil {
		_ = service.behaviors.FireNodeWriteAfter(beforeNode, reparsed)

		removed, added := diffEdgeSets(beforeNode, reparsed)

		for _, edgeRow := range removed {
			_ = service.behaviors.FireEdgeRemoveAfter(edgeRow)
		}

		for _, edgeRow := range added {
			_ = service.behaviors.FireEdgeAddAfter(edgeRow)
		}

		// Surface recovered events: stderr warning + drift row.
		now := time.Now().UnixNano()

		for _, recovered := range fireResult.Recovered {
			_, _ = fmt.Fprintf(service.warnings,
				"warning: workflow %q recovered from unknown status %q → %q on %s; transition not validated\n",
				recovered.PackInstance, recovered.From, recovered.To, reparsed.ID)

			if service.drift != nil {
				_ = service.drift.Append(index.WorkflowDriftRow{
					NodeID:         reparsed.ID,
					PackInstance:   recovered.PackInstance,
					PackKind:       recovered.PackKind,
					ObservedStatus: recovered.From,
					Property:       recovered.Property,
					ObservedAt:     now,
				})
			}
		}

		// Clean pass: no rejection, no recovery — clear any prior drift for this node.
		if len(fireResult.Recovered) == 0 && service.drift != nil {
			_ = service.drift.ClearForNode(reparsed.ID)
		}
	}

	// Plan 7.b: property drift surface — fires after index commits.
	if len(modPropResult.Drift) > 0 {
		now := time.Now().UnixNano()

		for _, drift := range modPropResult.Drift {
			_, _ = fmt.Fprintf(service.warnings,
				"warning: node-types: property %q is not declared on type %q; surfaces as a property-drift in tusk doctor\n",
				drift.Property, reparsed.Type)

			if service.propertyDrift != nil {
				_ = service.propertyDrift.Append(index.PropertyDriftRow{
					Kind:       "undeclared-property",
					NodeID:     reparsed.ID,
					NodeType:   reparsed.Type,
					Property:   drift.Property,
					Details:    drift.Reason,
					ObservedAt: now,
				})
			}
		}
	} else if service.propertyDrift != nil {
		// Clean pass: no drift — clear any prior property drift for this node.
		_ = service.propertyDrift.ClearForNode(reparsed.ID)
	}

	return reparsed, nil
}

// atomicWrite writes content to a sibling temp file and renames over absPath.
func atomicWrite(absPath string, content []byte) error {
	dir := filepath.Dir(absPath)

	tempFile, createErr := os.CreateTemp(dir, ".tusk-modify-*")

	if createErr != nil {
		return createErr
	}

	tempPath := tempFile.Name()

	if _, writeErr := tempFile.Write(content); writeErr != nil {
		tempFile.Close()
		_ = os.Remove(tempPath)

		return writeErr
	}

	if syncErr := tempFile.Sync(); syncErr != nil {
		tempFile.Close()
		_ = os.Remove(tempPath)

		return syncErr
	}

	if closeErr := tempFile.Close(); closeErr != nil {
		_ = os.Remove(tempPath)

		return closeErr
	}

	return os.Rename(tempPath, absPath)
}

// resolveTargetType looks up a target's node type in the index. Returns
// ("", false) when the target is not known (which the validator treats as
// "allowed for now").
func (service *Service) resolveTargetType(targetID string) (string, bool) {
	row, getErr := service.repo.Get(targetID)

	if getErr != nil {
		return "", false
	}

	return row.Type, true
}

// flattenEdges turns parsed.Edges (map of edge-type → []targetID) into the
// EdgeRow shape expected by index.EdgeRepo.UpsertAll. Order is preserved
// within each edge type via Ordinal.
func flattenEdges(parsedNode *Node) []index.EdgeRow {
	var rows []index.EdgeRow

	for edgeType, targets := range parsedNode.Edges {
		for ordinal, target := range targets {
			rows = append(rows, index.EdgeRow{
				Type:       edgeType,
				SourceID:   parsedNode.ID,
				TargetID:   target,
				Ordinal:    ordinal,
				SourcePath: parsedNode.Path,
			})
		}
	}

	return rows
}

// diffEdgeSets compares before vs. after edge sets and returns the rows
// to fire EdgeRemove / EdgeAdd hooks for. A row identifies its edge by
// (Type, SourceID, TargetID, Ordinal); ordering matches flattenEdges.
func diffEdgeSets(before, after *Node) (removed, added []index.EdgeRow) {
	beforeRows := flattenEdges(before)
	afterRows := flattenEdges(after)

	type key struct {
		typeName string
		sourceID string
		targetID string
		ordinal  int
	}

	beforeSet := make(map[key]index.EdgeRow, len(beforeRows))

	for _, row := range beforeRows {
		beforeSet[key{row.Type, row.SourceID, row.TargetID, row.Ordinal}] = row
	}

	afterSet := make(map[key]index.EdgeRow, len(afterRows))

	for _, row := range afterRows {
		afterSet[key{row.Type, row.SourceID, row.TargetID, row.Ordinal}] = row
	}

	for kk, row := range beforeSet {
		if _, present := afterSet[kk]; !present {
			removed = append(removed, row)
		}
	}

	for kk, row := range afterSet {
		if _, present := beforeSet[kk]; !present {
			added = append(added, row)
		}
	}

	return removed, added
}

// appendUnique appends value to slice only if not already present.
func appendUnique(slice []string, value string) []string {
	for _, existing := range slice {
		if existing == value {
			return slice
		}
	}

	return append(slice, value)
}

// detectCyclesForAcyclicEdges runs DetectCycle for every edge of an Acyclic
// type, using the index as the existing-graph oracle.
func (service *Service) detectCyclesForAcyclicEdges(parsedNode *Node) error {
	if service.edges == nil {
		return nil
	}

	for edgeType, targets := range parsedNode.Edges {
		definition, declared := service.edgeTypes[edgeType]

		if !declared || !definition.Acyclic {
			continue
		}

		existing, loadErr := service.loadAdjacencyForType(edgeType)

		if loadErr != nil {
			return loadErr
		}

		for _, target := range targets {
			if cycleErr := DetectCycle(CycleProbe{EdgeType: edgeType, Source: parsedNode.ID, Target: target}, existing); cycleErr != nil {
				return cycleErr
			}
		}
	}

	return nil
}

// loadAdjacencyForType builds the existing-graph adjacency map for a single
// edge type by walking every row in EdgeRepo of that type.
func (service *Service) loadAdjacencyForType(edgeType string) (map[string][]string, error) {
	rows, listErr := service.edges.ListByType(edgeType)

	if listErr != nil {
		return nil, listErr
	}

	adjacency := map[string][]string{}

	for _, row := range rows {
		adjacency[row.SourceID] = append(adjacency[row.SourceID], row.TargetID)
	}

	return adjacency, nil
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
		case []any:
			builder.WriteString(key)
			builder.WriteString(":\n")
			for _, element := range typed {
				elementString, isString := element.(string)
				if !isString {
					return nil, fmt.Errorf("node: unsupported sequence element type for %s: %T", key, element)
				}
				builder.WriteString("  - ")
				builder.WriteString(elementString)
				builder.WriteString("\n")
			}
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
