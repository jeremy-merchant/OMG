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
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jeremy-merchant/OMG/internal/agentinstall"
	"github.com/jeremy-merchant/OMG/internal/app"
	"github.com/jeremy-merchant/OMG/internal/app/foundation"
	appLifecycle "github.com/jeremy-merchant/OMG/internal/app/lifecycle"
	"github.com/jeremy-merchant/OMG/internal/app/query"
	"github.com/jeremy-merchant/OMG/internal/domain"
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
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Retryable bool           `json:"retryable"`
	ExitCode  int            `json:"exit_code"`
	Recovery  *ErrorRecovery `json:"recovery,omitempty"`
}
type ErrorRecovery struct {
	Hint        string `json:"hint,omitempty"`
	NextCommand string `json:"next_command,omitempty"`
}
type ErrorEnvelope struct {
	OK       bool          `json:"ok"`
	Error    ErrorMetadata `json:"error"`
	Meta     Metadata      `json:"meta"`
	Warnings []string      `json:"warnings"`
}
type Request struct {
	Name                           string
	Subcommand                     string
	JSON                           bool
	Integrity                      bool
	Status                         bool
	Verbose                        bool
	Stdio                          bool
	Runtime                        string
	ControllerSessionID            string
	HumanID                        string
	Role                           string
	IntegrationBranch              string
	HandoffID                      string
	CanaryRunID                    string
	VerificationCommand            string
	ExecutionKind                  string
	EnvironmentFingerprint         string
	EvidencePath                   string
	ExitCode                       int
	PassedCount                    int
	FailedCount                    int
	SkippedCount                   int
	Command                        []string
	Project                        string
	Workspace                      string
	Store                          string
	projectProvided                bool
	workspaceProvided              bool
	storeProvided                  bool
	outputProvided                 bool
	planFileProvided               bool
	approvalFileProvided           bool
	idempotencyKeyProvided         bool
	formatProvided                 bool
	sessionProvided                bool
	taskProvided                   bool
	runtimeProvided                bool
	controllerSessionProvided      bool
	humanProvided                  bool
	roleProvided                   bool
	integrationBranchProvided      bool
	handoffProvided                bool
	canaryProvided                 bool
	verificationCommandProvided    bool
	executionKindProvided          bool
	environmentFingerprintProvided bool
	evidencePathProvided           bool
	exitCodeProvided               bool
	passedCountProvided            bool
	failedCountProvided            bool
	skippedCountProvided           bool
	Output                         string
	PlanFile                       string
	ApprovalFile                   string
	IdempotencyKey                 string
	Format                         string
	SessionID                      string
	TaskID                         string
	Payload                        string
	PayloadProvided                bool
	PayloadFile                    string
	PayloadFileProvided            bool
	PayloadStdin                   bool
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
	SourceCommit         string            `json:"source_commit,omitempty"`
	SourceTree           string            `json:"source_tree,omitempty"`
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
		case arg == "--verbose":
			request.Verbose = true
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
			case "--controller-session":
				request.ControllerSessionID = value
				request.controllerSessionProvided = true
			case "--human":
				request.HumanID = value
				request.humanProvided = true
			case "--role":
				request.Role = value
				request.roleProvided = true
			case "--integration-branch":
				request.IntegrationBranch = value
				request.integrationBranchProvided = true
			case "--integration-ref":
				request.IntegrationBranch = value
				request.integrationBranchProvided = true
			case "--handoff":
				request.HandoffID = value
				request.handoffProvided = true
			case "--canary":
				request.CanaryRunID = value
				request.canaryProvided = true
			case "--verification-command":
				request.VerificationCommand = value
				request.verificationCommandProvided = true
			case "--execution-kind":
				request.ExecutionKind = value
				request.executionKindProvided = true
			case "--environment-fingerprint":
				request.EnvironmentFingerprint = value
				request.environmentFingerprintProvided = true
			case "--evidence-path":
				request.EvidencePath = value
				request.evidencePathProvided = true
			case "--exit-code", "--passed", "--failed", "--skipped":
				number, parseErr := parseNonnegativeInt(value)
				if parseErr != nil {
					return Request{}, invalid("a canary count or exit code is invalid")
				}
				switch arg {
				case "--exit-code":
					request.ExitCode, request.exitCodeProvided = number, true
				case "--passed":
					request.PassedCount, request.passedCountProvided = number, true
				case "--failed":
					request.FailedCount, request.failedCountProvided = number, true
				case "--skipped":
					request.SkippedCount, request.skippedCountProvided = number, true
				}
			case "--payload":
				request.PayloadProvided = true
				request.Payload = value
			case "--payload-file":
				request.PayloadFileProvided = true
				request.PayloadFile = value
			}
		default:
			return Request{}, invalid(fmt.Sprintf("unsupported command argument %q", arg))
		}
		i++
	}
	return request, domain.DomainError{}
}

func commandTakesSubcommand(name string) bool {
	switch name {
	case "migration", "backup", "release", "agent", "worker", "mode", "board", "export", "integration",
		"shell-init", "completion", "watch", "human", "session", "delegate", "checkpoint",
		"task", "progress", "dependency", "message", "handoff", "reserve", "git", "orphan", "canary", "import", "receipt", "example", "mcp":
		return true
	default:
		return false
	}
}

func optionTakesValue(arg string) bool {
	switch arg {
	case "--project", "--workspace", "--store", "--output", "--plan-file", "--approval-file", "--idempotency-key", "--format", "--session", "--task", "--runtime", "--controller-session", "--human", "--role", "--integration-branch", "--integration-ref", "--handoff", "--canary", "--verification-command", "--execution-kind", "--environment-fingerprint", "--evidence-path", "--exit-code", "--passed", "--failed", "--skipped", "--payload", "--payload-file":
		return true
	default:
		return false
	}
}

func parseNonnegativeInt(value string) (int, error) {
	number, err := strconv.Atoi(value)
	if err != nil || number < 0 {
		return 0, errors.New("invalid nonnegative integer")
	}
	return number, nil
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
	if len(args) == 0 {
		width := cliTerminalWidth(output)
		color := cliTerminalColorEnabled(output)
		usage := renderGlobalHelp(version, color, width, compactHelpHeight-1)
		if _, err := io.WriteString(output, usage); err != nil {
			return ExitInternal
		}
		return ExitSuccess
	}
	if args[0] == "--version" {
		args = append([]string{"version"}, args[1:]...)
	}
	if args[0] == "inbox" {
		return writeErrorWithContext(output, hasJSON(args), domain.NewError(domain.CodeCommandNotWired, "legacy command omg inbox moved to omg message inbox", false), terminalErrorContext{
			Hint: "The inbox payload is {\"recipient\":{\"session_id\":\"SESSION_ID\"}}.", Next: "omg example show message-inbox --json",
		})
	}
	if args[0] == "schema" {
		return writeErrorWithContext(output, hasJSON(args), domain.NewError(domain.CodeCommandNotWired, "legacy command omg schema moved to omg migration", false), terminalErrorContext{
			Hint: "Migration planning is read-only; applying still requires an explicit approval file and backup.", Next: "omg migration --help",
		})
	}
	if len(args) == 1 && commandRequiresSubcommand(args[0]) {
		usage, found := renderHelpWithHeight(version, cliTerminalColorEnabled(output), cliTerminalWidth(output), cliTerminalHeight(output), helpTarget{Command: args[0]})
		if found {
			if _, err := io.WriteString(output, usage); err != nil {
				return ExitInternal
			}
			return ExitSuccess
		}
	}
	if target, requested := parseHelpTarget(args); requested {
		jsonOutput := hasJSON(args)
		width := cliTerminalWidth(output)
		height := cliTerminalHeight(output)
		color := !jsonOutput && cliTerminalColorEnabled(output)
		if jsonOutput {
			width = defaultTerminalWidth
			height = 0
		}
		usage, found := renderHelpWithHeight(version, color, width, height, target)
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
	if args[0] == "example" {
		return runExample(output, args, version)
	}
	request, err := Decode(args)
	if err.Code != "" {
		return writeErrorWithContext(output, hasJSON(args), err, decodeRecoveryContext(args))
	}
	if request.Project == "" && request.Workspace == "" && request.Store == "" {
		request.Project = os.Getenv("OMG_PROJECT")
	}
	if request.Name != "worker" && (request.controllerSessionProvided || request.humanProvided || request.roleProvided) {
		return writeInvalidRequest(output, request, "worker identity options are supported only by worker bootstrap")
	}
	if request.Name != "canary" && hasCanaryOptions(request) || request.integrationBranchProvided && request.Name != "git" && request.Name != "orphan" && request.Name != "canary" {
		return writeInvalidRequest(output, request, "command-specific Git or canary options are invalid here")
	}
	if request.Verbose && request.Name != "preflight" {
		return writeInvalidRequest(output, request, "--verbose is supported only by preflight")
	}
	if !request.JSON && applicationCommandName(request.Name) {
		if message, context, invalidPath := commandPathProblem(request.Name, request.Subcommand); invalidPath {
			return writeErrorWithContext(output, false, domain.NewError(domain.CodeCommandNotWired, message, false), context)
		}
	}
	if (request.PayloadProvided || request.PayloadFileProvided || request.PayloadStdin) && !applicationCommandName(request.Name) && request.Name != "mode" &&
		!(request.Name == "backup" && request.Subcommand == "restore") {
		return writeError(output, request.JSON, invalid("payload transport is invalid for this command"))
	}
	selection := foundation.Selection{Project: request.Project, Workspace: request.Workspace, Store: request.Store}
	switch request.Name {
	case "agent":
		return runAgent(output, request)
	case "worker":
		return runWorkerBootstrap(ctx, output, request, application)
	case "mode":
		return runLifecycleMode(output, stdin, request)
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
				Status        string `json:"status"`
				Repository    string `json:"repository"`
				License       string `json:"license"`
				StableRelease bool   `json:"stable_release"`
			}{
				Status:        "SOURCE PUBLISHED",
				Repository:    "github.com/jeremy-merchant/OMG",
				License:       "Apache-2.0",
				StableRelease: false,
			})
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
	case "status":
		return runOperatorSummary(ctx, output, request, application, selection)
	case "stale":
		return runStaleSessions(ctx, output, request, application, selection)
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
	case "human", "session", "delegate", "checkpoint", "task", "progress", "dependency", "message", "handoff", "reserve", "git", "orphan", "canary", "import", "receipt":
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

type exampleListResult struct {
	Topics []string `json:"topics"`
}

type exampleShowResult struct {
	Topic          string         `json:"topic"`
	Command        string         `json:"command"`
	Usage          string         `json:"usage"`
	PayloadSchema  map[string]any `json:"payload_schema,omitempty"`
	ExamplePayload any            `json:"example_payload,omitempty"`
}

func runExample(output io.Writer, args []string, version string) int {
	jsonOutput := hasJSON(args)
	if len(args) < 2 {
		return writeErrorWithContext(output, jsonOutput, invalid("an example subcommand is required"), terminalErrorContext{
			Hint: "Use `list` to discover topics or `show` to open one copyable example.",
			Next: "omg example --help",
		})
	}

	operands := make([]string, 0, len(args)-2)
	for _, argument := range args[2:] {
		if argument == "--json" {
			continue
		}
		if strings.HasPrefix(argument, "-") {
			return writeErrorWithContext(output, jsonOutput, invalid(fmt.Sprintf("unsupported command argument %q", argument)), terminalErrorContext{
				Hint: "Example discovery accepts only a topic and optional --json.",
				Next: "omg example --help",
			})
		}
		operands = append(operands, argument)
	}

	switch args[1] {
	case "list":
		if len(operands) != 0 {
			return writeErrorWithContext(output, jsonOutput, invalid("example list does not accept a topic"), terminalErrorContext{Next: "omg example list --help"})
		}
		topics := exampleTopics()
		if jsonOutput {
			return writeSuccess(output, true, exampleListResult{Topics: topics})
		}
		_, _ = fmt.Fprintln(output, "Example topics:")
		for _, topic := range topics {
			_, _ = fmt.Fprintf(output, "  %s\n", topic)
		}
		return ExitSuccess
	case "show":
		if len(operands) != 1 {
			return writeErrorWithContext(output, jsonOutput, invalid("example show requires exactly one topic"), terminalErrorContext{
				Hint: "Run `omg example list` to discover valid topics.",
				Next: "omg example show --help",
			})
		}
		topic, target, found := resolveExampleTopic(operands[0])
		if !found {
			suggestion := closestExampleTopic(operands[0])
			context := terminalErrorContext{Next: "omg example list"}
			if suggestion != "" {
				context.Hint = fmt.Sprintf("Did you mean %q?", suggestion)
				context.Next = "omg example show " + suggestion
			}
			return writeErrorWithContext(output, jsonOutput, invalid(fmt.Sprintf("unknown example topic %q", operands[0])), context)
		}
		usage, rendered := renderHelpWithHeight(version, !jsonOutput && cliTerminalColorEnabled(output), cliTerminalWidth(output), cliTerminalHeight(output), target)
		if !rendered {
			return writeErrorWithContext(output, jsonOutput, invalid("example topic is unavailable"), terminalErrorContext{Next: "omg example list"})
		}
		if jsonOutput {
			payloadSchema, examplePayload, _ := examplePayloadContract(topic)
			return writeSuccess(output, true, exampleShowResult{
				Topic: topic, Command: strings.TrimSpace("omg " + target.Command + " " + target.Subcommand),
				Usage: stripTerminalANSI(usage), PayloadSchema: payloadSchema, ExamplePayload: examplePayload,
			})
		}
		if _, err := io.WriteString(output, usage); err != nil {
			return ExitInternal
		}
		return ExitSuccess
	default:
		return writeErrorWithContext(output, jsonOutput, invalid(fmt.Sprintf("unknown example subcommand %q", args[1])), terminalErrorContext{
			Hint: "Use `list` or `show`.",
			Next: "omg example --help",
		})
	}
}

func decodeRecoveryContext(args []string) terminalErrorContext {
	if len(args) == 0 {
		return terminalErrorContext{Next: "omg --help"}
	}
	if args[0] == "reserve" {
		return terminalErrorContext{
			Hint: "reservations require `reserve add` with a strict lineage payload",
			Next: "omg reserve add --help",
		}
	}
	if knownCommand(args[0]) {
		return terminalErrorContext{
			Hint: "Use only the options shown by contextual help.",
			Next: "omg " + args[0] + " --help",
		}
	}
	return terminalErrorContext{Next: "omg --help"}
}

func runAgent(output io.Writer, request Request) int {
	if request.Integrity || request.Status || request.Stdio || request.Runtime != "" || len(request.Command) != 0 ||
		request.Project != "" || request.Workspace != "" || request.Store != "" || request.Output != "" ||
		request.PlanFile != "" || request.ApprovalFile != "" || request.IdempotencyKey != "" || request.Format != "" ||
		request.SessionID != "" || request.TaskID != "" || request.PayloadProvided || request.PayloadFileProvided || request.PayloadStdin {
		return writeInvalidRequest(output, request, "agent request is invalid")
	}
	service, err := agentinstall.FromEnvironment()
	if err != nil {
		return writeError(output, request.JSON, domain.NewError(domain.CodeUnavailable, "global agent home is unsafe or unavailable", false))
	}
	var report agentinstall.Report
	switch request.Subcommand {
	case "install":
		report, err = service.Install()
	case "status":
		report, err = service.Status()
	case "doctor":
		report, err = service.Doctor()
	case "uninstall":
		report, err = service.Uninstall()
	default:
		return writeInvalidRequest(output, request, "agent subcommand is invalid")
	}
	if err != nil {
		switch {
		case errors.Is(err, agentinstall.ErrConflict):
			return writeErrorWithContext(output, request.JSON, domain.NewError(domain.CodeConflict, "global agent integration conflicts with existing or drifted content", false), terminalErrorContext{Next: "omg agent status"})
		case errors.Is(err, agentinstall.ErrUnsafeHome):
			return writeErrorWithContext(output, request.JSON, domain.NewError(domain.CodeUnavailable, "global agent integration contains an unsafe filesystem component", false), terminalErrorContext{Next: "omg agent doctor"})
		default:
			return writeErrorWithContext(output, request.JSON, domain.NewError(domain.CodeUnavailable, "global agent integration could not be updated", false), terminalErrorContext{Next: "omg agent doctor"})
		}
	}
	if request.JSON {
		return writeSuccess(output, true, report)
	}
	renderAgentReport(output, report)
	return ExitSuccess
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
	directCanary := request.Name == "canary" && (request.Subcommand == "start" || request.Subcommand == "finish")
	if len(request.Command) != 0 || request.Output != "" || request.PlanFile != "" ||
		request.ApprovalFile != "" || request.Format != "" || !directCanary && request.SessionID != "" || request.TaskID != "" ||
		request.Runtime != "" || request.Integrity || request.Status || request.Stdio {
		return writeInvalidRequest(output, request, "application request is invalid")
	}
	var payload []byte
	var err error
	directGitQuery := request.Name == "git" && request.Subcommand == "reconcile" || request.Name == "orphan" && request.Subcommand == "scan"
	if directCanary {
		if request.PayloadProvided || request.PayloadFileProvided || request.PayloadStdin || request.IdempotencyKey == "" || request.SessionID == "" {
			return writeInvalidRequest(output, request, "exact-SHA canary request is invalid")
		}
		if request.Subcommand == "start" {
			if request.HandoffID == "" || request.IntegrationBranch == "" || request.VerificationCommand == "" || request.ExecutionKind == "" || request.EnvironmentFingerprint == "" || request.canaryProvided || request.exitCodeProvided || request.passedCountProvided || request.failedCountProvided || request.skippedCountProvided || request.evidencePathProvided {
				return writeInvalidRequest(output, request, "canary start request is invalid")
			}
			payload, err = json.Marshal(map[string]any{"handoff_id": request.HandoffID, "actor_session_id": request.SessionID, "integration_ref": request.IntegrationBranch, "verification_command": request.VerificationCommand, "execution_kind": request.ExecutionKind, "environment_fingerprint": request.EnvironmentFingerprint})
		} else {
			if request.CanaryRunID == "" || !request.exitCodeProvided || !request.passedCountProvided || !request.failedCountProvided || !request.skippedCountProvided || request.handoffProvided || request.integrationBranchProvided || request.verificationCommandProvided || request.executionKindProvided || request.environmentFingerprintProvided {
				return writeInvalidRequest(output, request, "canary finish request is invalid")
			}
			payload, err = json.Marshal(map[string]any{"canary_run_id": request.CanaryRunID, "actor_session_id": request.SessionID, "exit_code": request.ExitCode, "passed_count": request.PassedCount, "failed_count": request.FailedCount, "skipped_count": request.SkippedCount, "evidence_path": request.EvidencePath})
		}
	} else if directGitQuery {
		if request.PayloadProvided || request.PayloadFileProvided || request.PayloadStdin || request.IdempotencyKey != "" || request.Name == "git" && request.IntegrationBranch == "" {
			return writeInvalidRequest(output, request, "Git reconciliation request is invalid")
		}
		integrationBranch := request.IntegrationBranch
		if integrationBranch == "" {
			integrationBranch = "HEAD"
		}
		payload, err = json.Marshal(map[string]string{"integration_branch": integrationBranch})
	} else {
		if request.integrationBranchProvided || request.IntegrationBranch != "" || hasCanaryOptions(request) {
			return writeInvalidRequest(output, request, "direct Git and canary options are not supported by this command")
		}
		if request.Name == "git" && request.Subcommand == "inventory" && !request.PayloadProvided && !request.PayloadFileProvided && !request.PayloadStdin {
			return writeErrorWithContext(output, request.JSON, invalid("git inventory records a fresh observation and requires an idempotency key plus directory payload"), terminalErrorContext{
				Hint: "For read-only inspection of the current observation, use git current.", Next: "omg git current --project " + shellQuote(selection.Project) + " --json",
			})
		}
		var loaded string
		loaded, err = loadApplicationPayload(request, input)
		payload = []byte(loaded)
	}
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
		if outcome.Error.Code == domain.CodeInvalidArgument {
			return writeErrorWithContext(output, request.JSON, outcome.Error, applicationPayloadRecovery(request))
		}
		if request.Name == "session" && request.Subcommand == "create" && outcome.Error.Code == domain.CodeNotFound {
			return writeErrorWithContext(output, request.JSON, outcome.Error, terminalErrorContext{
				Hint: "Use the controller-provided OMG_HUMAN_ID; create a human only when establishing a new owner.",
				Next: "omg example show session-create --json",
			})
		}
		return writeError(output, request.JSON, outcome.Error)
	}
	return writeSuccess(output, request.JSON, outcome.Data)
}

func applicationPayloadRecovery(request Request) terminalErrorContext {
	topic := request.Name
	if request.Subcommand != "" {
		topic += "-" + request.Subcommand
	}
	if _, _, ok := examplePayloadContract(topic); ok {
		return terminalErrorContext{Hint: "Inspect the copyable payload_schema and example_payload fields.", Next: "omg example show " + topic + " --json"}
	}
	return terminalErrorContext{Hint: "Use only the fields documented by contextual help.", Next: "omg " + request.Name + " " + request.Subcommand + " --help"}
}

func applicationCommandName(name string) bool {
	switch name {
	case "human", "session", "delegate", "checkpoint", "task", "progress", "dependency", "message", "handoff", "reserve", "git", "orphan", "canary", "import", "receipt":
		return true
	default:
		return false
	}
}

func runLifecycleMode(output io.Writer, input io.Reader, request Request) int {
	if request.Integrity || request.Status || request.Stdio || request.Verbose || request.runtimeProvided || request.Runtime != "" ||
		len(request.Command) != 0 || request.outputProvided || request.Output != "" || request.planFileProvided || request.PlanFile != "" ||
		request.approvalFileProvided || request.ApprovalFile != "" || request.idempotencyKeyProvided || request.IdempotencyKey != "" ||
		request.formatProvided || request.Format != "" || request.sessionProvided || request.SessionID != "" || request.taskProvided || request.TaskID != "" {
		return writeInvalidRequest(output, request, "mode request is invalid")
	}

	var (
		contract appLifecycle.Contract
		err      error
	)
	switch request.Subcommand {
	case "observe", "work-lite", "full":
		if request.PayloadProvided || request.PayloadFileProvided || request.PayloadStdin {
			return writeInvalidRequest(output, request, "fixed mode request does not accept a payload")
		}
		contract, err = appLifecycle.ContractFor(appLifecycle.Mode(strings.ToUpper(strings.ReplaceAll(request.Subcommand, "-", "_"))))
	case "classify":
		payload, loadErr := loadApplicationPayload(request, input)
		if loadErr != nil {
			return writeErrorWithContext(output, request.JSON, invalid("mode payload transport is invalid"), terminalErrorContext{Next: "omg mode classify --help"})
		}
		var modeInput appLifecycle.Input
		if !decodePayload(payload, &modeInput) {
			return writeErrorWithContext(output, request.JSON, invalid("mode payload is invalid"), terminalErrorContext{Next: "omg mode classify --help"})
		}
		contract, err = appLifecycle.Classify(modeInput)
	default:
		return writeInvalidRequest(output, request, "mode request is invalid")
	}
	if err != nil {
		return writeError(output, request.JSON, invalid(err.Error()))
	}
	if request.JSON {
		return writeSuccess(output, true, contract)
	}
	_, _ = fmt.Fprintf(output, "OMG MODE %s\nverification=%s session=%t task=%t run=%t progress=%t reservation=%t handoff=%t independent_verification=%t auto_archive=%t\n",
		contract.Mode, contract.VerificationLevel, contract.SessionRequired, contract.TaskRequired, contract.RunRequired,
		contract.ProgressRequired, contract.ReservationRequired, contract.HandoffRequired, contract.IndependentVerificationRequired, contract.AutoArchive)
	return ExitSuccess
}

func hasCanaryOptions(request Request) bool {
	return request.handoffProvided || request.canaryProvided || request.verificationCommandProvided || request.executionKindProvided || request.environmentFingerprintProvided || request.evidencePathProvided || request.exitCodeProvided || request.passedCountProvided || request.failedCountProvided || request.skippedCountProvided
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
	if request.Subcommand == string(query.BoardMe) && !request.sessionProvided && request.SessionID == "" {
		if sessionID := os.Getenv("OMG_SESSION_ID"); sessionID != "" {
			request.SessionID = sessionID
			request.sessionProvided = true
		}
	}
	if !validBoardRequest(request) {
		return writeInvalidRequest(output, request, "board request is invalid")
	}
	if request.Subcommand == string(query.BoardSummary) || request.Subcommand == "backlog" {
		return runOperatorSummary(ctx, output, request, application, selection)
	}
	if request.Subcommand == "hygiene" {
		return runBoardHygiene(ctx, output, request, application, selection)
	}
	if request.Subcommand == "actionable" {
		return runBoardActionable(ctx, output, request, application, selection)
	}
	if request.Subcommand == "history" {
		request.Subcommand = string(query.BoardAll)
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
	switch request.Subcommand {
	case "actionable", "backlog", "hygiene", "history":
		return !request.sessionProvided && !request.taskProvided && !request.formatProvided
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
	case query.BoardSummary:
		if request.sessionProvided || request.taskProvided || request.formatProvided {
			return false
		}
		return true
	}
	return query.BoardRequest{
		Mode:      query.BoardMode(request.Subcommand),
		SessionID: request.SessionID,
		TaskID:    request.TaskID,
	}.Validate() == nil
}

func runOperatorSummary(ctx context.Context, output io.Writer, request Request, application app.CLIService, selection foundation.Selection) int {
	if request.Name == "status" {
		if request.Subcommand != "" || request.Integrity || request.Status || request.Stdio || request.Verbose || request.runtimeProvided || request.Runtime != "" || len(request.Command) != 0 || request.outputProvided || request.Output != "" || request.planFileProvided || request.PlanFile != "" || request.approvalFileProvided || request.ApprovalFile != "" || request.idempotencyKeyProvided || request.IdempotencyKey != "" || request.formatProvided || request.Format != "" || request.sessionProvided || request.SessionID != "" || request.taskProvided || request.TaskID != "" || request.PayloadProvided || request.Payload != "" || request.PayloadFileProvided || request.PayloadFile != "" || request.PayloadStdin {
			return writeInvalidRequest(output, request, "status request is invalid")
		}
	}
	model, err := loadBoard(ctx, application.Dispatcher, selection, query.BoardRequest{Mode: query.BoardAll})
	if err.Code != "" {
		return writeError(output, request.JSON, err)
	}
	var snapshot query.BoardSnapshot
	if decodeErr := json.Unmarshal(model.Data(), &snapshot); decodeErr != nil {
		return writeError(output, request.JSON, domain.NewError(domain.CodeInternal, "unable to decode operator summary", false))
	}
	summary := query.Summarize(snapshot)
	if request.JSON {
		return writeSuccess(output, true, summary)
	}
	_, _ = fmt.Fprintf(output, "OMG STATUS\nactive_sessions=%d stale_sessions=%d conflicts=%d integration_queue=%d\n", summary.ActiveSessions, summary.StaleSessions, summary.Conflicts, summary.IntegrationQueue)
	for _, bottleneck := range summary.Bottlenecks {
		_, _ = fmt.Fprintf(output, "%s %d → %s %d\n", bottleneck.From, bottleneck.Waiting, bottleneck.To, bottleneck.Done)
	}
	return ExitSuccess
}

type actionableBoardView struct {
	GeneratedAt          time.Time                        `json:"generated_at"`
	SnapshotCursor       string                           `json:"snapshot_cursor"`
	Totals               actionableBoardCounts            `json:"totals"`
	Truncated            map[string]int                   `json:"truncated"`
	Tasks                []query.TaskView                 `json:"tasks"`
	Handoffs             []query.IntegrationQueueItemView `json:"handoffs"`
	UnreadInbox          []query.InboxItemView            `json:"unread_inbox"`
	ConflictingResources []query.ReservationView          `json:"conflicting_resources"`
	StaleSessions        []query.IdentityView             `json:"stale_sessions"`
}

type actionableBoardCounts struct {
	Tasks         int `json:"tasks"`
	Handoffs      int `json:"handoffs"`
	UnreadInbox   int `json:"unread_inbox"`
	Conflicts     int `json:"conflicts"`
	StaleSessions int `json:"stale_sessions"`
}

const actionableItemLimit = 10

func runBoardActionable(ctx context.Context, output io.Writer, request Request, application app.CLIService, selection foundation.Selection) int {
	snapshot, err := loadBoardSnapshot(ctx, application, selection)
	if err.Code != "" {
		return writeError(output, request.JSON, err)
	}
	view := actionableBoardView{
		GeneratedAt: snapshot.GeneratedAt, SnapshotCursor: snapshot.SnapshotCursor, Truncated: map[string]int{},
		Tasks: []query.TaskView{}, Handoffs: []query.IntegrationQueueItemView{}, UnreadInbox: []query.InboxItemView{},
		ConflictingResources: []query.ReservationView{}, StaleSessions: []query.IdentityView{},
	}
	for _, task := range snapshot.Tasks {
		switch task.State {
		case "WORK_COMPLETE", "BLOCKED", "FAILED":
			view.Totals.Tasks++
			if len(view.Tasks) < actionableItemLimit {
				view.Tasks = append(view.Tasks, task)
			}
		}
	}
	for _, handoff := range query.IntegrationQueue(snapshot) {
		view.Totals.Handoffs++
		if len(view.Handoffs) < actionableItemLimit {
			view.Handoffs = append(view.Handoffs, handoff)
		}
	}
	for _, item := range snapshot.Inbox {
		if item.ReadAt == nil || item.Acknowledgement == "pending" {
			view.Totals.UnreadInbox++
			if len(view.UnreadInbox) < actionableItemLimit {
				view.UnreadInbox = append(view.UnreadInbox, item)
			}
		}
	}
	for _, item := range snapshot.Reservations {
		if len(item.ConflictIDs) != 0 {
			view.Totals.Conflicts++
			if len(view.ConflictingResources) < actionableItemLimit {
				view.ConflictingResources = append(view.ConflictingResources, item)
			}
		}
	}
	for _, session := range snapshot.Sessions {
		if session.EndedAt == nil && (session.Liveness == query.SessionLivenessStale || session.InterruptedAt != nil) {
			view.Totals.StaleSessions++
			if len(view.StaleSessions) < actionableItemLimit {
				view.StaleSessions = append(view.StaleSessions, session)
			}
		}
	}
	view.Truncated["tasks"] = view.Totals.Tasks - len(view.Tasks)
	view.Truncated["handoffs"] = view.Totals.Handoffs - len(view.Handoffs)
	view.Truncated["unread_inbox"] = view.Totals.UnreadInbox - len(view.UnreadInbox)
	view.Truncated["conflicts"] = view.Totals.Conflicts - len(view.ConflictingResources)
	view.Truncated["stale_sessions"] = view.Totals.StaleSessions - len(view.StaleSessions)
	if request.JSON {
		return writeSuccess(output, true, view)
	}
	_, _ = fmt.Fprintf(output, "OMG ACTIONABLE\ntasks=%d handoffs=%d inbox=%d conflicts=%d stale_sessions=%d\n",
		view.Totals.Tasks, view.Totals.Handoffs, view.Totals.UnreadInbox, view.Totals.Conflicts, view.Totals.StaleSessions)
	return ExitSuccess
}

func runBoardHygiene(ctx context.Context, output io.Writer, request Request, application app.CLIService, selection foundation.Selection) int {
	snapshot, err := loadBoardSnapshot(ctx, application, selection)
	if err.Code != "" {
		return writeError(output, request.JSON, err)
	}
	view := query.ClassifySessions(snapshot)
	truncated := 0
	if len(view.Sessions) > actionableItemLimit {
		truncated = len(view.Sessions) - actionableItemLimit
		view.Sessions = view.Sessions[:actionableItemLimit]
	}
	if request.JSON {
		return writeSuccess(output, true, struct {
			query.StaleView
			Truncated int `json:"truncated"`
		}{StaleView: view, Truncated: truncated})
	}
	exit := renderSessionClassifications(output, false, view, "OMG HYGIENE")
	if truncated != 0 {
		_, _ = fmt.Fprintf(output, "... %d additional session classifications; use omg stale for the complete diagnostic list\n", truncated)
	}
	return exit
}

func loadBoardSnapshot(ctx context.Context, application app.CLIService, selection foundation.Selection) (query.BoardSnapshot, domain.DomainError) {
	model, err := loadBoard(ctx, application.Dispatcher, selection, query.BoardRequest{Mode: query.BoardAll})
	if err.Code != "" {
		return query.BoardSnapshot{}, err
	}
	var snapshot query.BoardSnapshot
	if decodeErr := json.Unmarshal(model.Data(), &snapshot); decodeErr != nil {
		return query.BoardSnapshot{}, domain.NewError(domain.CodeInternal, "unable to decode board snapshot", false)
	}
	return snapshot, domain.DomainError{}
}

func runStaleSessions(ctx context.Context, output io.Writer, request Request, application app.CLIService, selection foundation.Selection) int {
	if !validStaleRequest(request) {
		return writeInvalidRequest(output, request, "stale request is invalid")
	}
	model, err := loadBoard(ctx, application.Dispatcher, selection, query.BoardRequest{Mode: query.BoardAll})
	if err.Code != "" {
		return writeError(output, request.JSON, err)
	}
	var snapshot query.BoardSnapshot
	if decodeErr := json.Unmarshal(model.Data(), &snapshot); decodeErr != nil {
		return writeError(output, request.JSON, domain.NewError(domain.CodeInternal, "unable to decode session classifications", false))
	}
	return renderSessionClassifications(output, request.JSON, query.ClassifySessions(snapshot), "OMG STALE")
}

func renderSessionClassifications(output io.Writer, jsonOutput bool, view query.StaleView, title string) int {
	if jsonOutput {
		return writeSuccess(output, true, view)
	}
	_, _ = fmt.Fprintf(output, "%s\nthresholds idle=%s stale=%s\nalive=%d idle=%d stale=%d runtime_unobservable=%d finished_unclosed=%d\n", title,
		time.Duration(view.IdleAfterSeconds)*time.Second,
		time.Duration(view.StaleAfterSeconds)*time.Second,
		view.Counts.Alive,
		view.Counts.Idle,
		view.Counts.Stale,
		view.Counts.RuntimeUnobservable,
		view.Counts.FinishedUnclosed,
	)
	for _, session := range view.Sessions {
		heartbeat := "-"
		if session.LastHeartbeatAt != nil {
			heartbeat = session.LastHeartbeatAt.Format(time.RFC3339)
		}
		taskID := session.TaskID
		if taskID == "" {
			taskID = "-"
		}
		runStates := "-"
		if len(session.RunStates) != 0 {
			runStates = strings.Join(session.RunStates, ",")
		}
		_, _ = fmt.Fprintf(output, "%s %s age=%s heartbeat=%s runtime=%s task=%s runs=%s action=%s\n",
			strings.ToUpper(string(session.Classification)),
			session.SessionID,
			formatElapsedSeconds(session.ElapsedSeconds),
			heartbeat,
			session.Runtime,
			taskID,
			runStates,
			session.RecommendedAction,
		)
	}
	return ExitSuccess
}

func validStaleRequest(request Request) bool {
	return request.Name == "stale" && request.Subcommand == "" &&
		!request.Integrity && !request.Status && !request.Stdio && !request.Verbose &&
		!request.runtimeProvided && request.Runtime == "" && len(request.Command) == 0 &&
		!request.outputProvided && request.Output == "" && !request.planFileProvided && request.PlanFile == "" &&
		!request.approvalFileProvided && request.ApprovalFile == "" && !request.idempotencyKeyProvided && request.IdempotencyKey == "" &&
		!request.formatProvided && request.Format == "" && !request.sessionProvided && request.SessionID == "" &&
		!request.taskProvided && request.TaskID == "" && !request.PayloadProvided && request.Payload == "" &&
		!request.PayloadFileProvided && request.PayloadFile == "" && !request.PayloadStdin
}

func formatElapsedSeconds(seconds int64) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	if seconds < 3600 {
		return fmt.Sprintf("%dm", seconds/60)
	}
	if seconds < 86400 {
		return fmt.Sprintf("%dh%dm", seconds/3600, seconds%3600/60)
	}
	return fmt.Sprintf("%dd%dh", seconds/86400, seconds%86400/3600)
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
	if request.Subcommand == "queue" {
		return runIntegrationQueue(ctx, output, request, application, selection)
	}
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

func runIntegrationQueue(ctx context.Context, output io.Writer, request Request, application app.CLIService, selection foundation.Selection) int {
	if !validIntegrationQueueRequest(request) {
		return writeInvalidRequest(output, request, "integration queue request is invalid")
	}
	model, err := loadBoard(ctx, application.Dispatcher, selection, query.BoardRequest{Mode: query.BoardAll})
	if err.Code != "" {
		return writeError(output, request.JSON, err)
	}
	var snapshot query.BoardSnapshot
	if decodeErr := json.Unmarshal(model.Data(), &snapshot); decodeErr != nil {
		return writeError(output, request.JSON, domain.NewError(domain.CodeInternal, "unable to decode integration queue", false))
	}
	items := query.IntegrationQueue(snapshot)
	result := struct {
		Count int                              `json:"count"`
		Items []query.IntegrationQueueItemView `json:"items"`
	}{Count: len(items), Items: items}
	if request.JSON {
		return writeSuccess(output, true, result)
	}
	_, _ = fmt.Fprintf(output, "OMG INTEGRATION QUEUE  %d item(s)\n", len(items))
	for _, item := range items {
		missing := ""
		if len(item.MissingEvidence) != 0 {
			missing = " missing=" + strings.Join(item.MissingEvidence, ",")
		}
		_, _ = fmt.Fprintf(output, "%s  %s  task=%s%s\n", item.State, item.HandoffID, item.TaskID, missing)
	}
	return ExitSuccess
}

func validIntegrationQueueRequest(request Request) bool {
	return request.Subcommand == "queue" && !request.Integrity && !request.Status && !request.Stdio && !request.Verbose &&
		!request.runtimeProvided && request.Runtime == "" && len(request.Command) == 0 &&
		!request.outputProvided && request.Output == "" && !request.planFileProvided && request.PlanFile == "" &&
		!request.approvalFileProvided && request.ApprovalFile == "" && !request.idempotencyKeyProvided && request.IdempotencyKey == "" &&
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
		ID                string   `json:"id"`
		Project           string   `json:"project"`
		FromVersion       int      `json:"from_version"`
		ToVersion         int      `json:"to_version"`
		Checksums         []string `json:"checksums"`
		AutomaticEligible bool     `json:"automatic_eligible"`
	}{plan.ID, plan.Project, plan.FromVersion, plan.ToVersion, plan.Checksums, plan.AutomaticEligible}
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
	warnings := make([]string, 0, 2)
	if context.Hint != "" {
		warnings = append(warnings, "hint: "+neutralizeTerminalControls(context.Hint))
	}
	if context.Next != "" {
		warnings = append(warnings, "next: "+neutralizeTerminalControls(context.Next))
	}
	var recovery *ErrorRecovery
	if context.Hint != "" || context.Next != "" {
		recovery = &ErrorRecovery{Hint: neutralizeTerminalControls(context.Hint), NextCommand: neutralizeTerminalControls(context.Next)}
	}
	return writeJSON(output, ErrorEnvelope{OK: false, Error: ErrorMetadata{
		Code: string(err.Code), Message: err.Message, Retryable: err.Retryable, ExitCode: exit, Recovery: recovery,
	}, Meta: Metadata{EnvelopeSchemaVersion, CommandSchemaVersion}, Warnings: warnings}, exit)
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
