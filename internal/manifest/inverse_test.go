package manifest_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
)

// hierarchyFixture declares a parent→child hierarchy with a depth cap and an
// edge type with an inverse label (#850).
const hierarchyFixture = `[workspace]
name = "x"

[node-types.service]
description = "s"

[edge-types.depends-on]
from        = ["service"]
to          = ["service"]
cardinality = "many-to-many"
inverse     = "depended-on-by"

[edge-types.has-part]
from             = ["service"]
to               = ["service"]
cardinality      = "one-to-many"
hierarchy        = "parts"
parent-is-source = true
max-depth        = 3
`

func TestLoad_HierarchyDirectionAndDepth(test *testing.T) {
	loaded := loadTOMLFromString(test, hierarchyFixture)
	edge := loaded.EdgeTypes["has-part"]

	if !edge.ParentIsSource || edge.MaxDepth != 3 {
		test.Errorf("has-part = parent-is-source %v, max-depth %d; want true, 3", edge.ParentIsSource, edge.MaxDepth)
	}

	if loaded.EdgeTypes["depends-on"].ParentIsSource || loaded.EdgeTypes["depends-on"].MaxDepth != 0 {
		test.Errorf("depends-on picked up hierarchy settings it doesn't declare")
	}
}

func TestLoad_RejectsBadHierarchySettings(test *testing.T) {
	cases := []struct {
		name    string
		replace string
		with    string
		want    string
	}{
		{"negative max-depth", "max-depth        = 3", "max-depth        = -1", `edge "has-part": max-depth must be at least 1`},
		{"max-depth without hierarchy", "inverse     = \"depended-on-by\"", "inverse     = \"depended-on-by\"\nmax-depth   = 2", `edge "depends-on": max-depth requires hierarchy alias`},
	}

	for _, testCase := range cases {
		_, loadErr := loadTOMLString(test, strings.Replace(hierarchyFixture, testCase.replace, testCase.with, 1))

		if loadErr == nil || !strings.Contains(loadErr.Error(), testCase.want) {
			test.Errorf("%s: err = %v, want it to contain %q", testCase.name, loadErr, testCase.want)
		}
	}
}

func TestLoad_RejectsCollidingInverses(test *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "inverse names another edge type",
			body: hierarchyFixture + `
[edge-types.depended-on-by]
from        = ["service"]
to          = ["service"]
cardinality = "many-to-many"
`,
			want: `edge-type "depends-on": inverse "depended-on-by" is also a declared edge type`,
		},
		{
			name: "inverse names its own edge type",
			body: strings.Replace(hierarchyFixture, `inverse     = "depended-on-by"`, `inverse     = "depends-on"`, 1),
			want: `edge-type "depends-on": inverse "depends-on" is also a declared edge type`,
		},
		{
			name: "two edge types share an inverse",
			body: hierarchyFixture + `
[edge-types.uses]
from        = ["service"]
to          = ["service"]
cardinality = "many-to-many"
inverse     = "depended-on-by"
`,
			want: `edge-types "depends-on" and "uses" both declare inverse "depended-on-by"`,
		},
		{
			name: "a ref property's inverse collides",
			body: strings.Replace(hierarchyFixture, `description = "s"`, `description = "s"
properties = [{ name = "owner", type = "ref", to = "service", inverse = "depended-on-by" }]`, 1),
			want: `both declare inverse "depended-on-by"`,
		},
	}

	for _, testCase := range cases {
		_, loadErr := loadTOMLString(test, testCase.body)

		if loadErr == nil || !strings.Contains(loadErr.Error(), testCase.want) {
			test.Errorf("%s: err = %v, want it to contain %q", testCase.name, loadErr, testCase.want)
		}
	}
}

func TestInverseOf(test *testing.T) {
	loaded := loadTOMLFromString(test, hierarchyFixture)

	if forward, found := manifest.InverseOf(loaded.EdgeTypes, "depended-on-by"); !found || forward != "depends-on" {
		test.Errorf("InverseOf(depended-on-by) = %q, %v; want depends-on, true", forward, found)
	}

	for _, name := range []string{"depends-on", "has-part", "nope", ""} {
		if forward, found := manifest.InverseOf(loaded.EdgeTypes, name); found {
			test.Errorf("InverseOf(%q) = %q, want no inverse", name, forward)
		}
	}
}

func TestLoad_GraphClusterInheritsParentIsSource(test *testing.T) {
	cluster := `
[graph.cluster]
by   = "ancestor"
edge = "has-part"
`

	if inherited := loadTOMLFromString(test, hierarchyFixture+cluster).GraphCluster; !inherited.ParentIsSource {
		test.Errorf("[graph.cluster] on has-part: ParentIsSource = false, want it inherited from the edge type")
	}

	explicit := loadTOMLFromString(test, hierarchyFixture+cluster+"parent-is-source = false\n").GraphCluster

	if explicit.ParentIsSource {
		test.Errorf("[graph.cluster] parent-is-source = false was overridden by the edge type")
	}

	other := loadTOMLFromString(test, hierarchyFixture+strings.Replace(cluster, "has-part", "depends-on", 1)).GraphCluster

	if other.ParentIsSource {
		test.Errorf("[graph.cluster] on depends-on: ParentIsSource = true, want false")
	}
}

// TestSchemaFingerprint_IgnoresHierarchyWalkSettings pins that parent-is-source
// and max-depth, which only shape queries, neither change the fingerprint nor
// show up in an edge type's encoding when unset, so neither adding the keys
// to tusk nor setting them makes reindex re-process every file.
func TestSchemaFingerprint_IgnoresHierarchyWalkSettings(test *testing.T) {
	unset := strings.NewReplacer("parent-is-source = true\n", "", "max-depth        = 3\n", "").Replace(hierarchyFixture)

	if fingerprintOf(test, hierarchyFixture) != fingerprintOf(test, unset) {
		test.Errorf("SchemaFingerprint changed with parent-is-source / max-depth")
	}

	encoded, marshalErr := json.Marshal(manifest.EdgeType{From: []string{"*"}, To: []string{"*"}})

	if marshalErr != nil {
		test.Fatalf("marshal: %v", marshalErr)
	}

	if strings.Contains(string(encoded), "ParentIsSource") || strings.Contains(string(encoded), "MaxDepth") {
		test.Errorf("an edge type without the keys encodes them: %s", encoded)
	}
}
