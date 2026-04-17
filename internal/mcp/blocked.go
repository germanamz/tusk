package mcp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// checkBlocked returns a tool-result error when the request supplies any
// field listed in s.cfg.BlockedFields[toolName]. Absent or nil values
// pass. Returns nil when nothing is blocked.
//
// Field presence is determined from req.GetArguments(): a key present in
// the arguments map with a non-nil value is considered "supplied".
func (s *Server) checkBlocked(toolName string, req mcp.CallToolRequest) *mcp.CallToolResult {
	s.cfgMu.RLock()
	blocked := s.cfg.BlockedFields[toolName]
	s.cfgMu.RUnlock()
	if len(blocked) == 0 {
		return nil
	}
	args := req.GetArguments()
	if len(args) == 0 {
		return nil
	}
	var hit []string
	for _, field := range blocked {
		v, ok := args[field]
		if !ok || v == nil {
			continue
		}
		hit = append(hit, field)
	}
	if len(hit) == 0 {
		return nil
	}
	sort.Strings(hit)
	return mcp.NewToolResultError(fmt.Sprintf(
		"fields [%s] are blocked by mcp.blocked_fields.%s",
		strings.Join(hit, ", "), toolName,
	))
}
