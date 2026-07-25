// Package mcp exposes typed OMG application requests over bounded MCP stdio.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path/filepath"

	"github.com/jeremy-merchant/OMG/internal/app"
	"github.com/jeremy-merchant/OMG/internal/app/query"
)

const (
	maxFrameSize  = 1 << 20
	maxArgCount   = 256
	maxOutputSize = 1 << 20
	maxArgSize    = 32 << 10
	maxJSONDepth  = 64

	parseErrorCode     = -32700
	invalidRequestCode = -32600
	methodNotFoundCode = -32601
	invalidParamsCode  = -32602
	internalErrorCode  = -32603
)

var allowedCommands = []string{
	"human.create", "human.get", "session.create", "session.resume", "session.adopt", "session.import",
	"delegate.issue", "delegate.register", "delegate.revoke", "checkpoint.record",
	"task.create", "task.get", "task.claim", "task.transition", "task.run-create", "task.run-transition",
	"progress.add", "progress.history", "dependency.add", "dependency.list",
	"message.send", "message.inbox", "message.thread", "message.deliver", "message.read", "message.ack",
	"handoff.create", "handoff.show", "handoff.history", "handoff.supersede", "handoff.accept", "handoff.reject", "handoff.adopt",
	"reserve.add", "reserve.list", "reserve.active", "reserve.history", "reserve.renew", "reserve.release", "reserve.override",
	"git.inventory", "git.current", "git.latest", "git.history", "git.diff", "git.cleanup-plan", "git.adopt",
	"board.query", "preflight.query", "receipt.get", "receipt.list",
	"import.record",
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type toolArguments struct {
	Request app.Request `json:"request"`
}

// Serve reads newline-delimited JSON-RPC 2.0 requests and writes one response
// per non-notification request. It delegates typed, allowed requests once.
func Serve(ctx context.Context, input io.Reader, output io.Writer, version string, dispatcher app.Dispatcher) error {
	reader := bufio.NewReaderSize(input, 64<<10)
	frameBuffer := make([]byte, 0, maxFrameSize)

	for {
		frame, oversized, err := readFrame(reader, frameBuffer)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if oversized {
			if err := writeError(output, nil, parseErrorCode); err != nil {
				return err
			}
			continue
		}

		if !json.Valid(frame) {
			if err := writeError(output, nil, parseErrorCode); err != nil {
				return err
			}
			continue
		}
		var req request
		if err := decodeStrict(frame, &req); err != nil {
			if err := writeError(output, nil, invalidRequestCode); err != nil {
				return err
			}
			continue
		}
		if req.JSONRPC != "2.0" || req.Method == "" || !validID(req.ID) {
			if err := writeError(output, nil, invalidRequestCode); err != nil {
				return err
			}
			continue
		}
		if req.ID == nil {
			continue
		}

		if err := handleRequest(ctx, output, req, version, dispatcher); err != nil {
			return err
		}
	}
}

func readFrame(reader *bufio.Reader, frame []byte) ([]byte, bool, error) {
	frame = frame[:0]
	for {
		chunk, err := reader.ReadSlice('\n')
		hasNewline := len(chunk) > 0 && chunk[len(chunk)-1] == '\n'
		if hasNewline {
			chunk = chunk[:len(chunk)-1]
		}
		if len(frame)+len(chunk) > maxFrameSize {
			return drainOversizedFrame(reader, hasNewline, err)
		}
		frame = append(frame, chunk...)

		switch err {
		case nil:
			return frame, false, nil
		case bufio.ErrBufferFull:
			continue
		case io.EOF:
			if len(frame) == 0 {
				return nil, false, io.EOF
			}
			return frame, false, nil
		default:
			return nil, false, err
		}
	}
}

func drainOversizedFrame(reader *bufio.Reader, hasNewline bool, err error) ([]byte, bool, error) {
	for {
		if hasNewline || err == io.EOF {
			return nil, true, nil
		}
		if err != bufio.ErrBufferFull {
			return nil, false, err
		}

		chunk, readErr := reader.ReadSlice('\n')
		hasNewline = len(chunk) > 0 && chunk[len(chunk)-1] == '\n'
		err = readErr
	}
}

func handleRequest(ctx context.Context, output io.Writer, req request, version string, dispatcher app.Dispatcher) error {
	switch req.Method {
	case "initialize":
		if !isJSONObjectOrEmpty(req.Params) {
			return writeError(output, req.ID, invalidParamsCode)
		}
		return writeResult(output, req.ID, struct {
			ProtocolVersion string `json:"protocolVersion"`
			Capabilities    struct {
				Tools map[string]any `json:"tools"`
			} `json:"capabilities"`
			ServerInfo struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"serverInfo"`
		}{
			ProtocolVersion: "2024-11-05",
			Capabilities: struct {
				Tools map[string]any `json:"tools"`
			}{Tools: map[string]any{}},
			ServerInfo: struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			}{Name: "omg", Version: version},
		})
	case "tools/list":
		if !isEmptyObjectOrAbsent(req.Params) {
			return writeError(output, req.ID, invalidParamsCode)
		}
		return writeResult(output, req.ID, toolsListResult())
	case "tools/call":
		return handleToolCall(ctx, output, req, dispatcher)
	default:
		return writeError(output, req.ID, methodNotFoundCode)
	}
}

func handleToolCall(ctx context.Context, output io.Writer, req request, dispatcher app.Dispatcher) error {
	var params toolCallParams
	if len(req.Params) == 0 || decodeStrict(req.Params, &params) != nil || params.Name != "omg" || len(params.Arguments) == 0 {
		return writeError(output, req.ID, invalidParamsCode)
	}
	var arguments toolArguments
	if !validJSONDepth(params.Arguments) {
		return writeError(output, req.ID, invalidParamsCode)
	}
	if decodeStrict(params.Arguments, &arguments) != nil || !validRequest(arguments.Request) {
		return writeError(output, req.ID, invalidParamsCode)
	}
	if dispatcher == nil {
		return writeError(output, req.ID, internalErrorCode)
	}
	outcome, ok := callDispatcher(ctx, dispatcher, arguments.Request)
	if !ok {
		return writeError(output, req.ID, internalErrorCode)
	}
	result := struct {
		StructuredContent any  `json:"structuredContent"`
		IsError           bool `json:"isError,omitempty"`
	}{IsError: outcome.Error.Code != ""}
	if outcome.Error.Code != "" {
		result.StructuredContent = struct {
			OK    bool `json:"ok"`
			Error struct {
				Code      string `json:"code"`
				Message   string `json:"message"`
				Retryable bool   `json:"retryable"`
			} `json:"error"`
		}{OK: false, Error: struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			Retryable bool   `json:"retryable"`
		}{Code: string(outcome.Error.Code), Message: outcome.Error.Message, Retryable: outcome.Error.Retryable}}
	} else {
		result.StructuredContent = struct {
			OK   bool `json:"ok"`
			Data any  `json:"data"`
		}{OK: true, Data: outcome.Data}
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil || len(encoded) > maxOutputSize {
		return writeError(output, req.ID, internalErrorCode)
	}
	return writeResult(output, req.ID, result)
}

func callDispatcher(ctx context.Context, dispatcher app.Dispatcher, request app.Request) (outcome app.Outcome, ok bool) {
	ok = true
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	outcome = dispatcher.Dispatch(ctx, request)
	return outcome, ok
}

func validRequest(request app.Request) bool {
	if request.Version != app.RequestVersion || len(request.Payload) == 0 ||
		len(request.Payload) > maxFrameSize || !validExplicitPath(request.Project) ||
		(request.Workspace != "" && !validExplicitPath(request.Workspace)) ||
		(request.Store != "" && !validExplicitPath(request.Store)) ||
		len(request.IdempotencyKey) > maxArgSize {
		return false
	}
	if request.Command == "board.query" {
		var selector query.BoardRequest
		return decodeStrict(request.Payload, &selector) == nil
	}
	if request.Command == "preflight.query" {
		var selector app.PreflightRequest
		return request.IdempotencyKey == "" && decodeStrict(request.Payload, &selector) == nil
	}
	if request.Command == "receipt.get" {
		var payload app.ReceiptGet
		return request.IdempotencyKey == "" && decodeStrict(request.Payload, &payload) == nil && payload.ID != ""
	}
	if request.Command == "receipt.list" {
		return request.IdempotencyKey == "" && decodeStrict(request.Payload, &struct{}{}) == nil
	}
	for _, command := range allowedCommands {
		if request.Command == command {
			return true
		}
	}
	return false
}

func validExplicitPath(path string) bool {
	return path != "" && len(path) <= maxArgSize && filepath.IsAbs(path) && filepath.Clean(path) == path
}

func validID(id json.RawMessage) bool {
	if id == nil || bytes.Equal(id, []byte("null")) {
		return id == nil
	}
	var text string
	if id[0] == '"' {
		return json.Unmarshal(id, &text) == nil
	}
	var number json.Number
	return json.Unmarshal(id, &number) == nil
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return err
	}
	return nil
}

func validJSONDepth(data []byte) bool {
	depth := 0
	inString := false
	escaped := false
	for _, value := range data {
		if inString {
			if escaped {
				escaped = false
			} else if value == '\\' {
				escaped = true
			} else if value == '"' {
				inString = false
			}
			continue
		}
		switch value {
		case '"':
			inString = true
		case '{', '[':
			depth++
			if depth > maxJSONDepth {
				return false
			}
		case '}', ']':
			depth--
			if depth < 0 {
				return false
			}
		}
	}
	return !inString && depth == 0
}

func isJSONObjectOrEmpty(params json.RawMessage) bool {
	if len(params) == 0 || bytes.Equal(params, []byte("null")) {
		return true
	}
	var object map[string]json.RawMessage
	return json.Unmarshal(params, &object) == nil && object != nil
}

func isEmptyObjectOrAbsent(params json.RawMessage) bool {
	if len(params) == 0 || bytes.Equal(params, []byte("null")) {
		return true
	}
	var object map[string]json.RawMessage
	return json.Unmarshal(params, &object) == nil && len(object) == 0
}

func toolsListResult() struct {
	Tools []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		InputSchema any    `json:"inputSchema"`
	} `json:"tools"`
} {
	return struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			InputSchema any    `json:"inputSchema"`
		} `json:"tools"`
	}{
		Tools: []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			InputSchema any    `json:"inputSchema"`
		}{{
			Name:        "omg",
			Description: "Execute one typed OMG application request.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"request"},
				"properties": map[string]any{
					"request": map[string]any{
						"type": "object", "additionalProperties": false,
						"required": []string{"version", "command", "project", "payload"},
						"allOf": []any{
							map[string]any{
								"if": map[string]any{"properties": map[string]any{"command": map[string]any{"const": "board.query"}}},
								"then": map[string]any{"properties": map[string]any{
									"payload": map[string]any{
										"type": "object", "additionalProperties": false, "required": []string{"mode"},
										"properties": map[string]any{
											"mode":       map[string]any{"type": "string", "enum": []query.BoardMode{query.BoardMe, query.BoardTree, query.BoardTask, query.BoardAll, query.BoardGit}},
											"session_id": map[string]any{"type": "string", "maxLength": maxArgSize},
											"task_id":    map[string]any{"type": "string", "maxLength": maxArgSize},
										},
									},
								}},
							},
							map[string]any{
								"if": map[string]any{"properties": map[string]any{"command": map[string]any{"const": "preflight.query"}}},
								"then": map[string]any{"properties": map[string]any{
									"payload": map[string]any{
										"type": "object", "additionalProperties": false,
										"properties": map[string]any{
											"session_id": map[string]any{"type": "string", "maxLength": maxArgSize},
										},
									},
								}},
							},
						},
						"properties": map[string]any{
							"version":         map[string]any{"type": "integer", "const": app.RequestVersion},
							"command":         map[string]any{"type": "string", "enum": allowedCommands},
							"idempotency_key": map[string]any{"type": "string", "minLength": 1, "maxLength": maxArgSize},
							"project":         map[string]any{"type": "string", "minLength": 1, "maxLength": maxArgSize},
							"workspace":       map[string]any{"type": "string", "maxLength": maxArgSize},
							"store":           map[string]any{"type": "string", "maxLength": maxArgSize},
							"payload":         map[string]any{"type": "object"},
						},
					},
				},
			},
		}},
	}
}

func writeResult(output io.Writer, id json.RawMessage, result any) error {
	return json.NewEncoder(output).Encode(response{JSONRPC: "2.0", ID: id, Result: result})
}

func writeError(output io.Writer, id json.RawMessage, code int) error {
	return json.NewEncoder(output).Encode(response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: errorMessage(code)}})
}

func errorMessage(code int) string {
	switch code {
	case parseErrorCode:
		return "Parse error"
	case invalidRequestCode:
		return "Invalid request"
	case methodNotFoundCode:
		return "Method not found"
	case invalidParamsCode:
		return "Invalid params"
	default:
		return "Internal error"
	}
}
