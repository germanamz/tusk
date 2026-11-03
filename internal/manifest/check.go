package manifest

import "github.com/BurntSushi/toml"

// Check is one [check.<name>] declaration: a project command doctor runs
// under `tusk doctor --checks` and reports with its own findings (#852). Load
// only decodes checks; doctor validates them, so a bad check is reported by
// doctor instead of failing the load.
type Check struct {
	// Description names the check in a finding that carries no location.
	Description string `toml:"description"`

	// Command runs through the shell from the workspace root.
	Command string `toml:"command"`

	// Severity is "error", "warning" or "advice"; empty means "error".
	Severity string `toml:"severity"`

	// TimeoutSeconds stops a command still running after this long; zero
	// means doctor's default.
	TimeoutSeconds int `toml:"timeout-seconds"`

	// UnknownKeys and DecodeError are set by Load, as on Rule.
	UnknownKeys []string `toml:"-"`
	DecodeError string   `toml:"-"`
}

// decodeChecks decodes each [check.<name>] block on its own (see
// decodeNamedBlocks). Returns an error only when the manifest does not parse.
func decodeChecks(body string, loaded *Manifest) error {
	var wrapper struct {
		Check map[string]toml.Primitive `toml:"check"`
	}

	meta, decodeErr := toml.Decode(body, &wrapper)

	if decodeErr != nil {
		return decodeErr
	}

	loaded.decodeMetas = append(loaded.decodeMetas, &meta)
	loaded.Checks = decodeNamedBlocks[Check](&meta, "check", wrapper.Check)

	return nil
}

func (check *Check) setDecodeError(message string) { check.DecodeError = message }

func (check *Check) setUnknownKeys(keys []string) { check.UnknownKeys = keys }

func (check *Check) decodeFailed() bool { return check.DecodeError != "" }
