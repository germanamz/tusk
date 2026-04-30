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

func (server *Server) handleConfigShow(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	cfg, loadErr := config.Load(server.loadOpts...)

	if loadErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading config: %v", loadErr)), nil
	}

	raw, marshalErr := json.Marshal(cfg)

	if marshalErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshaling config: %v", marshalErr)), nil
	}

	return toolResultJSON(configShowResponse{
		ActiveFile: cfg.Sources.File,
		Effective:  raw,
	})
}

func (server *Server) handleConfigSet(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := server.checkBlocked("tusk_config_set", req); result != nil {
		return result, nil
	}

	key, keyErr := req.RequireString("key")

	if keyErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("missing key: %v", keyErr)), nil
	}

	value, valueErr := req.RequireString("value")

	if valueErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("missing value: %v", valueErr)), nil
	}

	if strings.HasPrefix(key, "storage.") {
		return mcp.NewToolResultError("refusing to modify storage.* keys via MCP; change the config file directly and restart the server"), nil
	}
	if strings.HasPrefix(key, "projects.") {
		return mcp.NewToolResultError("projects.* is managed by the database — use `tusk project modify` instead"), nil
	}
	if strings.HasPrefix(key, "workflows.") {
		return mcp.NewToolResultError("workflows.* is managed by the database — use `tusk workflow modify` instead"), nil
	}
	if !config.IsValidKey(key) {
		return mcp.NewToolResultError(fmt.Sprintf("unknown config key: %q", key)), nil
	}

	path, pathErr := config.ConfigFilePath(server.loadOpts...)

	if pathErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("resolving config file path: %v", pathErr)), nil
	}

	fileCfg, fileCfgErr := config.LoadFile(path)

	if fileCfgErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading config file: %v", fileCfgErr)), nil
	}

	data, tomlErr := toml.Marshal(fileCfg)

	if tomlErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshaling config: %v", tomlErr)), nil
	}

	viperCfg := viper.New()
	viperCfg.SetConfigType("toml")

	readErr := viperCfg.ReadConfig(bytes.NewReader(data))

	if readErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("reading config into viper: %v", readErr)), nil
	}

	var parsedValue any
	if config.IsSliceKey(key) {
		parsedValue = strings.Split(value, ",")
	} else {
		parsedValue = value
	}
	viperCfg.Set(key, parsedValue)

	var newCfg config.Config

	unmarshalErr := viperCfg.Unmarshal(&newCfg)

	if unmarshalErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("applying config change: %v", unmarshalErr)), nil
	}

	validateErr := newCfg.Validate()

	if validateErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid config: %v", validateErr)), nil
	}

	writeErr := config.WriteConfig(&newCfg, path)

	if writeErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("writing config: %v", writeErr)), nil
	}

	reloadErr := server.reloadConfig(ctx)

	if reloadErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("reloading config: %v", reloadErr)), nil
	}

	return toolResultJSON(map[string]any{
		"ok":          true,
		"key":         key,
		"active_file": path,
	})
}

// HandleConfigShowForTest exposes handleConfigShow for internal package tests.
func (server *Server) HandleConfigShowForTest(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return server.handleConfigShow(ctx, req)
}

// HandleConfigSetForTest exposes handleConfigSet for internal package tests.
func (server *Server) HandleConfigSetForTest(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return server.handleConfigSet(ctx, req)
}
