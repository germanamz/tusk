package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// ConfigFilePath resolves the config file path using the same logic as
// Load: WithExplicitFile > walk-up from WithStartDir > global directory
// (WithSearchPath option > TUSK_CONFIG_DIR env > ~/.config/tusk) +
// "config.toml".
//
// When WithExplicitFile is set and the file is missing, ConfigFilePath
// returns a hard error — matching Load.
//
// When no explicit file is set and the global file does not yet exist,
// ConfigFilePath still returns the would-be path (globalDir/config.toml)
// so that callers like `tusk config init` can create it.
func ConfigFilePath(opts ...Option) (string, error) {
	var lo loadOptions
	for _, opt := range opts {
		opt(&lo)
	}

	if lo.explicitFile != "" {
		if _, statErr := os.Stat(lo.explicitFile); statErr != nil {
			if os.IsNotExist(statErr) {
				return "", fmt.Errorf("config file not found: %s", lo.explicitFile)
			}

			return "", fmt.Errorf("stat %s: %w", lo.explicitFile, statErr)
		}

		return lo.explicitFile, nil
	}

	if hit := walkUpForLocal(lo.startDir); hit != "" {
		return hit, nil
	}

	globalDir := resolveGlobalDir(lo.searchPath)
	if globalDir == "" {
		home, homeErr := os.UserHomeDir()

		if homeErr != nil {
			return "", fmt.Errorf("resolving home directory: %w", homeErr)
		}

		globalDir = filepath.Join(home, ".config", "tusk")
	}
	return filepath.Join(globalDir, "config.toml"), nil
}

// LoadFile parses a single TOML config file into a Config struct.
// Unlike Load(), this uses go-toml directly — no Viper, no env merging, no defaults.
// Used by config set (load-modify-write) and config validate (file-only validation).
func LoadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)

	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var cfg Config
	if unmarshalErr := toml.Unmarshal(data, &cfg); unmarshalErr != nil {
		return nil, fmt.Errorf("parsing config file: %w", unmarshalErr)
	}

	applyFileDefaults(&cfg)

	return &cfg, nil
}

// applyFileDefaults backfills sections with embedded defaults when the parsed
// file omits them. LoadFile bypasses viper and the embedded default.toml
// merge, so new config sections added in later releases would otherwise come
// back zero-valued and fail Validate. Keep this in sync with
// config/default.toml.
func applyFileDefaults(cfg *Config) {
	if cfg.Inline.MaxExpansionSize == 0 {
		cfg.Inline.MaxExpansionSize = 1 << 20 // 1 MB
	}
	if cfg.Notes.WindowSize == 0 {
		cfg.Notes.WindowSize = 20
	}
}

// WriteConfig marshals a Config struct to TOML and writes it to path atomically.
// Writes to a temporary file first, then renames to avoid partial writes.
func WriteConfig(cfg *Config, path string) error {
	data, marshalErr := toml.Marshal(cfg)

	if marshalErr != nil {
		return fmt.Errorf("marshaling config: %w", marshalErr)
	}

	dir := filepath.Dir(path)
	tmp, createErr := os.CreateTemp(dir, "tusk-config-*.toml")

	if createErr != nil {
		return fmt.Errorf("creating temp file: %w", createErr)
	}

	tmpPath := tmp.Name()

	if _, writeErr := tmp.Write(data); writeErr != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("writing temp file: %w", writeErr)
	}

	if closeErr := tmp.Close(); closeErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("closing temp file: %w", closeErr)
	}

	if renameErr := os.Rename(tmpPath, path); renameErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("renaming temp file: %w", renameErr)
	}

	return nil
}

// IsSliceKey checks whether a dot-path key corresponds to a slice field in the Config struct.
func IsSliceKey(key string) bool {
	if key == "" {
		return false
	}
	parts := strings.Split(key, ".")
	return isSliceKeyPath(reflect.TypeOf(Config{}), parts)
}

func isSliceKeyPath(reflectType reflect.Type, parts []string) bool {
	for reflectType.Kind() == reflect.Pointer {
		reflectType = reflectType.Elem()
	}

	if len(parts) == 0 {
		return false
	}

	switch reflectType.Kind() {
	case reflect.Struct:
		for index := 0; index < reflectType.NumField(); index++ {
			field := reflectType.Field(index)
			tag := field.Tag.Get("mapstructure")
			if tag == parts[0] {
				if len(parts) == 1 {
					fieldType := field.Type
					for fieldType.Kind() == reflect.Pointer {
						fieldType = fieldType.Elem()
					}
					return fieldType.Kind() == reflect.Slice
				}
				return isSliceKeyPath(field.Type, parts[1:])
			}
		}
		return false
	case reflect.Map:
		if len(parts) == 0 {
			return false
		}
		if len(parts) == 1 {
			elem := reflectType.Elem()
			for elem.Kind() == reflect.Pointer {
				elem = elem.Elem()
			}
			return elem.Kind() == reflect.Slice
		}
		return isSliceKeyPath(reflectType.Elem(), parts[1:])
	default:
		return false
	}
}

// IsValidKey checks whether a dot-path key corresponds to a leaf field in the Config struct.
// For map-keyed sections (workflows, projects), any map key is accepted.
func IsValidKey(key string) bool {
	if key == "" {
		return false
	}
	parts := strings.Split(key, ".")
	return isValidKeyPath(reflect.TypeOf(Config{}), parts)
}

// isValidKeyPath recursively walks the struct type tree to validate a dot-path.
func isValidKeyPath(reflectType reflect.Type, parts []string) bool {
	if len(parts) == 0 {
		return false
	}

	// Unwrap pointer types.
	for reflectType.Kind() == reflect.Pointer {
		reflectType = reflectType.Elem()
	}

	switch reflectType.Kind() {
	case reflect.Struct:
		// Find the field matching the mapstructure tag.
		for index := 0; index < reflectType.NumField(); index++ {
			field := reflectType.Field(index)
			tag := field.Tag.Get("mapstructure")
			if tag == parts[0] {
				if len(parts) == 1 {
					// Valid only if this is a leaf (not a struct or map, or is a slice/basic type).
					fieldType := field.Type
					for fieldType.Kind() == reflect.Pointer {
						fieldType = fieldType.Elem()
					}
					return fieldType.Kind() != reflect.Struct && fieldType.Kind() != reflect.Map
				}
				return isValidKeyPath(field.Type, parts[1:])
			}
		}
		return false

	case reflect.Map:
		// Map key can be anything (e.g., workflows.<anyname>).
		// Continue validating the value type with remaining parts.
		if len(parts) == 0 {
			return false
		}
		if len(parts) == 1 {
			elem := reflectType.Elem()
			for elem.Kind() == reflect.Pointer {
				elem = elem.Elem()
			}
			return elem.Kind() != reflect.Struct
		}
		return isValidKeyPath(reflectType.Elem(), parts[1:])

	default:
		// Leaf type — valid only if no more parts.
		return len(parts) == 0
	}
}
