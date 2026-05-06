package behavior_test

import (
	"github.com/germanamz/tusk/internal/behavior"
)

// fakePack is a minimal behavior.Instance for engine and registry tests.
// Tests configure Hooks, ReservedKeys, Name, and Kind directly.
type fakePack struct {
	name     string
	kind     string
	hooks    behavior.Hooks
	reserved []behavior.ReservedKey
}

func (pack *fakePack) Name() string                         { return pack.name }
func (pack *fakePack) Kind() string                         { return pack.kind }
func (pack *fakePack) Hooks() behavior.Hooks                { return pack.hooks }
func (pack *fakePack) ReservedKeys() []behavior.ReservedKey { return pack.reserved }
