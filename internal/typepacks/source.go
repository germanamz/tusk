package typepacks

import (
	"context"
	"fmt"
	"strings"

	"github.com/germanamz/tusk/packs"
)

// LoadPack returns a pack's TOML. A built-in name (see packs.Names) reads the
// copy compiled into the binary, so it needs no network; anything containing
// "://" is fetched as a URL; any other name is an error listing the built-ins.
func LoadPack(ctx context.Context, source string) ([]byte, error) {
	if body, builtin := packs.Read(source); builtin {
		return body, nil
	}

	if strings.Contains(source, "://") {
		return Fetch(ctx, source)
	}

	return nil, fmt.Errorf("pack add: unknown pack name %q; supported names: %s — to install from a URL, pass the full URL", source, strings.Join(packs.Names(), ", "))
}
