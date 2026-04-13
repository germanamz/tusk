package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/germanamz/tusk/config"
	"github.com/mark3labs/mcp-go/mcp"
	toml "github.com/pelletier/go-toml/v2"
	"github.com/spf13/viper"
)

// configShowResponse is the JSON payload returned by tusk_config_show.
type configShowResponse struct {
	ActiveFile string          `json:"active_file"`
	Effective  json.RawMessage `json:"effective"`
}

func (s *Server) handleConfigShow(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	cfg, err := config.Load(s.loadOpts...)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading config: %v", err)), nil
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshaling config: %v", err)), nil
	}
	return toolResultJSON(configShowResponse{
		ActiveFile: cfg.Sources.File,
		Effective:  raw,
	})
}

func (s *Server) handleConfigSet(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	key, err := req.RequireString("key")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("missing key: %v", err)), nil
	}
	value, err := req.RequireString("value")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("missing value: %v", err)), nil
	}

	if strings.HasPrefix(key, "storage.") {
		return mcp.NewToolResultError("refusing to modify storage.* keys via MCP; change the config file directly and restart the server"), nil
	}
	if !config.IsValidKey(key) {
		return mcp.NewToolResultError(fmt.Sprintf("unknown config key: %q", key)), nil
	}

	// Serialize the read-modify-write critical section plus the reload so
	// concurrent tusk_config_set calls cannot clobber each other or let a
	// reader observe partially-applied repo state. Cheap pre-validation
	// guards above run unlocked so error responses are not serialized.
	s.configMu.Lock()
	defer s.configMu.Unlock()

	path, err := config.ConfigFilePath(s.loadOpts...)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("resolving config file path: %v", err)), nil
	}

	fileCfg, err := config.LoadFile(path)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading config file: %v", err)), nil
	}

	data, err := toml.Marshal(fileCfg)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshaling config: %v", err)), nil
	}

	v := viper.New()
	v.SetConfigType("toml")
	if err := v.ReadConfig(bytes.NewReader(data)); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("reading config into viper: %v", err)), nil
	}

	var parsedValue any
	if config.IsSliceKey(key) {
		parsedValue = strings.Split(value, ",")
	} else {
		parsedValue = value
	}
	v.Set(key, parsedValue)

	var newCfg config.Config
	if err := v.Unmarshal(&newCfg); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("applying config change: %v", err)), nil
	}

	if err := newCfg.Validate(); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid config: %v", err)), nil
	}

	if err := config.WriteConfig(&newCfg, path); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("writing config: %v", err)), nil
	}

	// Already holding s.configMu — use the locked variant to avoid
	// self-deadlock.
	if err := s.reloadConfigLocked(ctx); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("reloading config: %v", err)), nil
	}

	return toolResultJSON(map[string]any{
		"ok":          true,
		"key":         key,
		"active_file": path,
	})
}

// HandleConfigShowForTest exposes handleConfigShow for internal package tests.
func (s *Server) HandleConfigShowForTest(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.handleConfigShow(ctx, req)
}

// HandleConfigSetForTest exposes handleConfigSet for internal package tests.
func (s *Server) HandleConfigSetForTest(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.handleConfigSet(ctx, req)
}
