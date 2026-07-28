// Command adapter-roundtrip exercises OMG's optional adapters against a disposable project.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error"`
}

func main() {
	binary := flag.String("omg", "omg", "path to the OMG executable")
	flag.Parse()
	if flag.NArg() != 0 {
		fatal(errors.New("adapter-roundtrip accepts only --omg <path>"))
	}
	absolute, err := exec.LookPath(*binary)
	if err != nil {
		fatal(fmt.Errorf("locate OMG executable: %w", err))
	}
	absolute, err = filepath.Abs(absolute)
	if err != nil {
		fatal(fmt.Errorf("resolve OMG executable: %w", err))
	}
	project, err := os.MkdirTemp("", "omg-adapter-roundtrip-")
	if err != nil {
		fatal(fmt.Errorf("create disposable project: %w", err))
	}
	defer os.RemoveAll(project)

	original := []byte("# Existing instructions\n\nKeep this content.\n")
	agentsPath := filepath.Join(project, "AGENTS.md")
	if err := os.WriteFile(agentsPath, original, 0o600); err != nil {
		fatal(fmt.Errorf("write fixture: %w", err))
	}

	steps := [][]string{
		{"init", "--project", project, "--json"},
		{"integration", "plan", "--project", project, "--json"},
		{"integration", "apply", "--project", project, "--json"},
		{"integration", "apply", "--project", project, "--json"},
		{"integration", "status", "--project", project, "--json"},
		{"shell-init", "bash"},
		{"completion", "powershell"},
		{"run", "--runtime", "roundtrip", "--", absolute, "version", "--json"},
		{"integration", "remove", "--project", project, "--json"},
	}
	for _, args := range steps {
		if _, err := run(absolute, nil, args...); err != nil {
			fatal(err)
		}
	}

	restored, err := os.ReadFile(agentsPath)
	if err != nil {
		fatal(fmt.Errorf("read restored instructions: %w", err))
	}
	if !bytes.Equal(restored, original) {
		fatal(errors.New("integration remove did not restore AGENTS.md byte-for-byte"))
	}
	if _, err := os.Stat(filepath.Join(project, "CLAUDE.md")); !errors.Is(err, os.ErrNotExist) {
		fatal(errors.New("integration remove did not delete the target it created"))
	}

	callFrame, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      "call",
		"method":  "tools/call",
		"params": map[string]any{
			"name": "omg",
			"arguments": map[string]any{
				"request": map[string]any{
					"version": 1,
					"command": "preflight.query",
					"project": project,
					"payload": map[string]any{},
				},
			},
		},
	})
	if err != nil {
		fatal(fmt.Errorf("encode MCP call: %w", err))
	}
	frames := strings.Join([]string{
		`{"jsonrpc":"2.0","id":"init","method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":"list","method":"tools/list","params":{}}`,
		string(callFrame),
	}, "\n") + "\n"
	mcpOutput, err := run(absolute, strings.NewReader(frames), "mcp", "serve", "--stdio")
	if err != nil {
		fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(mcpOutput)), "\n")
	if len(lines) != 3 {
		fatal(fmt.Errorf("MCP returned %d responses, want 3", len(lines)))
	}
	for index, line := range lines {
		var response rpcResponse
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			fatal(fmt.Errorf("decode MCP response %d: %w", index+1, err))
		}
		if response.JSONRPC != "2.0" || len(response.Error) != 0 || len(response.Result) == 0 {
			fatal(fmt.Errorf("invalid MCP response %d", index+1))
		}
	}

	summary := struct {
		OK                   bool `json:"ok"`
		Daemonless           bool `json:"daemonless"`
		IntegrationRoundTrip bool `json:"integration_round_trip"`
		MCPResponses         int  `json:"mcp_responses"`
		RuntimeWrapper       bool `json:"runtime_wrapper"`
	}{true, true, true, len(lines), true}
	encoded, err := json.Marshal(summary)
	if err != nil {
		fatal(err)
	}
	fmt.Println(string(encoded))
}

func run(binary string, input *strings.Reader, args ...string) ([]byte, error) {
	command := exec.Command(binary, args...)
	if input != nil {
		command.Stdin = input
	}
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s failed: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
