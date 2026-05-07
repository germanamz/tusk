package typepacks

import (
	"bytes"
	"fmt"
	"regexp"

	"github.com/BurntSushi/toml"

	"github.com/germanamz/tusk/internal/manifest"
)

// FindCollisions returns the qualified section names ("node-types.X",
// "edge-types.X", "behaviors.K.I") that exist in both the user manifest
// (raw bytes) and the pack manifest.
func FindCollisions(userBody []byte, pack *manifest.Manifest) ([]string, error) {
	userManifest := &manifest.Manifest{}

	if _, decodeErr := toml.Decode(string(userBody), userManifest); decodeErr != nil {
		return nil, fmt.Errorf("typepacks: decode user manifest: %w", decodeErr)
	}

	var collisions []string

	for typeName := range pack.NodeTypes {
		if _, present := userManifest.NodeTypes[typeName]; present {
			collisions = append(collisions, "node-types."+typeName)
		}
	}

	for edgeName := range pack.EdgeTypes {
		if _, present := userManifest.EdgeTypes[edgeName]; present {
			collisions = append(collisions, "edge-types."+edgeName)
		}
	}

	for kindName, perInstance := range pack.Behaviors {
		for instanceName := range perInstance {
			if userKind, present := userManifest.Behaviors[kindName]; present {
				if _, has := userKind[instanceName]; has {
					collisions = append(collisions, fmt.Sprintf("behaviors.%s.%s", kindName, instanceName))
				}
			}
		}
	}

	return collisions, nil
}

// sectionHeaderPattern matches a TOML section header on a line by
// itself (with optional leading/trailing whitespace). Captures the
// inner key (may be multi-segment like "behaviors.workflow.kanban").
var sectionHeaderPattern = regexp.MustCompile(`(?m)^\s*\[([^\]]+)\]\s*$`)

// StripSections removes the named sections from body. Sections is a
// list of qualified names like "node-types.task". Lines belonging to
// a stripped section header are discarded; other content is preserved.
func StripSections(body []byte, sections []string) []byte {
	target := make(map[string]struct{}, len(sections))

	for _, name := range sections {
		target[name] = struct{}{}
	}

	var output bytes.Buffer
	var current bytes.Buffer

	// currentHeader is the qualified section key of the block accumulating
	// in current. An empty string means we are in the preamble (before any
	// section header).
	currentHeader := ""
	inPreamble := true

	flush := func() {
		if inPreamble {
			// Preamble always flushes (no section header to check against).
			output.Write(current.Bytes())
			current.Reset()

			return
		}

		if _, drop := target[currentHeader]; drop {
			current.Reset()

			return
		}

		output.Write(current.Bytes())
		current.Reset()
	}

	for _, line := range bytes.SplitAfter(body, []byte("\n")) {
		match := sectionHeaderPattern.FindSubmatch(line)

		if match != nil {
			flush()
			inPreamble = false
			currentHeader = string(match[1])
		}

		current.Write(line)
	}

	// Flush the final block.
	flush()

	return output.Bytes()
}
