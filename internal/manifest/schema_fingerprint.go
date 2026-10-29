package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/BurntSushi/toml"
)

// schemaTables is what SchemaFingerprint hashes: the declarations the reindex
// worker derives a file's edges and drift from.
type schemaTables struct {
	NodeTypes map[string]NodeType
	EdgeTypes EdgeTypes
	Behaviors map[string]map[string]any
}

// SchemaFingerprint hashes [node-types], [edge-types] and [behaviors] as
// loaded, built-in packs included, with every type and property description
// cleared. Reindex records it and re-processes every file when it changes,
// because a file's edges and drift are derived from these tables when the file
// is parsed and an unchanged file is not parsed again (#831). Hashing whole
// tables keeps a field added later covered without a list to keep in step with
// the worker; descriptions are left out because nothing indexed reads them and
// they are the edit people make most. A nil manifest yields "".
func (loaded *Manifest) SchemaFingerprint() string {
	if loaded == nil {
		return ""
	}

	tables := schemaTables{
		NodeTypes: make(map[string]NodeType, len(loaded.NodeTypes)),
		EdgeTypes: make(EdgeTypes, len(loaded.EdgeTypes)),
		Behaviors: make(map[string]map[string]any, len(loaded.Behaviors)),
	}

	for name, nodeType := range loaded.NodeTypes {
		nodeType.Description = ""
		nodeType.Properties = slices.Clone(nodeType.Properties)

		for position := range nodeType.Properties {
			nodeType.Properties[position].Description = ""
		}

		tables.NodeTypes[name] = nodeType
	}

	// parent-is-source and max-depth only shape how queries walk a hierarchy,
	// so they are left out like descriptions.
	for name, edgeType := range loaded.EdgeTypes {
		edgeType.Description = ""
		edgeType.ParentIsSource = false
		edgeType.MaxDepth = 0
		tables.EdgeTypes[name] = edgeType
	}

	for kind, instances := range loaded.Behaviors {
		decoded := make(map[string]any, len(instances))

		for instance, raw := range instances {
			decoded[instance] = loaded.decodeBehavior(raw)
		}

		tables.Behaviors[kind] = decoded
	}

	encoded, marshalErr := json.Marshal(tables)

	if marshalErr != nil {
		// A NaN or infinite float in a behavior table, which JSON can't carry.
		// fmt sorts map keys too, so the text is still stable.
		encoded = fmt.Append(nil, tables)
	}

	sum := sha256.Sum256(encoded)

	return hex.EncodeToString(sum[:])
}

// decodeBehavior decodes one behavior instance's table into plain values. A
// hand-built manifest has no decode metadata, and a table that won't decode is
// one the behavior engine rejects first; either way only the instance's name
// counts toward the fingerprint.
func (loaded *Manifest) decodeBehavior(raw toml.Primitive) map[string]any {
	if loaded.Meta == nil {
		return nil
	}

	var table map[string]any

	if decodeErr := loaded.Meta.PrimitiveDecode(raw, &table); decodeErr != nil {
		return nil
	}

	return table
}
