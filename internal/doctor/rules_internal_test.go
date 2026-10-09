package doctor

import (
	"reflect"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
)

// TestRuleKinds_KeysAreDecodedRuleFields guards the kind registry: every
// kind's key must be a key manifest.Load decodes into manifest.Rule, or the
// kind could never be selected and its key would be reported as unknown.
func TestRuleKinds_KeysAreDecodedRuleFields(test *testing.T) {
	decoded := map[string]struct{}{}
	ruleType := reflect.TypeFor[manifest.Rule]()

	for position := range ruleType.NumField() {
		if tag := ruleType.Field(position).Tag.Get("toml"); tag != "" && tag != "-" {
			decoded[tag] = struct{}{}
		}
	}

	if len(ruleKinds) == 0 {
		test.Fatal("ruleKinds is empty")
	}

	for _, kind := range ruleKinds {
		if _, ok := decoded[kind.key]; !ok {
			test.Errorf("rule kind %q is not a toml key on manifest.Rule (decoded: %v)", kind.key, decoded)
		}
	}
}
