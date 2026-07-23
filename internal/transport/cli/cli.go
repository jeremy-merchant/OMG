// Package cli decodes CLI arguments and serializes stable OMG envelopes.
package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"example.invalid/coordledger/internal/app"
	"example.invalid/coordledger/internal/app/foundation"
	"example.invalid/coordledger/internal/app/query"
	"example.invalid/coordledger/internal/domain"
)

const (
	EnvelopeSchemaVersion = 1
	CommandSchemaVersion  = 1
	ExitSuccess           = 0
	ExitUsage             = 2
	ExitNotFound          = 3
	ExitUnavailable       = 4
	ExitConflict          = 5
	ExitTemporary         = 6
	ExitInternal          = 70
)

type Metadata struct {
	SchemaVersion  int `json:"schema_version"`
	CommandVersion int `json:"command_version"`
}
type SuccessEnvelope struct {
	OK       bool     `json:"ok"`
	Data     any      `json:"data"`
	Meta     Metadata `json:"meta"`
	Warnings []string `json:"warnings"`
}
type ErrorMetadata struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	ExitCode  int    `json:"exit_code"`
}
type ErrorEnvelope struct {
	OK       bool          `json:"ok"`
	Error    ErrorMetadata `json:"error"`
	Meta     Metadata      `json:"meta"`
	Warnings []string      `json:"warnings"`
}
type Request struct {
	Name                   string
	Subcommand             string
	JSON                   bool
	Integrity              bool
	Status                 bool
	Stdio                  bool
	Runtime                string
	Command                []string
	Project                string
	Workspace              string
	Store                  string
	projectProvided        bool
	workspaceProvided      bool
	storeProvided          bool
	outputProvided         bool
	planFileProvided       bool
	approvalFileProvided   bool
	idempotencyKeyProvided bool
	formatProvided         bool
	sessionProvided        bool
	taskProvided           bool
	runtimeProvided        bool
	Output                 string
	PlanFile               string
	ApprovalFile           string
	IdempotencyKey         string
	Format                 string
	SessionID              string
	TaskID                 string
	Payload                string
	PayloadProvided        bool
	PayloadFile            string
	PayloadFileProvided    bool
	PayloadStdin           bool
}

type taskCreatePayload struct {
	Title              string `json:"title"`
	CreatedBySessionID string `json:"created_by_session_id"`
	ParentTaskID       string `json:"parent_task_id,omitempty"`
}

type recipientPayload struct {
	SessionID string `json:"session_id,omitempty"`
	HumanID   string `json:"human_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	Role      string `json:"role,omitempty"`
}

type messageSendPayload struct {
	ID              string             `json:"id"`
	Type            string             `json:"type"`
	ThreadID        string             `json:"thread_id"`
	SenderSessionID string             `json:"sender_session_id"`
	Recipients      []recipientPayload `json:"recipients"`
	Subject         string             `json:"subject,omitempty"`
	Body            string             `json:"body"`
	RelatedTaskID   string             `json:"related_task_id,omitempty"`
}

type evidencePayload struct {
	Summary string `json:"summary"`
	Hash    string `json:"hash"`
}

type handoffCreatePayload struct {
	ID                   string            `json:"id"`
	TaskID               string            `json:"task_id"`
	RunID                string            `json:"run_id"`
	SourceSessionID      string            `json:"source_session_id"`
	TargetSessionID      string            `json:"target_session_id,omitempty"`
	TargetTaskID         string            `json:"target_task_id,omitempty"`
	Summary              string            `json:"summary"`
	FinalOutputPolicy    string            `json:"final_output_policy"`
	FinalOutputText      string            `json:"final_output_text,omitempty"`
	FinalOutputHash      string            `json:"final_output_hash,omitempty"`
	ChangedFiles         []string          `json:"changed_files,omitempty"`
	Commits              []string          `json:"commits,omitempty"`
	VerificationEvidence []evidencePayload `json:"verification_evidence,omitempty"`
	RemainingRisks       []string          `json:"remaining_risks,omitempty"`
	SuggestedActions     []string          `json:"suggested_actions,omitempty"`
}

func Decode(args []string) (Request, domain.DomainError) {
	if len(args) == 0 {
		return Request{}, invalid("a command is required")
	}
	request := Request{Name: args[0]}
	i := 1
	if commandTakesSubcommand(request.Name) && i < len(args) && len(args[i]) > 0 && args[i][0] != '-' {
		request.Subcommand = args[i]
		i++
	}
	for i < len(args) {
		arg := args[i]
		if arg == "--" {
			request.Command = append([]string(nil), args[i+1:]...)
			break
		}
		switch {
		case arg == "--json":
			request.JSON = true
		case arg == "--integrity":
			request.Integrity = true
		case arg == "--status":
			request.Status = true
		case arg == "--stdio":
			request.Stdio = true
		case arg == "--payload-stdin":
			request.PayloadStdin = true
		case optionTakesValue(arg):
			if i+1 >= len(args) {
				return Request{}, invalid("a command option requires a value")
			}
			i++
			value := args[i]
			switch arg {
			case "--project":
				request.Project = value
				request.projectProvided = true
			case "--workspace":
				request.Workspace = value
				request.workspaceProvided = true
			case "--store":
				request.Store = value
				request.storeProvided = true
			case "--output":
				request.Output = value
				request.outputProvided = true
			case "--plan-file":
				request.PlanFile = value
				request.planFileProvided = true
			case "--approval-file":
				request.ApprovalFile = value
				request.approvalFileProvided = true
			case "--idempotency-key":
				request.IdempotencyKey = value
				request.idempotencyKeyProvided = true
			case "--format":
				request.Format = value
				request.formatProvided = true
			case "--session":
				request.SessionID = value
				request.sessionProvided = true
			case "--task":
				request.TaskID = value
				request.taskProvided = true
			case "--runtime":
				request.Runtime = value
				request.runtimeProvided = true
			case "--payload":
				request.PayloadProvided = true
				request.Payload = value
			case "--payload-file":
				request.PayloadFileProvided = true
				request.PayloadFile = value
			}
		default:
			return Request{}, invalid("unsupported command argument")
		}
		i++
	}
	return request, domain.DomainError{}
}

func commandTakesSubcommand(name string) bool {
	switch name {
	case "migration", "backup", "release", "board", "export", "integration",
		"shell-init", "completion", "watch", "human", "session", "delegate", "checkpoint",
		"task", "progress", "dependency", "message", "handoff", "reserve", "git", "import", "receipt", "mcp":
		return true
	default:
		return false
	}
}

func optionTakesValue(arg string) bool {
	switch arg {
	case "--project", "--workspace", "--store", "--output", "--plan-file", "--approval-file", "--idempotency-key", "--format", "--session", "--task", "--runtime", "--payload", "--payload-file":
		return true
	default:
		return false
	}
}

// RunWithApplication executes the CLI against dependencies composed at the
// application boundary.
func RunWithApplication(ctx context.Context, args []string, version string, stdin io.Reader, stdout, stderr io.Writer, application app.CLIService) int {
	return runWithContext(ctx, args, version, stdout, application, stdin, stderr)
}

func runWithContext(ctx context.Context, args []string, version string, output io.Writer, application app.CLIService, stdin io.Reader, stderr io.Writer) int {
	if ctx == nil || application.Foundation == nil || application.Dispatcher == nil || stdin == nil || output == nil || stderr == nil {
		return writeError(output, hasJSON(args), invalid("command context is invalid"))
	}
	if target, requested := parseHelpTarget(args); requested {
		jsonOutput := hasJSON(args)
		width := cliTerminalWidth(output)
		color := !jsonOutput && cliTerminalColorEnabled(output)
		if jsonOutput {
			width = defaultTerminalWidth
		}
		usage, found := renderHelp(version, color, width, target)
		if !found {
			message, hint, next := unknownHelpMessage(target)
			return writeErrorWithContext(output, jsonOutput, invalid(message), terminalErrorContext{Hint: hint, Next: next})
		}
		if jsonOutput {
			return writeSuccess(output, true, struct {
				Version string `json:"version"`
				Usage   string `json:"usage"`
			}{Version: version, Usage: stripTerminalANSI(usage)})
		}
		if _, err := io.WriteString(output, usage); err != nil {
			return ExitInternal
		}
		return ExitSuccess
	}
	request, err := Decode(args)
	if err.Code != "" {
		return writeError(output, hasJSON(args), err)
	}
	if !request.JSON && applicationCommandName(request.Name) {
		if message, context, invalidPath := commandPathProblem(request.Name, request.Subcommand); invalidPath {
			return writeErrorWithContext(output, false, domain.NewError(domain.CodeCommandNotWired, message, false), context)
		}
	}
	if (request.PayloadProvided || request.PayloadFileProvided || request.PayloadStdin) && !applicationCommandName(request.Name) &&
		!(request.Name == "backup" && request.Subcommand == "restore") {
		return writeError(output, request.JSON, invalid("payload transport is invalid for this command"))
	}
	selection := foundation.Selection{Project: request.Project, Workspace: request.Workspace, Store: request.Store}
	switch request.Name {
	case "version":
		if request.Subcommand != "" {
			break
		}
		return writeSuccess(output, request.JSON, struct {
			Version string `json:"version"`
		}{version})
	case "release":
		if request.Subcommand == "status" {
			return writeSuccess(output, request.JSON, struct {
				Status string `json:"status"`
			}{"NOT PUBLISHED"})
		}
	case "init":
		if request.Subcommand == "" {
			result, commandErr := application.Foundation.Init(ctx, selection)
			return statusResult(output, request, result, commandErr)
		}
	case "doctor":
		if request.Subcommand == "" {
			result, commandErr := application.Foundation.Status(ctx, selection, request.Integrity)
			return statusResult(output, request, result, commandErr)
		}
	case "preflight":
		return runPreflight(ctx, output, request, application, selection)
	case "migration":
		if request.Subcommand == "plan" {
			return runPlan(output, request, application.Foundation, selection, ctx)
		}
		if request.Subcommand == "apply" {
			return runApply(output, request, application.Foundation, selection, ctx)
		}
	case "backup":
		if request.Subcommand == "create" {
			return runBackup(output, request, application.Foundation, selection, ctx)
		}
		if request.Subcommand == "restore" {
			return runRestorePlan(ctx, output, stdin, request, application.Foundation, selection)
		}
	case "board":
		return runBoard(output, request, application, selection, ctx)
	case "export":
		return runExport(output, request, application, selection, ctx)
	case "integration":
		return runIntegration(output, request, application, selection, ctx)
	case "run":
		return runRuntime(ctx, output, stderr, stdin, request, application)
	case "shell-init", "completion":
		return runShell(output, request, application)
	case "watch":
		return runWatch(ctx, output, request, application, selection)
	case "human", "session", "delegate", "checkpoint", "task", "progress", "dependency", "message", "handoff", "reserve", "git", "import", "receipt":
		return runApplicationCommand(ctx, output, stdin, request, application, selection)
	case "mcp":
		return runMCP(ctx, stdin, output, request, version, application)
	}
	if !knownCommand(request.Name) {
		suggestion := closestCommand(request.Name)
		context := terminalErrorContext{Next: "omg --help"}
		if suggestion != "" {
			context.Hint = fmt.Sprintf("Did you mean %q?", suggestion)
			context.Next = "omg " + suggestion + " --help"
		}
		return writeErrorWithContext(output, request.JSON, domain.NewError(domain.CodeCommandNotWired, fmt.Sprintf("unknown command %q", request.Name), false), context)
	}
	return writeErrorWithContext(output, request.JSON, domain.NewError(domain.CodeCommandNotWired, "command is not supported", false), terminalErrorContext{Next: "omg " + request.Name + " --help"})
}
func runMCP(ctx context.Context, input io.Reader, output io.Writer, request Request, version string, application app.CLIService) int {
	if request.Subcommand != "serve" || !request.Stdio || request.JSON || request.Integrity || request.Status ||
		request.Runtime != "" || len(request.Command) != 0 || request.Project != "" || request.Workspace != "" ||
		request.Store != "" || request.Output != "" || request.PlanFile != "" || request.ApprovalFile != "" ||
		request.IdempotencyKey != "" || request.Format != "" || request.SessionID != "" || request.TaskID != "" ||
		request.PayloadProvided || request.PayloadFileProvided || request.PayloadStdin {
		return writeInvalidRequest(output, request, "MCP request is invalid")
	}
	err := application.MCPServe(ctx, input, output, version, application.Dispatcher)
	if err != nil {
		return ExitInternal
	}
	return ExitSuccess
}

func runApplicationCommand(ctx context.Context, output io.Writer, input io.Reader, request Request, application app.CLIService, selection foundation.Selection) int {
	if len(request.Command) != 0 || request.Output != "" || request.PlanFile != "" ||
		request.ApprovalFile != "" || request.Format != "" || request.SessionID != "" || request.TaskID != "" ||
		request.Runtime != "" || request.Integrity || request.Status || request.Stdio {
		return writeInvalidRequest(output, request, "application request is invalid")
	}
	payload, err := loadApplicationPayload(request, input)
	if err != nil {
		return writeError(output, request.JSON, invalid("application payload transport is invalid"))
	}
	subcommand := request.Subcommand
	if request.Name == "checkpoint" && subcommand == "" {
		subcommand = "record"
	}
	command := request.Name + "." + subcommand
	outcome := application.Dispatcher.Dispatch(ctx, app.Request{
		Version: app.RequestVersion, Command: command, Project: selection.Project, Workspace: selection.Workspace,
		Store: selection.Store, IdempotencyKey: request.IdempotencyKey, Payload: json.RawMessage(payload),
	})
	if outcome.Error.Code != "" {
		return writeError(output, request.JSON, outcome.Error)
	}
	return writeSuccess(output, request.JSON, outcome.Data)
}

func applicationCommandName(name string) bool {
	switch name {
	case "human", "session", "delegate", "checkpoint", "task", "progress", "dependency", "message", "handoff", "reserve", "git", "import", "receipt":
		return true
	default:
		return false
	}
}

func decodePayload(data string, target any) bool {
	if len(data) > 1<<20 {
		return false
	}
	decoder := json.NewDecoder(bytes.NewBufferString(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target) == nil && decoder.Decode(&struct{}{}) == io.EOF
}

func runBoard(output io.Writer, request Request, application app.CLIService, selection foundation.Selection, ctx context.Context) int {
	if !validBoardRequest(request) {
		return writeInvalidRequest(output, request, "board request is invalid")
	}
	format, _ := boardFormat(request.Format)
	model, err := loadBoard(ctx, application.Dispatcher, selection, query.BoardRequest{
		Mode:      query.BoardMode(request.Subcommand),
		SessionID: request.SessionID,
		TaskID:    request.TaskID,
	})
	if err.Code != "" {
		return writeError(output, request.JSON, err)
	}
	if request.JSON {
		return writeSuccess(output, true, model)
	}
	if renderErr := application.RenderBoard(format, model, output); renderErr != nil {
		return writeError(output, false, domain.NewError(domain.CodeInternal, "unable to render board", false))
	}
	return ExitSuccess
}

func validBoardRequest(request Request) bool {
	if request.JSON && request.formatProvided {
		return false
	}
	if request.formatProvided && request.Format == "" {
		return false
	}
	if _, ok := boardFormat(request.Format); !ok {
		return false
	}
	if request.Integrity || request.Status || request.Stdio || request.runtimeProvided || request.Runtime != "" ||
		len(request.Command) != 0 || request.outputProvided || request.Output != "" ||
		request.planFileProvided || request.PlanFile != "" || request.approvalFileProvided ||
		request.ApprovalFile != "" || request.idempotencyKeyProvided || request.IdempotencyKey != "" ||
		request.PayloadProvided || request.Payload != "" || request.PayloadFileProvided ||
		request.PayloadFile != "" || request.PayloadStdin {
		return false
	}
	switch query.BoardMode(request.Subcommand) {
	case query.BoardMe:
		if !request.sessionProvided || request.taskProvided {
			return false
		}
	case query.BoardTask:
		if !request.taskProvided || request.sessionProvided {
			return false
		}
	case query.BoardTree, query.BoardAll, query.BoardGit:
		if request.sessionProvided || request.taskProvided {
			return false
		}
	}
	return query.BoardRequest{
		Mode:      query.BoardMode(request.Subcommand),
		SessionID: request.SessionID,
		TaskID:    request.TaskID,
	}.Validate() == nil
}

func validExportRequest(request Request) bool {
	if request.Integrity || request.Status || request.Stdio || request.runtimeProvided || request.Runtime != "" ||
		len(request.Command) != 0 || request.formatProvided || request.Format != "" ||
		request.sessionProvided || request.SessionID != "" || request.taskProvided || request.TaskID != "" ||
		request.planFileProvided || request.PlanFile != "" || request.approvalFileProvided ||
		request.ApprovalFile != "" || request.idempotencyKeyProvided || request.IdempotencyKey != "" ||
		request.PayloadProvided || request.Payload != "" || request.PayloadFileProvided ||
		request.PayloadFile != "" || request.PayloadStdin {
		return false
	}
	if request.Subcommand == "" {
		return request.JSON && !request.outputProvided && request.Output == ""
	}
	if request.JSON || !request.outputProvided || request.Output == "" {
		return false
	}
	_, ok := boardFormat(request.Subcommand)
	return ok
}

func runExport(output io.Writer, request Request, application app.CLIService, selection foundation.Selection, ctx context.Context) int {
	if !validExportRequest(request) {
		return writeInvalidRequest(output, request, "export request is invalid")
	}
	model, err := loadBoard(ctx, application.Dispatcher, selection, query.BoardRequest{Mode: query.BoardAll})
	if err.Code != "" {
		return writeError(output, request.JSON, err)
	}
	if request.Subcommand == "" {
		if !request.JSON || request.Output != "" || request.Format != "" {
			return writeError(output, request.JSON, invalid("export format is required"))
		}
		var board query.BoardSnapshot
		if decodeErr := json.Unmarshal(model.Data(), &board); decodeErr != nil {
			return writeError(output, true, domain.NewError(domain.CodeInternal, "unable to render export", false))
		}
		return writeSuccess(output, true, query.ReportSnapshot{SchemaVersion: query.BoardSchemaVersion, Board: board})
	}
	if request.JSON || request.Output == "" || request.Format != "" {
		return writeError(output, request.JSON, invalid("export output is invalid"))
	}
	format, ok := boardFormat(request.Subcommand)
	if !ok {
		return writeError(output, false, invalid("export format is invalid"))
	}
	var rendered bytes.Buffer
	if renderErr := application.RenderBoard(format, model, &rendered); renderErr != nil {
		return writeError(output, false, domain.NewError(domain.CodeInternal, "unable to render export", false))
	}
	if writeErr := writeNewExport(request.Output, rendered.Bytes()); writeErr != nil {
		return writeError(output, false, domain.NewError(domain.CodeUnavailable, "unable to write export", false))
	}
	sum := sha256.Sum256(rendered.Bytes())
	return writeSuccess(output, false, struct {
		Format   string `json:"format"`
		Checksum string `json:"checksum"`
	}{string(format), hex.EncodeToString(sum[:])})
}

func loadBoard(ctx context.Context, dispatcher app.Dispatcher, selection foundation.Selection, request query.BoardRequest) (query.ViewModel, domain.DomainError) {
	payload, err := json.Marshal(request)
	if err != nil {
		return query.ViewModel{}, domain.NewError(domain.CodeInternal, "unable to encode board query", false)
	}
	outcome := dispatcher.Dispatch(ctx, app.Request{
		Version:   app.RequestVersion,
		Command:   "board.query",
		Project:   selection.Project,
		Workspace: selection.Workspace,
		Store:     selection.Store,
		Payload:   payload,
	})
	if outcome.Error.Code != "" {
		return query.ViewModel{}, outcome.Error
	}
	model, ok := outcome.Data.(query.ViewModel)
	if !ok {
		return query.ViewModel{}, domain.NewError(domain.CodeInternal, "board query returned an invalid model", false)
	}
	return model, domain.DomainError{}
}

func boardFormat(value string) (string, bool) {
	if value == "" {
		return "tty", true
	}
	switch value {
	case "tty", "json", "markdown", "html":
		return value, true
	default:
		return "", false
	}
}

func writeNewExport(path string, data []byte) error {
	if path == "" {
		return errors.New("empty export path")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	absolute = filepath.Clean(absolute)
	file, err := createNewPrivateExportFile(absolute)
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		_ = file.Close()
		if !complete {
			_ = os.Remove(absolute)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	complete = true
	return nil
}

func runShell(output io.Writer, request Request, application app.CLIService) int {
	if request.Subcommand == "" || len(request.Command) != 0 ||
		request.Project != "" || request.Workspace != "" || request.Store != "" ||
		request.Output != "" || request.PlanFile != "" || request.ApprovalFile != "" ||
		request.IdempotencyKey != "" || request.Format != "" || request.SessionID != "" ||
		request.TaskID != "" || request.Runtime != "" || request.Integrity || request.Status {
		return writeInvalidRequest(output, request, "shell request is invalid")
	}
	result, err := application.Shell(request.Name, request.Subcommand)
	if err != nil {
		return writeError(output, request.JSON, invalid("shell is unsupported"))
	}
	if request.JSON {
		return writeSuccess(output, true, result.Content)
	}
	if _, err := io.WriteString(output, result.Text); err != nil {
		return ExitInternal
	}
	return ExitSuccess
}

func runWatch(ctx context.Context, output io.Writer, request Request, application app.CLIService, selection foundation.Selection) int {
	statusOnly := request.Subcommand == "status" || (request.Subcommand == "" && request.Status)
	if (request.Subcommand != "" && request.Subcommand != "status") ||
		(request.Subcommand == "status" && request.Status) || len(request.Command) != 0 ||
		request.Output != "" || request.PlanFile != "" || request.ApprovalFile != "" ||
		request.IdempotencyKey != "" || request.Format != "" || request.SessionID != "" ||
		request.TaskID != "" || request.Runtime != "" || request.Integrity {
		return writeInvalidRequest(output, request, "watch request is invalid")
	}
	result, err := application.Watch(ctx, app.CLIWatchRequest{
		Project: selection.Project, Workspace: selection.Workspace, Store: selection.Store, Status: statusOnly,
		Refresh: func(callbackContext context.Context) domain.DomainError {
			_, callbackErr := loadBoard(callbackContext, application.Dispatcher, selection, query.BoardRequest{Mode: query.BoardAll})
			return callbackErr
		},
	})
	if err.Code != "" {
		return writeError(output, request.JSON, err)
	}
	if statusOnly {
		return writeSuccess(output, request.JSON, result.Status)
	}
	if result.Stopped {
		return writeSuccess(output, request.JSON, result.Result)
	}
	if result.Conflict {
		return writeError(output, request.JSON, domain.NewError(domain.CodeConflict, "watch is already active", true))
	}
	return writeError(output, request.JSON, domain.NewError(domain.CodeUnavailable, "watch failed", false))
}

func runRuntime(ctx context.Context, output, stderr io.Writer, stdin io.Reader, request Request, application app.CLIService) int {
	if request.JSON || request.Runtime == "" || len(request.Command) == 0 ||
		request.Project != "" || request.Workspace != "" || request.Store != "" ||
		request.Output != "" || request.PlanFile != "" || request.ApprovalFile != "" ||
		request.IdempotencyKey != "" || request.Format != "" || request.SessionID != "" ||
		request.TaskID != "" || request.Integrity || request.Status {
		return writeError(output, request.JSON, invalid("runtime invocation is invalid"))
	}
	result, err := application.Runtime(ctx, app.CLIRuntimeRequest{Runtime: request.Runtime, Argv: request.Command, Stdin: stdin, Stdout: output, Stderr: stderr})
	if err == nil {
		writeRunResult(output, result)
		return ExitSuccess
	}
	if result.Exited {
		writeRunResult(output, result)
		if result.ExitCode >= 0 {
			return result.ExitCode
		}
	}
	switch {
	case result.Invalid:
		return writeError(output, false, invalid("runtime invocation is invalid"))
	case result.NotFound:
		return writeError(output, false, domain.NewError(domain.CodeNotFound, "runtime executable was not found", false))
	case result.Cancelled:
		return writeError(output, false, domain.NewError(domain.CodeUnavailable, "runtime invocation was cancelled", true))
	default:
		return writeError(output, false, domain.NewError(domain.CodeUnavailable, "runtime invocation failed", false))
	}
}

func writeRunResult(output io.Writer, result app.CLIRuntimeResult) {
	renderRuntimeResult(output, result)
}

func runIntegration(output io.Writer, request Request, application app.CLIService, selection foundation.Selection, ctx context.Context) int {
	if request.workspaceProvided || request.storeProvided || request.Workspace != "" || request.Store != "" {
		return writeError(output, request.JSON, invalid("integration request does not support --workspace or --store"))
	}
	if !validIntegrationRequest(request) {
		return writeInvalidRequest(output, request, "integration request is invalid")
	}
	result, err := application.Integration(ctx, app.CLIIntegrationRequest{
		Project: selection.Project, Subcommand: request.Subcommand, Status: request.Status,
	})
	if err.Code != "" {
		return writeError(output, request.JSON, err)
	}
	return writeSuccess(output, request.JSON, result)
}

func validIntegrationRequest(request Request) bool {
	if !request.projectProvided || request.Project == "" {
		return false
	}
	switch request.Subcommand {
	case "status", "plan", "apply", "remove":
	default:
		return false
	}
	if request.Subcommand != "remove" && request.Status {
		return false
	}
	return !request.Integrity && !request.Stdio && !request.runtimeProvided && request.Runtime == "" &&
		len(request.Command) == 0 && !request.outputProvided && request.Output == "" &&
		!request.planFileProvided && request.PlanFile == "" && !request.approvalFileProvided &&
		request.ApprovalFile == "" && !request.idempotencyKeyProvided && request.IdempotencyKey == "" &&
		!request.formatProvided && request.Format == "" && !request.sessionProvided && request.SessionID == "" &&
		!request.taskProvided && request.TaskID == "" && !request.PayloadProvided && request.Payload == "" &&
		!request.PayloadFileProvided && request.PayloadFile == "" && !request.PayloadStdin
}

func runPlan(output io.Writer, request Request, service app.Foundation, selection foundation.Selection, ctx context.Context) int {
	plan, err := service.Plan(ctx, selection)
	if err.Code != "" {
		return writeError(output, request.JSON, err)
	}
	if request.Output != "" {
		data, marshalErr := json.Marshal(plan)
		if marshalErr != nil || writeNewPrivatePlan(request.Output, append(data, '\n')) != nil {
			return writeError(output, request.JSON, domain.NewError(domain.CodeUnavailable, "unable to write migration plan", false))
		}
	}
	return writeSuccess(output, request.JSON, safePlan(plan))
}
func runBackup(output io.Writer, request Request, service app.Foundation, selection foundation.Selection, ctx context.Context) int {
	var supplied *foundation.Plan
	if request.PlanFile != "" {
		data, readErr := readPrivatePayloadFile(request.PlanFile)
		if readErr != nil {
			return writeError(output, request.JSON, invalid("invalid migration plan"))
		}
		plan, parseErr := foundation.ReadPlan(data)
		if parseErr.Code != "" {
			return writeError(output, request.JSON, parseErr)
		}
		supplied = &plan
	}
	backup, err := service.Backup(ctx, selection, supplied)
	if err.Code != "" {
		return writeError(output, request.JSON, err)
	}
	return writeSuccess(output, request.JSON, struct {
		Checksum string `json:"checksum"`
	}{backup.Checksum})
}
func runApply(output io.Writer, request Request, service app.Foundation, selection foundation.Selection, ctx context.Context) int {
	if request.PlanFile == "" || request.ApprovalFile == "" {
		return writeError(output, request.JSON, invalid("migration apply requires plan and approval files"))
	}
	planData, planReadErr := readPrivatePayloadFile(request.PlanFile)
	if planReadErr != nil {
		return writeError(output, request.JSON, invalid("invalid migration plan"))
	}
	plan, planErr := foundation.ReadPlan(planData)
	if planErr.Code != "" {
		return writeError(output, request.JSON, planErr)
	}
	approvalData, approvalReadErr := readPrivatePayloadFile(request.ApprovalFile)
	if approvalReadErr != nil {
		return writeError(output, request.JSON, domain.NewError(domain.CodeInvalidArgument, "migration approval is invalid", false))
	}
	approval, approvalErr := foundation.ReadApproval(approvalData)
	if approvalErr.Code != "" {
		return writeError(output, request.JSON, approvalErr)
	}
	if err := service.Apply(ctx, selection, plan, approval); err.Code != "" {
		return writeError(output, request.JSON, err)
	}
	return writeSuccess(output, request.JSON, struct {
		Applied bool `json:"applied"`
	}{true})
}
func safePlan(plan foundation.Plan) any {
	return struct {
		ID          string   `json:"id"`
		Project     string   `json:"project"`
		FromVersion int      `json:"from_version"`
		ToVersion   int      `json:"to_version"`
		Checksums   []string `json:"checksums"`
	}{plan.ID, plan.Project, plan.FromVersion, plan.ToVersion, plan.Checksums}
}
func statusResult(output io.Writer, request Request, result foundation.Status, err domain.DomainError) int {
	if err.Code != "" {
		context := requestErrorContext(request)
		if strings.Contains(err.Message, "state path") {
			context.Hint = "Choose an absolute store path whose ancestors are owner/root controlled and grant no other account write access."
		}
		return writeErrorWithContext(output, request.JSON, err, context)
	}
	return writeSuccess(output, request.JSON, result)
}
func invalid(message string) domain.DomainError {
	return domain.NewError(domain.CodeInvalidArgument, message, false)
}

func requestErrorContext(request Request) terminalErrorContext {
	command, found := helpCommandByName(request.Name)
	if !found {
		return terminalErrorContext{Next: "omg --help"}
	}
	if request.Subcommand == "" {
		return terminalErrorContext{Next: "omg " + command.Name + " --help"}
	}
	if _, valid := helpSubcommandByName(command, request.Subcommand); valid {
		return terminalErrorContext{Next: "omg " + command.Name + " " + request.Subcommand + " --help"}
	}
	suggestion := closestSubcommand(command, request.Subcommand)
	if suggestion != "" {
		return terminalErrorContext{
			Hint: fmt.Sprintf("Did you mean %q?", suggestion),
			Next: "omg " + command.Name + " " + suggestion + " --help",
		}
	}
	return terminalErrorContext{Next: "omg " + command.Name + " --help"}
}

func writeInvalidRequest(output io.Writer, request Request, message string) int {
	return writeErrorWithContext(output, request.JSON, invalid(message), requestErrorContext(request))
}
func ExitCode(err domain.DomainError) int {
	switch err.Code {
	case domain.CodeInvalidArgument:
		return ExitUsage
	case domain.CodeNotFound:
		return ExitNotFound
	case domain.CodeUninitialized, domain.CodeCommandNotWired, domain.CodeUnavailable:
		return ExitUnavailable
	case domain.CodeConflict:
		return ExitConflict
	case domain.CodeInternal:
		return ExitInternal
	default:
		if err.Retryable {
			return ExitTemporary
		}
		return ExitInternal
	}
}
func writeSuccess(output io.Writer, jsonOutput bool, data any) int {
	if !jsonOutput {
		renderSuccess(output, data)
		return ExitSuccess
	}
	return writeJSON(output, SuccessEnvelope{OK: true, Data: data, Meta: Metadata{EnvelopeSchemaVersion, CommandSchemaVersion}, Warnings: []string{}}, ExitSuccess)
}
func neutralizeTerminalControls(value string) string {
	for index, character := range value {
		if !unicode.IsControl(character) {
			continue
		}
		var safe strings.Builder
		safe.Grow(len(value))
		safe.WriteString(value[:index])
		for _, character := range value[index:] {
			if !unicode.IsControl(character) {
				safe.WriteRune(character)
				continue
			}
			safe.WriteString(`\u`)
			for shift := uint(12); ; shift -= 4 {
				safe.WriteByte("0123456789ABCDEF"[(character>>shift)&0xF])
				if shift == 0 {
					break
				}
			}
		}
		return safe.String()
	}
	return value
}

func writeError(output io.Writer, jsonOutput bool, err domain.DomainError) int {
	return writeErrorWithContext(output, jsonOutput, err, terminalErrorContext{})
}

func writeErrorWithContext(output io.Writer, jsonOutput bool, err domain.DomainError, context terminalErrorContext) int {
	exit := ExitCode(err)
	if !jsonOutput {
		renderErrorWithContext(output, err, exit, context)
		return exit
	}
	return writeJSON(output, ErrorEnvelope{OK: false, Error: ErrorMetadata{string(err.Code), err.Message, err.Retryable, exit}, Meta: Metadata{EnvelopeSchemaVersion, CommandSchemaVersion}, Warnings: []string{}}, exit)
}
func writeJSON(output io.Writer, value any, exit int) int {
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(true)
	if encoder.Encode(value) != nil {
		return ExitInternal
	}
	return exit
}
func hasJSON(args []string) bool {
	for _, arg := range args {
		if arg == "--json" {
			return true
		}
	}
	return false
}

func helpRequested(args []string) bool {
	_, requested := parseHelpTarget(args)
	return requested
}

func usageText(version string) string {
	return renderUsage(version, false)
}
