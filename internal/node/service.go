package node

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/leaseconfig"
	"github.com/germanamz/tusk/internal/manifest"
	"gopkg.in/yaml.v3"
)

// ErrAlreadyExists is returned by Create when the target file already exists.
var ErrAlreadyExists = errors.New("node: file already exists")

// ErrLeaseNotConfigured is returned by Service.Create when the Service
// was constructed without a FileStateRepo. The lease path is required
// for write handlers — read-only constructors (NewService,
// NewServiceWithManifest, NewServiceWithEmbedQueue) leave fileState nil
// and produce services that cannot Create.
var ErrLeaseNotConfigured = errors.New("node: service constructed without lease; use NewServiceWithLease or NewServiceWithBehaviors")

// ErrHTMLNodeNotEditable indicates an attempt to create or modify an HTML node
// through the node service. HTML files are authored and edited externally; the
// service only reads them (Get) and the engine indexes them. Mutating one here
// would re-render it as markdown and corrupt the file.
var ErrHTMLNodeNotEditable = errors.New("node: HTML nodes cannot be created or modified via the node service; edit the .html file directly")

// ErrPathEscapesVault is returned when a write target would resolve outside the
// workspace root (an absolute path or one containing "..").
var ErrPathEscapesVault = errors.New("node: path escapes the workspace root")

// ErrReservedID is returned when a write target's derived node id would collide
// with tusk's reserved id syntax (a '#' aliases the sub-unit separator; a
// "reindex:" prefix collides with the embed-queue key namespace). Such a file
// could never be indexed, so the write surface refuses to create it (#683).
var ErrReservedID = errors.New("node: path is not indexable")

// ensureVaultLocal rejects a workspace-relative path that does not stay inside
// the vault. It guards the write surface (Create / Rename) so neither the CLI
// nor the MCP tools can write a file outside the workspace root — an LLM acting
// on untrusted content must not be able to turn node_create/node_move into an
// out-of-vault write primitive.
func ensureVaultLocal(relPath string) error {
	if !filepath.IsLocal(relPath) {
		return fmt.Errorf("%w: %q", ErrPathEscapesVault, relPath)
	}

	return nil
}

// ensureIndexableID rejects a write target whose derived node id would collide
// with tusk's reserved id syntax. It guards Create and Rename so tusk cannot
// author a file the indexer would then have to silently skip — the same
// collisions the reindex walk refuses to enqueue.
func ensureIndexableID(relPath string) error {
	if reason := index.ReservedIDReason(relPath); reason != "" {
		return fmt.Errorf("%w: %s", ErrReservedID, reason)
	}

	return nil
}

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
	Body      []byte         // optional; when non-nil, replaces the markdown body (nil leaves the body untouched)
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

	refs RefLookup // optional; nil = ref resolution disabled

	// Lease primitives required by Create's WriteWithLease path. Nil
	// fileState means the service was built via a read-only constructor
	// (NewService, NewServiceWithManifest, NewServiceWithEmbedQueue) and
	// Create will reject with ErrLeaseNotConfigured.
	fileState *index.FileStateRepo
	workerID  string
	leaseTTL  time.Duration
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

// NewServiceWithLease constructs a write-capable Service wired with a
// FileStateRepo so Create routes through WriteWithLease. It is the
// minimal lease-aware constructor; callers that need behavior hooks,
// drift, or ref resolution should use NewServiceWithBehaviors instead.
func NewServiceWithLease(
	workspaceRoot string,
	repo *index.NodeRepo,
	edges *index.EdgeRepo,
	edgeTypes manifest.EdgeTypes,
	embedQueue *index.EmbedQueueRepo,
	fileState *index.FileStateRepo,
	workerID string,
	leaseTTL time.Duration,
) *Service {
	return &Service{
		root:       workspaceRoot,
		repo:       repo,
		edges:      edges,
		edgeTypes:  edgeTypes,
		embedQueue: embedQueue,
		fileState:  fileState,
		workerID:   workerID,
		leaseTTL:   leaseTTL,
	}
}

// ServiceDeps is the named-field bundle the write-capable Service constructor
// takes. It carries one field per positional parameter of the historical
// NewServiceWithBehaviors so call sites can wire dependencies by name and stop
// restating a 14-argument positional recipe. Optional fields may be left zero:
// nil EmbedQueue skips embed enqueues, nil NodeTypes runs untyped, nil Behaviors
// disables hook dispatch, nil Refs disables ref resolution, nil Warnings
// defaults to io.Discard.
type ServiceDeps struct {
	WorkspaceRoot string
	Repo          *index.NodeRepo
	Edges         *index.EdgeRepo
	EdgeTypes     manifest.EdgeTypes
	EmbedQueue    *index.EmbedQueueRepo
	NodeTypes     map[string]manifest.NodeType
	PropertyDrift *index.PropertyDriftRepo
	Behaviors     Behaviors
	Drift         *index.WorkflowDriftRepo
	Warnings      io.Writer
	Refs          RefLookup
	FileState     *index.FileStateRepo
	WorkerID      string
	LeaseTTL      time.Duration
}

// DepsFromIndex assembles the ServiceDeps fields derivable from an open index
// and the merged manifest: the repo handles (fresh, stateless handles over the
// same DB), the manifest-driven edge/node types, the ref lookup, and the
// lease primitives (worker id + resolved TTL). The two per-runtime divergent
// inputs — the behavior engine and the warnings writer — are passed explicitly
// so this helper never bakes in runtime-specific state. Callers can override
// any returned field before constructing the Service.
func DepsFromIndex(workspaceRoot string, store *index.Index, loaded *manifest.Manifest, behaviors Behaviors, warnings io.Writer) ServiceDeps {
	nodes := index.NewNodeRepo(store)

	return ServiceDeps{
		WorkspaceRoot: workspaceRoot,
		Repo:          nodes,
		Edges:         index.NewEdgeRepo(store),
		EdgeTypes:     loaded.EdgeTypes,
		EmbedQueue:    index.NewEmbedQueueRepo(store),
		NodeTypes:     loaded.NodeTypes,
		PropertyDrift: index.NewPropertyDriftRepo(store),
		Behaviors:     behaviors,
		Drift:         index.NewWorkflowDriftRepo(store),
		Warnings:      warnings,
		Refs:          NewIndexRefLookup(nodes),
		FileState:     index.NewFileStateRepo(store),
		WorkerID:      index.WorkerID(),
		LeaseTTL:      leaseconfig.Resolve(loaded.Lease.TTLSeconds),
	}
}

// NewServiceWithDeps is the named-field production constructor. It is the single
// place the write-capable Service is assembled; NewServiceWithBehaviors wraps
// it for callers that still pass positional arguments. A nil Warnings writer
// defaults to io.Discard.
func NewServiceWithDeps(deps ServiceDeps) *Service {
	warnings := deps.Warnings

	if warnings == nil {
		warnings = io.Discard
	}

	return &Service{
		root:          deps.WorkspaceRoot,
		repo:          deps.Repo,
		edges:         deps.Edges,
		edgeTypes:     deps.EdgeTypes,
		embedQueue:    deps.EmbedQueue,
		nodeTypes:     deps.NodeTypes,
		propertyDrift: deps.PropertyDrift,
		behaviors:     deps.Behaviors,
		drift:         deps.Drift,
		warnings:      warnings,
		refs:          deps.Refs,
		fileState:     deps.FileState,
		workerID:      deps.WorkerID,
		leaseTTL:      deps.LeaseTTL,
	}
}

// WithWarningWriter returns a shallow copy of the Service that writes recovery /
// property-drift warnings to warnings instead of the receiver's writer. Every
// other dependency is shared with the receiver (the repos and engine are
// concurrency-safe handles), so callers that need a per-call warning sink can
// derive one without rebuilding the whole dependency recipe. A nil writer
// defaults to io.Discard.
func (service *Service) WithWarningWriter(warnings io.Writer) *Service {
	if warnings == nil {
		warnings = io.Discard
	}

	clone := *service
	clone.warnings = warnings

	return &clone
}

// NewServiceWithBehaviors is the Plan 7 production constructor: like
// NewServiceWithEmbedQueue, but also wires the behavior engine, the
// drift log, and a warnings writer (defaults to io.Discard when nil).
// Plan 7.b adds nodeTypes and propertyDrift; Plan 7.c.1 adds refs for
// ref-property resolution. Phase 4 (T4.2) adds the lease primitives
// (fileState, workerID, leaseTTL) — Create requires these. Pass nil
// for unused optional fields; nil refs disables ref resolution.
//
// It delegates to NewServiceWithDeps; prefer that named-field constructor at
// new call sites.
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
	refs RefLookup,
	fileState *index.FileStateRepo,
	workerID string,
	leaseTTL time.Duration,
) *Service {
	return NewServiceWithDeps(ServiceDeps{
		WorkspaceRoot: workspaceRoot,
		Repo:          repo,
		Edges:         edges,
		EdgeTypes:     edgeTypes,
		EmbedQueue:    embedQueue,
		NodeTypes:     nodeTypes,
		PropertyDrift: propertyDrift,
		Behaviors:     behaviors,
		Drift:         drift,
		Warnings:      warnings,
		Refs:          refs,
		FileState:     fileState,
		WorkerID:      workerID,
		LeaseTTL:      leaseTTL,
	})
}

// reservedProperties is a nil-safe accessor for Behaviors.ReservedProperties.
// Returns nil when the service has no behavior engine wired.
func (service *Service) reservedProperties() map[string]map[string]struct{} {
	if service.behaviors == nil {
		return nil
	}

	return service.behaviors.ReservedProperties()
}

// Create writes the node file and upserts the index row in one operation.
// When the service has an EdgeRepo configured, edges are also persisted.
//
// persistNodeRow stats the just-written file, checksums the rendered bytes, and
// upserts the node's index row. It runs after WriteWithLease has committed the
// file (no lease is held here) and stops at repo.Upsert — edge upserts and embed
// enqueues stay at the call site. Create and Modify share it; each passes its
// own already-computed absPath.
func (service *Service) persistNodeRow(absPath string, node *Node, rendered []byte) error {
	stat, statErr := os.Stat(absPath)

	if statErr != nil {
		return fmt.Errorf("node: stat %s: %w", absPath, statErr)
	}

	checksum := sha256Hex(rendered)
	propertiesJSON, marshalErr := json.Marshal(node.Properties)

	if marshalErr != nil {
		return fmt.Errorf("node: marshal properties: %w", marshalErr)
	}

	if upsertErr := service.repo.Upsert(index.NodeRow{
		ID:             node.ID,
		Type:           node.Type,
		Path:           node.Path,
		Title:          node.Title,
		PropertiesJSON: string(propertiesJSON),
		LastMtime:      stat.ModTime().UnixNano(),
		LastSize:       stat.Size(),
		LastChecksum:   checksum,
	}); upsertErr != nil {
		return upsertErr
	}

	return nil
}

// surfacePropertyDrift emits the undeclared-property warnings + drift rows, or
// clears prior drift on a clean pass. It fires after the index commits, holds no
// lease, and touches no file. Both Create and Modify call it as their last
// statement before returning.
func (service *Service) surfacePropertyDrift(node *Node, drift []PropertyDrift) {
	if len(drift) > 0 {
		now := time.Now().UnixNano()

		for _, entry := range drift {
			_, _ = fmt.Fprintf(service.warnings,
				"warning: node-types: property %q is not declared on type %q; surfaces as a property-drift in tusk doctor\n",
				entry.Property, node.Type)

			if service.propertyDrift != nil {
				_ = service.propertyDrift.Append(index.PropertyDriftRow{
					Kind:       "undeclared-property",
					NodeID:     node.ID,
					NodeType:   node.Type,
					Property:   entry.Property,
					Details:    entry.Reason,
					ObservedAt: now,
				})
			}
		}
	} else if service.propertyDrift != nil {
		// Clean pass: no hard errors, no drift — clear any prior drift for this node.
		_ = service.propertyDrift.ClearForNode(node.ID)
	}
}

// resolveAndValidate runs the shared edge/property resolution-and-validation
// pipeline that Create and Modify both need: initialize the Edges map, resolve
// ref properties into edges (rejecting on hard ref errors), resolve and
// validate edges, detect cycles on acyclic edge types, then validate
// properties and filter reserved-property drift. It returns the property
// validation result (with reserved drift already filtered) so each caller can
// apply its own op-specific HardErrors gate — Create rejects on any hard error,
// Modify first suppresses ErrRequiredMissing and adds its required-unset check.
//
// The single asymmetry between the two callers is materializeWikilinks: Create
// passes true (body [[wikilinks]] become edges), Modify passes false (body
// changes are out of scope for Modify, so wikilinks are not re-materialized).
//
// op ("create" / "modify") is threaded only into RefValidationError so the
// surfaced error names the operation. A non-nil error is the caller's signal to
// reject before writing.
func (service *Service) resolveAndValidate(parsed *Node, op string, materializeWikilinks bool) (PropertyValidationResult, error) {
	// Initialize Edges map (ParseFile does not; ResolveEdges also does this,
	// but ResolveRefs must run first for ref-typed properties).
	if parsed.Edges == nil {
		parsed.Edges = map[string][]string{}
	}

	// Plan 7.c.1: ref resolution — runs before ResolveEdges so that ref
	// property values (bare titles / wikilinks) are resolved before the
	// raw-ID edge pass consumes them. On hard error, reject before any write.
	// On success, remove the resolved properties from parsed.Properties so
	// ResolveEdges does not treat them as raw-ID edges.
	if service.refs != nil {
		refResult := ResolveRefs(parsed, service.nodeTypes, service.refs)

		if len(refResult.HardErrors) > 0 {
			return PropertyValidationResult{}, &RefValidationError{
				Op:       op,
				NodeID:   parsed.ID,
				NodeType: parsed.Type,
				Errors:   refResult.HardErrors,
			}
		}

		for _, edge := range refResult.Edges {
			parsed.Edges[edge.EdgeType] = appendUnique(parsed.Edges[edge.EdgeType], edge.TargetID)
		}

		// Remove resolved ref properties so ResolveEdges skips them.
		removeRefProperties(parsed, service.nodeTypes)
	}

	if resolveErr := ResolveEdges(parsed, service.edgeTypes); resolveErr != nil {
		return PropertyValidationResult{}, resolveErr
	}

	if materializeWikilinks {
		MaterializeWikilinks(parsed, service.edgeTypes)
	}

	if validateErr := ValidateEdges(parsed, service.edgeTypes, EdgeContext{
		ResolveTargetType: service.resolveTargetType,
	}); validateErr != nil {
		return PropertyValidationResult{}, validateErr
	}

	if cycleErr := service.detectCyclesForAcyclicEdges(parsed); cycleErr != nil {
		return PropertyValidationResult{}, cycleErr
	}

	// Plan 7.b: property validation — runs before hook validate-phase.
	propResult := ValidateProperties(parsed, service.nodeTypes)
	propResult.Drift = FilterReservedDrift(propResult.Drift, parsed.Type, service.reservedProperties())

	return propResult, nil
}

// Phase 4 (T4.2): the file write routes through WriteWithLease so the
// file_state row is populated and concurrent creates against the same
// path are coordinated by the lease. Requires the service to have been
// constructed with lease wiring; ErrLeaseNotConfigured otherwise.
func (service *Service) Create(input CreateInput) (*Node, error) {
	if service.fileState == nil {
		return nil, ErrLeaseNotConfigured
	}

	if IsHTMLPath(input.RelPath) {
		return nil, ErrHTMLNodeNotEditable
	}

	if localErr := ensureVaultLocal(input.RelPath); localErr != nil {
		return nil, localErr
	}

	if reservedErr := ensureIndexableID(input.RelPath); reservedErr != nil {
		return nil, reservedErr
	}

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

	// Shared resolve+validate pipeline. Create materializes body wikilinks
	// into edges (true); the op-specific HardErrors gate stays here.
	propResult, validateErr := service.resolveAndValidate(parsed, "create", true)

	if validateErr != nil {
		return nil, validateErr
	}

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

		for _, edgeRow := range flattenEdges(parsed, service.nodeTypes) {
			if rejector, fireErr := service.behaviors.FireEdgeAddValidate(edgeRow); fireErr != nil {
				return nil, fmt.Errorf("behavior %s rejected edge add: %w", rejector, fireErr)
			}
		}
	}

	mutator := func(current []byte) (Mutation, error) {
		// Race-safety guard: the lease has been claimed and the file
		// (re-)read under the lease. If something is already there, the
		// pre-write os.Stat check above lost a race with another writer.
		if len(current) > 0 {
			return Mutation{}, ErrAlreadyExists
		}

		return WriteReplace(rendered), nil
	}

	if writeErr := WriteWithLease(
		context.Background(),
		service.root,
		service.fileState,
		service.workerID,
		service.leaseTTL,
		input.RelPath,
		mutator,
	); writeErr != nil {
		return nil, writeErr
	}

	if persistErr := service.persistNodeRow(absPath, parsed, rendered); persistErr != nil {
		return nil, persistErr
	}

	edgeRows := flattenEdges(parsed, service.nodeTypes)

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
	service.surfacePropertyDrift(parsed, propResult.Drift)

	return parsed, nil
}

// Modify reads a node from disk, applies SetProps/UnsetKeys, optionally
// replaces the body (input.Body != nil), validates against the manifest,
// atomically rewrites the file, and updates index rows. Modify enqueues the
// node for re-embedding when the service has an EmbedQueue. When the body is
// replaced, body wikilinks are materialized into edges as in Create.
//
// Phase 4 (T4.3): the file write routes through WriteWithLease. The
// read+parse+apply+render+validate pipeline runs inside the Mutator so
// the on-disk bytes seen by validation are the bytes the lease commits.
// A no-op delta (rendered == current) returns WriteNoChange and skips
// post-write index/embed/hook work — mtime stays put and the embed
// queue does not grow.
func (service *Service) Modify(input ModifyInput) (*Node, error) {
	if service.fileState == nil {
		return nil, ErrLeaseNotConfigured
	}

	row, getErr := service.repo.Get(input.ID)

	if getErr != nil {
		return nil, getErr
	}

	if IsHTMLPath(row.Path) {
		return nil, ErrHTMLNodeNotEditable
	}

	var (
		beforeNode    *Node
		reparsed      *Node
		rendered      []byte
		modPropResult PropertyValidationResult
		fireResult    FireResult
		changed       bool
	)

	mutator := func(current []byte) (Mutation, error) {
		if current == nil {
			return Mutation{}, fmt.Errorf("node: %s: file vanished", row.Path)
		}

		parsedBefore, parseBeforeErr := ParseFile(row.Path, current)

		if parseBeforeErr != nil {
			return Mutation{}, parseBeforeErr
		}

		// Canonicalize any date the YAML parser produced as a time.Time (an
		// unquoted on-disk date) into its string form before cloning, rendering,
		// or validating — renderMarkdown cannot serialize a time.Time, and the
		// date validator expects a string. Lets a modify succeed on a node whose
		// date was authored unquoted, and re-emits it quoted.
		CanonicalizeDates(parsedBefore, service.nodeTypes)

		// Clone the after-node BEFORE resolving edges on the before-node. Edge
		// resolution moves edge-type keys out of Properties (edges.go), so
		// cloning first keeps those keys in the clone's Properties — the
		// re-render then re-emits them and an unrelated set/unset no longer
		// deletes the node's relationships (#670). Set/Unset targeting an edge
		// key still works: it mutates the clone's Properties directly.
		parsed := parsedBefore.Clone()

		// Resolve edges on the before-node (mutating it) so the diff against the
		// after-node is well-defined; the clone above is untouched by this.
		if resolveErr := ResolveEdges(parsedBefore, service.edgeTypes); resolveErr != nil {
			return Mutation{}, resolveErr
		}

		beforeNode = parsedBefore

		// Apply Set/Unset to produce after-node.

		for _, key := range input.UnsetKeys {
			if key == "type" {
				return Mutation{}, fmt.Errorf("node: cannot unset reserved key %q", key)
			}

			delete(parsed.Properties, key)
		}

		for key, value := range input.SetProps {
			if key == "type" && value != parsed.Type {
				return Mutation{}, fmt.Errorf("node: cannot change type via Modify (current=%q, requested=%v)", parsed.Type, value)
			}

			parsed.Properties[key] = value
		}

		// Optional body overwrite. A nil Body leaves the existing body in place
		// (the common frontmatter-only modify); a non-nil Body (including empty)
		// replaces it.
		if input.Body != nil {
			parsed.Body = input.Body
		}

		newRendered, renderErr := renderMarkdown(parsed.Properties, parsed.Body)

		if renderErr != nil {
			return Mutation{}, renderErr
		}

		newParsed, reparseErr := ParseFile(row.Path, newRendered)

		if reparseErr != nil {
			return Mutation{}, reparseErr
		}

		// Shared resolve+validate pipeline. Body wikilinks are materialized into
		// edges only when this modify replaces the body (input.Body != nil),
		// matching Create; a frontmatter-only modify passes false so its edge set
		// is untouched. The op-specific HardErrors handling (ErrRequiredMissing
		// suppression + required-unset check) stays below.
		propResult, validateErr := service.resolveAndValidate(newParsed, "modify", input.Body != nil)

		if validateErr != nil {
			return Mutation{}, validateErr
		}

		// In the Modify path, ErrRequiredMissing from the validator is suppressed:
		// properties that were never set on a pre-existing node are not blocked by
		// Modify (the node predates the declaration). ErrCannotUnsetRequired (below)
		// handles the case where a required property is explicitly removed.
		var modHardErrors []PropertyError

		for _, pe := range propResult.HardErrors {
			if pe.Kind != ErrRequiredMissing {
				modHardErrors = append(modHardErrors, pe)
			}
		}

		propResult.HardErrors = modHardErrors

		// Detect explicit unset of required properties. We check input.UnsetKeys
		// directly so that unsetting a required key that was never present is
		// also caught.
		if nt, declared := service.nodeTypes[newParsed.Type]; declared {
			declByNameForUnset := make(map[string]manifest.PropertyDecl, len(nt.Properties))

			for _, decl := range nt.Properties {
				declByNameForUnset[decl.Name] = decl
			}

			for _, unsetKey := range input.UnsetKeys {
				if decl, found := declByNameForUnset[unsetKey]; found && decl.Required {
					propResult.HardErrors = append(propResult.HardErrors, PropertyError{
						Kind:     ErrCannotUnsetRequired,
						Property: unsetKey,
						Reason:   fmt.Sprintf("cannot unset required property %q on type %q", unsetKey, newParsed.Type),
					})
				}
			}
		}

		if len(propResult.HardErrors) > 0 {
			return Mutation{}, &PropertyValidationError{
				Op:       "modify",
				NodeID:   newParsed.ID,
				NodeType: newParsed.Type,
				Errors:   propResult.HardErrors,
			}
		}

		// Plan 7: recovery-aware validate phase + edge diff hooks.
		var fr FireResult

		if service.behaviors != nil {
			result, fireErr := service.behaviors.FireNodeWriteValidateWithRecovery(parsedBefore, newParsed)

			if fireErr != nil {
				return Mutation{}, fmt.Errorf("behavior %s rejected modify: %w", result.Rejector, fireErr)
			}

			fr = result

			removed, added := diffEdgeSets(parsedBefore, newParsed, service.nodeTypes)

			for _, edgeRow := range removed {
				if rejector, edgeFireErr := service.behaviors.FireEdgeRemoveValidate(edgeRow); edgeFireErr != nil {
					return Mutation{}, fmt.Errorf("behavior %s rejected edge remove: %w", rejector, edgeFireErr)
				}
			}

			for _, edgeRow := range added {
				if rejector, edgeFireErr := service.behaviors.FireEdgeAddValidate(edgeRow); edgeFireErr != nil {
					return Mutation{}, fmt.Errorf("behavior %s rejected edge add: %w", rejector, edgeFireErr)
				}
			}
		}

		// No-op detection: validation has passed; if the rendered bytes
		// match what's already on disk, release the lease without touching
		// the file or the embed queue. Validation still ran so input-only
		// errors (e.g. unsetting a required key that was never set) surface.
		if bytes.Equal(newRendered, current) {
			reparsed = parsedBefore
			return WriteNoChange(), nil
		}

		reparsed = newParsed
		rendered = newRendered
		modPropResult = propResult
		fireResult = fr
		changed = true

		return WriteReplace(newRendered), nil
	}

	if writeErr := WriteWithLease(
		context.Background(),
		service.root,
		service.fileState,
		service.workerID,
		service.leaseTTL,
		row.Path,
		mutator,
	); writeErr != nil {
		return nil, writeErr
	}

	if !changed {
		return reparsed, nil
	}

	absPath := filepath.Join(service.root, row.Path)

	if persistErr := service.persistNodeRow(absPath, reparsed, rendered); persistErr != nil {
		return nil, persistErr
	}

	if service.edges != nil {
		// UpsertContentEdges, not UpsertAll: Modify re-derives only the file's
		// own frontmatter/body edges and runs no sub-unit sync, so its
		// kind='structural' contains rows must be preserved. A blanket delete
		// dropped them permanently — Modify writes through the lease, so
		// file_state records the new mtime and the incremental reindex that
		// would otherwise re-sync them always skips the file (#680).
		if upsertErr := service.edges.UpsertContentEdges(reparsed.ID, reparsed.Path, flattenEdges(reparsed, service.nodeTypes)); upsertErr != nil {
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

		removed, added := diffEdgeSets(beforeNode, reparsed, service.nodeTypes)

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
					ErrorCode:      "recovered",
					Detail:         recovered.Message,
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
	service.surfacePropertyDrift(reparsed, modPropResult.Drift)

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
// EdgeRow shape expected by index.EdgeRepo.UpsertAll.
//
// Each row is tagged with `kind`: "derived" when the edge-type name matches a
// ref-property declared on parsedNode.Type (synthesized by the manifest
// loader's `synthesizeRefEdgeTypes`); "direct" for every other edge. Source
// is left NULL — structural sub-unit edges never come through this code path.
func flattenEdges(parsedNode *Node, nodeTypes map[string]manifest.NodeType) []index.EdgeRow {
	refProps := refPropertyNamesForType(parsedNode.Type, nodeTypes)

	var rows []index.EdgeRow

	for edgeType, targets := range parsedNode.Edges {
		kind := "direct"

		if _, isRef := refProps[edgeType]; isRef {
			kind = "derived"
		}

		for _, target := range targets {
			rows = append(rows, index.EdgeRow{
				Type:       edgeType,
				SourceID:   parsedNode.ID,
				TargetID:   target,
				SourcePath: parsedNode.Path,
				Kind:       kind,
			})
		}
	}

	return rows
}

// refPropertyNamesForType returns the set of property names that are
// ref-shaped on the given node type. Returns an empty (non-nil) map when the
// type is unknown or has no ref properties.
func refPropertyNamesForType(typeName string, nodeTypes map[string]manifest.NodeType) map[string]struct{} {
	out := map[string]struct{}{}

	nodeType, declared := nodeTypes[typeName]

	if !declared {
		return out
	}

	for _, prop := range nodeType.Properties {
		if manifest.IsRefProperty(prop) {
			out[prop.Name] = struct{}{}
		}
	}

	return out
}

// diffEdgeSets compares before vs. after edge sets and returns the rows
// to fire EdgeRemove / EdgeAdd hooks for. A row identifies its edge by
// (Type, SourceID, TargetID); ordering matches flattenEdges.
func diffEdgeSets(before, after *Node, nodeTypes map[string]manifest.NodeType) (removed, added []index.EdgeRow) {
	beforeRows := flattenEdges(before, nodeTypes)
	afterRows := flattenEdges(after, nodeTypes)

	type key struct {
		typeName string
		sourceID string
		targetID string
	}

	beforeSet := make(map[key]index.EdgeRow, len(beforeRows))

	for _, row := range beforeRows {
		beforeSet[key{row.Type, row.SourceID, row.TargetID}] = row
	}

	afterSet := make(map[key]index.EdgeRow, len(afterRows))

	for _, row := range afterRows {
		afterSet[key{row.Type, row.SourceID, row.TargetID}] = row
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

// removeRefProperties deletes from parsed.Properties any key that is a
// ref-shaped PropertyDecl on the node's type. This is called after ref
// resolution so that ResolveEdges does not re-process resolved ref values
// as raw-ID edges.
func removeRefProperties(parsedNode *Node, decls map[string]manifest.NodeType) {
	if decls == nil {
		return
	}

	nodeType, declared := decls[parsedNode.Type]
	if !declared {
		return
	}

	for _, prop := range nodeType.Properties {
		if manifest.IsRefProperty(prop) {
			delete(parsedNode.Properties, prop.Name)
		}
	}
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

	return ParseContentFile(row.Path, content)
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

// renderMarkdown serializes properties as YAML frontmatter and concatenates the
// body. `type` is emitted first and `title` second (when present and not an
// empty string); every other key follows in sorted order, so repeated renders
// of the same node are byte-identical (diff-friendly).
//
// Values are serialized through yaml.v3, which is the single source of truth for
// how a frontmatter value becomes YAML. Every shape a node file can legally hold
// — nested maps, nulls, empty lists, multi-line strings, mixed-type sequences —
// round-trips losslessly, and any string a YAML parser would otherwise resolve
// to a non-string (a date like "2026-06-11", "true", "500") stays quoted. The
// list indent is pinned to two spaces to match the historical output.
func renderMarkdown(properties map[string]any, body []byte) ([]byte, error) {
	root := &yaml.Node{Kind: yaml.MappingNode}

	appendPair := func(key string, value any) error {
		valueNode := &yaml.Node{}

		if encodeErr := valueNode.Encode(value); encodeErr != nil {
			return fmt.Errorf("node: encode frontmatter key %q: %w", key, encodeErr)
		}

		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: key},
			valueNode,
		)

		return nil
	}

	if typeValue, hasType := properties["type"]; hasType {
		if appendErr := appendPair("type", typeValue); appendErr != nil {
			return nil, appendErr
		}
	}

	// A non-string title is preserved (emitted via yaml), not dropped; only an
	// absent or empty-string title is skipped, matching the historical behavior.
	if titleValue, hasTitle := properties["title"]; hasTitle && !isEmptyTitle(titleValue) {
		if appendErr := appendPair("title", titleValue); appendErr != nil {
			return nil, appendErr
		}
	}

	rest := make([]string, 0, len(properties))

	for key := range properties {
		if key == "type" || key == "title" {
			continue
		}

		rest = append(rest, key)
	}

	sort.Strings(rest)

	for _, key := range rest {
		if appendErr := appendPair(key, properties[key]); appendErr != nil {
			return nil, appendErr
		}
	}

	var frontmatter bytes.Buffer

	if len(root.Content) > 0 {
		encoder := yaml.NewEncoder(&frontmatter)
		encoder.SetIndent(2)

		if encodeErr := encoder.Encode(root); encodeErr != nil {
			return nil, fmt.Errorf("node: render frontmatter: %w", encodeErr)
		}

		if closeErr := encoder.Close(); closeErr != nil {
			return nil, fmt.Errorf("node: render frontmatter: %w", closeErr)
		}
	}

	var out bytes.Buffer

	out.WriteString("---\n")
	out.Write(frontmatter.Bytes())
	out.WriteString("---\n\n")
	out.Write(body)

	if !bytes.HasSuffix(body, []byte("\n")) {
		out.WriteString("\n")
	}

	return out.Bytes(), nil
}

// isEmptyTitle reports whether a title value should be omitted from rendered
// frontmatter: an absent (nil) or empty-string title. Any other value —
// including a non-string one — is emitted so it is never silently dropped.
func isEmptyTitle(value any) bool {
	if value == nil {
		return true
	}

	str, isString := value.(string)

	return isString && str == ""
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)

	return hex.EncodeToString(sum[:])
}

// yamlQuoteString returns the string quoted with double-quotes when emitting
// it as a YAML scalar would change its meaning. Plain ASCII identifiers and
// simple strings are returned as-is. Conservative — a few values get quoted
// that don't strictly need it (e.g. all uses of `:`), but never the reverse:
// every output round-trips through a YAML 1.2 parser back to the original
// Go string.
func yamlQuoteString(str string) string {
	if !yamlNeedsQuoting(str) {
		return str
	}

	return yamlDoubleQuote(str)
}

// yamlDoubleQuote wraps str in a YAML double-quoted scalar, escaping the two
// characters that would otherwise end or re-interpret it.
func yamlDoubleQuote(str string) string {
	escaped := strings.ReplaceAll(str, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)

	return `"` + escaped + `"`
}

func yamlNeedsQuoting(str string) bool {
	if str == "" {
		return true
	}

	// Leading-character indicators that change YAML's interpretation.
	switch str[0] {
	case '-', '?', '[', ']', '{', '}', '&', '*', '!', '|', '>', '\'', '"', '%', '@', '`', '#', ',', ' ', '\t':
		return true
	}

	// Trailing whitespace would be stripped on a plain scalar; quote to
	// preserve it.
	switch str[len(str)-1] {
	case ' ', '\t':
		return true
	}

	for _, ch := range str {
		switch ch {
		case ':', '#', '\n', '\r', '"', '\'', '\\':
			return true
		}
	}

	// Reserved literals that YAML 1.1 / 1.2 parsers decode as bool or null.
	switch strings.ToLower(str) {
	case "true", "false", "yes", "no", "on", "off", "null", "~":
		return true
	}

	// Anything a YAML parser would decode as a number must be quoted to keep
	// it a string.
	if _, parseErr := strconv.ParseFloat(str, 64); parseErr == nil {
		return true
	}

	// Final safety net: defer to the real YAML parser for the forms the rules
	// above can't enumerate. A plain scalar resolves to a non-string for
	// date/timestamp strings ("2026-06-11" -> time.Time, issue #662), hex and
	// octal ints ("0x1F", "0o17"), and infinities (".inf"/".nan"). Quote
	// whenever the bare scalar would not decode back to this exact Go string,
	// honoring the round-trip guarantee this function promises.
	var decoded any

	if unmarshalErr := yaml.Unmarshal([]byte(str), &decoded); unmarshalErr != nil {
		return true
	}

	if decodedStr, isString := decoded.(string); !isString || decodedStr != str {
		return true
	}

	return false
}
