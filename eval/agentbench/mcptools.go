package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"
)

const mcpHandshakeTimeout = 30 * time.Second

type rpcMessage struct {
	ID     *int            `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// ListMCPTools starts `tusk mcp` in vaultDir and asks it for its tool names
// over the stdio JSON-RPC handshake. The tusk arm disallows every tool but
// tusk_query by name, and reading the list from the binary under test keeps
// that working when tusk gains a tool.
func ListMCPTools(ctx context.Context, tuskBin string, vaultDir string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, mcpHandshakeTimeout)
	defer cancel()

	command := exec.CommandContext(ctx, tuskBin, "mcp")
	command.Dir = vaultDir
	command.Env = tuskServerEnv()

	stdin, stdinErr := command.StdinPipe()

	if stdinErr != nil {
		return nil, stdinErr
	}

	stdout, stdoutErr := command.StdoutPipe()

	if stdoutErr != nil {
		return nil, stdoutErr
	}

	if startErr := command.Start(); startErr != nil {
		return nil, fmt.Errorf("starting tusk mcp: %w", startErr)
	}

	defer func() {
		stdin.Close()
		_ = command.Wait()
	}()

	session := &rpcSession{stdin: stdin, reader: bufio.NewReader(stdout)}

	_, initErr := session.call(1, "initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "agentbench", "version": "1"},
	})

	if initErr != nil {
		return nil, fmt.Errorf("tusk mcp initialize: %w", initErr)
	}

	if notifyErr := session.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); notifyErr != nil {
		return nil, notifyErr
	}

	var names []string

	cursor := ""

	for page := 2; ; page++ {
		params := map[string]any{}

		if cursor != "" {
			params["cursor"] = cursor
		}

		result, listErr := session.call(page, "tools/list", params)

		if listErr != nil {
			return nil, fmt.Errorf("tusk mcp tools/list: %w", listErr)
		}

		var listing struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}

		if decodeErr := json.Unmarshal(result, &listing); decodeErr != nil {
			return nil, fmt.Errorf("decoding tools/list: %w", decodeErr)
		}

		for _, tool := range listing.Tools {
			names = append(names, tool.Name)
		}

		if listing.NextCursor == "" {
			break
		}

		cursor = listing.NextCursor
	}

	if len(names) == 0 {
		return nil, errors.New("tusk mcp offered no tools")
	}

	return names, nil
}

type rpcSession struct {
	stdin  io.Writer
	reader *bufio.Reader
}

func (session *rpcSession) send(message any) error {
	encoded, encodeErr := json.Marshal(message)

	if encodeErr != nil {
		return encodeErr
	}

	_, writeErr := session.stdin.Write(append(encoded, '\n'))

	return writeErr
}

// call sends a request and reads lines until the response with its id,
// skipping notifications and log lines the server interleaves.
func (session *rpcSession) call(id int, method string, params any) (json.RawMessage, error) {
	sendErr := session.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})

	if sendErr != nil {
		return nil, sendErr
	}

	for {
		line, readErr := session.reader.ReadBytes('\n')

		if readErr != nil {
			return nil, fmt.Errorf("reading response: %w", readErr)
		}

		var message rpcMessage

		if json.Unmarshal(line, &message) != nil || message.ID == nil || *message.ID != id {
			continue
		}

		if message.Error != nil {
			return nil, errors.New(message.Error.Message)
		}

		return message.Result, nil
	}
}
