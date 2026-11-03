package manifest

import (
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// namedBlock is a [<table>.<name>] declaration that doctor validates instead
// of the loader: Rule and Check. Its decode problems ride on the block.
type namedBlock[Block any] interface {
	*Block

	// setDecodeError records that the block does not decode: a value of the
	// wrong type, or `<table>.<name>` that is not a table.
	setDecodeError(message string)

	// setUnknownKeys records the keys inside the block that no field decodes.
	setUnknownKeys(keys []string)

	decodeFailed() bool
}

// decodeNamedBlocks decodes each [<table>.<name>] block on its own, so a value
// of the wrong type fails only that block instead of the whole manifest, and
// stamps each block with the keys the decode left undecoded under it (a nested
// table counts as one key), so a misspelled key is reported instead of
// silently ignored. A block that failed to decode gets no unknown keys: its
// valid keys are undecoded too. Nil when the table declares no blocks.
func decodeNamedBlocks[Block any, Pointer namedBlock[Block]](meta *toml.MetaData, table string, primitives map[string]toml.Primitive) map[string]Block {
	if len(primitives) == 0 {
		return nil
	}

	blocks := make(map[string]Block, len(primitives))

	for name, primitive := range primitives {
		var block Block

		if decodeErr := meta.PrimitiveDecode(primitive, &block); decodeErr != nil {
			block = *new(Block)
			Pointer(&block).setDecodeError(strings.TrimPrefix(decodeErr.Error(), "toml: "))
		}

		blocks[name] = block
	}

	unknownByBlock := map[string]map[string]struct{}{}

	for _, key := range meta.Undecoded() {
		if len(key) < 3 || key[0] != table {
			continue
		}

		if unknownByBlock[key[1]] == nil {
			unknownByBlock[key[1]] = map[string]struct{}{}
		}

		unknownByBlock[key[1]][key[2]] = struct{}{}
	}

	for name, unknown := range unknownByBlock {
		block, declared := blocks[name]

		if !declared || Pointer(&block).decodeFailed() {
			continue
		}

		keys := make([]string, 0, len(unknown))

		for key := range unknown {
			keys = append(keys, key)
		}

		sort.Strings(keys)
		Pointer(&block).setUnknownKeys(keys)
		blocks[name] = block
	}

	return blocks
}
