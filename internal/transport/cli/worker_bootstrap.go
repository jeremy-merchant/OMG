package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jeremy-merchant/OMG/internal/app"
	"github.com/jeremy-merchant/OMG/internal/app/foundation"
	"github.com/jeremy-merchant/OMG/internal/app/query"
	"github.com/jeremy-merchant/OMG/internal/domain"
)

type workerTask struct {
	ID                 string `json:"id"`
	State              string `json:"state"`
	ClaimedBySessionID string `json:"claimed_by_session_id,omitempty"`
}

type workerBootstrapResult struct {
	Healthy             bool              `json:"healthy"`
	Project             string            `json:"project"`
	SessionID           string            `json:"session_id"`
	TaskID              string            `json:"task_id"`
	ControllerSessionID string            `json:"controller_session_id"`
	HumanID             string            `json:"human_id"`
	SessionCreated      bool              `json:"session_created"`
	TaskClaimed         bool              `json:"task_claimed"`
	Environment         map[string]string `json:"environment"`
	Inbox               any               `json:"inbox"`
	Board               query.ViewModel   `json:"board"`
	NextAction          workerNextAction  `json:"next_action"`
	BootstrapFile       string            `json:"bootstrap_file,omitempty"`
}

type workerNextAction struct {
	Code    string   `json:"code"`
	Command string   `json:"command"`
	Argv    []string `json:"argv"`
}

func runWorkerBootstrap(ctx context.Context, output io.Writer, request Request, application app.CLIService) int {
	if request.Subcommand != "bootstrap" || request.Workspace != "" || request.Store != "" || request.Integrity || request.Status || request.Verbose || request.Stdio ||
		request.Format != "" || request.PayloadProvided || request.PayloadFileProvided || request.PayloadStdin || request.PlanFile != "" || request.ApprovalFile != "" || len(request.Command) != 0 {
		return writeInvalidRequest(output, request, "worker bootstrap request is invalid")
	}
	applyWorkerEnvironment(&request)
	if request.Project == "" || request.SessionID == "" || request.TaskID == "" || request.ControllerSessionID == "" || request.HumanID == "" || request.IdempotencyKey == "" {
		return writeErrorWithContext(output, request.JSON, invalid("worker bootstrap requires project, session, task, controller session, human, and idempotency key"), terminalErrorContext{
			Hint: "Set OMG_PROJECT, OMG_SESSION_ID, OMG_TASK_ID, OMG_CONTROLLER_SESSION_ID, and OMG_HUMAN_ID or pass their matching options.",
			Next: "omg example show worker-bootstrap --json",
		})
	}
	if request.Runtime == "" {
		request.Runtime = "omp"
	}
	if request.Role == "" {
		request.Role = "worker"
	}

	selection := foundation.Selection{Project: request.Project}
	status, statusErr := application.Foundation.Status(ctx, selection, false)
	if statusErr.Code != "" {
		return writeError(output, request.JSON, statusErr)
	}
	if !status.Initialized {
		return writeErrorWithContext(output, request.JSON, domain.NewError(domain.CodeUninitialized, "project is not initialized", false), terminalErrorContext{Next: "omg init --project " + shellQuote(request.Project) + " --json"})
	}
	if status.Pending != 0 {
		automatic, automaticErr := application.Foundation.AutoMigrate(ctx, selection)
		if automaticErr.Code != "" {
			return writeError(output, request.JSON, automaticErr)
		}
		if automatic.Applied {
			status, statusErr = application.Foundation.Status(ctx, selection, false)
			if statusErr.Code != "" {
				return writeError(output, request.JSON, statusErr)
			}
		}
	}
	if status.Pending != 0 {
		return writeErrorWithContext(output, request.JSON, domain.NewError(domain.CodeUnavailable, "worker bootstrap stopped because schema migrations are pending", false), terminalErrorContext{Next: "omg migration plan --project " + shellQuote(request.Project) + " --json"})
	}

	taskOutcome := dispatchWorker(ctx, application.Dispatcher, request, "task.get", "", map[string]any{"task_id": request.TaskID})
	if taskOutcome.Error.Code != "" {
		return writeErrorWithContext(output, request.JSON, taskOutcome.Error, terminalErrorContext{Next: "omg task get --project " + shellQuote(request.Project) + " --payload '{\"task_id\":\"" + request.TaskID + "\"}' --json"})
	}
	var task workerTask
	if !decodeWorkerResult(taskOutcome.Data, &task) || task.ID == "" {
		return writeError(output, request.JSON, domain.NewError(domain.CodeInternal, "worker bootstrap could not decode task state", false))
	}

	boardOutcome := dispatchWorker(ctx, application.Dispatcher, request, "board.query", "", query.BoardRequest{Mode: query.BoardMe, SessionID: request.SessionID})
	sessionCreated := false
	if boardOutcome.Error.Code == domain.CodeNotFound {
		sessionOutcome := dispatchWorker(ctx, application.Dispatcher, request, "session.create", request.IdempotencyKey+"-session", map[string]any{
			"id": request.SessionID, "human_id": request.HumanID, "runtime": request.Runtime, "role": request.Role,
			"source_ref": "controller:" + request.ControllerSessionID, "task_id": request.TaskID, "worktree_ref": request.Project, "native_access_state": "unsupported",
		})
		if sessionOutcome.Error.Code != "" {
			return writeErrorWithContext(output, request.JSON, sessionOutcome.Error, terminalErrorContext{Next: "omg example show session-create --json"})
		}
		sessionCreated = true
	} else if boardOutcome.Error.Code != "" {
		return writeError(output, request.JSON, boardOutcome.Error)
	} else {
		snapshot, ok := decodeWorkerBoard(boardOutcome.Data)
		if !ok || snapshot.Identity == nil {
			return writeError(output, request.JSON, domain.NewError(domain.CodeInternal, "worker bootstrap could not decode existing session identity", false))
		}
		if snapshot.Identity.HumanID != request.HumanID {
			return writeErrorWithContext(output, request.JSON, domain.NewError(domain.CodeConflict, "worker session belongs to a different human", false), terminalErrorContext{Next: "omg board me --project " + shellQuote(request.Project) + " --session " + request.SessionID + " --json"})
		}
		if snapshot.Identity.TaskID != request.TaskID {
			return writeErrorWithContext(output, request.JSON, domain.NewError(domain.CodeConflict, "worker session is not linked to the requested task", false), terminalErrorContext{Hint: "Have the controller pre-register a task-bound worker session or choose the matching task.", Next: "omg board me --project " + shellQuote(request.Project) + " --session " + request.SessionID + " --json"})
		}
		if snapshot.Identity.ParentSessionID != "" && snapshot.Identity.ParentSessionID != request.ControllerSessionID {
			return writeErrorWithContext(output, request.JSON, domain.NewError(domain.CodeConflict, "worker session belongs to a different controller", false), terminalErrorContext{Next: "omg board me --project " + shellQuote(request.Project) + " --session " + request.SessionID + " --json"})
		}
	}

	taskClaimed := false
	switch task.State {
	case "READY":
		claimOutcome := dispatchWorker(ctx, application.Dispatcher, request, "task.claim", request.IdempotencyKey+"-claim", map[string]any{"task_id": request.TaskID, "session_id": request.SessionID})
		if claimOutcome.Error.Code != "" {
			return writeErrorWithContext(output, request.JSON, claimOutcome.Error, terminalErrorContext{Next: "omg board task --project " + shellQuote(request.Project) + " --task " + request.TaskID + " --json"})
		}
		taskClaimed = true
	case "CLAIMED", "IN_PROGRESS", "WAITING", "BLOCKED", "REWORK":
		if task.ClaimedBySessionID != request.SessionID {
			return writeErrorWithContext(output, request.JSON, domain.NewError(domain.CodeConflict, "task is owned by another session", false), terminalErrorContext{Next: "omg board task --project " + shellQuote(request.Project) + " --task " + request.TaskID + " --json"})
		}
	default:
		return writeErrorWithContext(output, request.JSON, domain.NewError(domain.CodeConflict, "task is not startable from its current state", false), terminalErrorContext{Next: "omg board task --project " + shellQuote(request.Project) + " --task " + request.TaskID + " --json"})
	}

	boardOutcome = dispatchWorker(ctx, application.Dispatcher, request, "board.query", "", query.BoardRequest{Mode: query.BoardMe, SessionID: request.SessionID})
	if boardOutcome.Error.Code != "" {
		return writeError(output, request.JSON, boardOutcome.Error)
	}
	board, ok := boardOutcome.Data.(query.ViewModel)
	if !ok {
		return writeError(output, request.JSON, domain.NewError(domain.CodeInternal, "worker bootstrap could not render the worker board", false))
	}
	inboxOutcome := dispatchWorker(ctx, application.Dispatcher, request, "message.inbox", "", map[string]any{"recipient": map[string]string{"session_id": request.SessionID}})
	if inboxOutcome.Error.Code != "" {
		return writeErrorWithContext(output, request.JSON, inboxOutcome.Error, terminalErrorContext{Next: "omg example show message-inbox --json"})
	}

	environment := map[string]string{
		"OMG_PROJECT": request.Project, "OMG_SESSION_ID": request.SessionID, "OMG_TASK_ID": request.TaskID,
		"OMG_CONTROLLER_SESSION_ID": request.ControllerSessionID, "OMG_HUMAN_ID": request.HumanID,
	}
	result := workerBootstrapResult{
		Healthy: true, Project: request.Project, SessionID: request.SessionID, TaskID: request.TaskID,
		ControllerSessionID: request.ControllerSessionID, HumanID: request.HumanID, SessionCreated: sessionCreated,
		TaskClaimed: taskClaimed, Environment: environment, Inbox: inboxOutcome.Data, Board: board,
		NextAction: workerNextAction{Code: "start_task", Command: "omg board me --project " + shellQuote(request.Project) + " --session " + request.SessionID + " --json", Argv: []string{"omg", "board", "me", "--project", request.Project, "--session", request.SessionID, "--json"}},
	}
	if request.Output != "" {
		content := renderWorkerEnvironment(environment)
		if err := writeNewPrivatePlan(request.Output, []byte(content)); err != nil {
			return writeErrorWithContext(output, request.JSON, domain.NewError(domain.CodeUnavailable, "worker bootstrap file could not be created", false), terminalErrorContext{Hint: "The destination must be a new safe owner-only file.", Next: "Choose a new --output path."})
		}
		result.BootstrapFile = request.Output
	}
	return writeSuccess(output, request.JSON, result)
}

func applyWorkerEnvironment(request *Request) {
	if request.Project == "" {
		request.Project = os.Getenv("OMG_PROJECT")
	}
	if request.SessionID == "" {
		request.SessionID = os.Getenv("OMG_SESSION_ID")
	}
	if request.TaskID == "" {
		request.TaskID = os.Getenv("OMG_TASK_ID")
	}
	if request.ControllerSessionID == "" {
		request.ControllerSessionID = os.Getenv("OMG_CONTROLLER_SESSION_ID")
	}
	if request.HumanID == "" {
		request.HumanID = os.Getenv("OMG_HUMAN_ID")
	}
	if request.Runtime == "" {
		request.Runtime = os.Getenv("OMG_RUNTIME")
	}
	if request.Role == "" {
		request.Role = os.Getenv("OMG_ROLE")
	}
}

func dispatchWorker(ctx context.Context, dispatcher app.Dispatcher, request Request, command, key string, payload any) app.Outcome {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return app.Outcome{Error: domain.NewError(domain.CodeInternal, "worker bootstrap payload could not be encoded", false)}
	}
	return dispatcher.Dispatch(ctx, app.Request{Version: app.RequestVersion, Command: command, Project: request.Project, IdempotencyKey: key, Payload: encoded})
}

func decodeWorkerResult(value any, target any) bool {
	encoded, err := json.Marshal(value)
	return err == nil && json.Unmarshal(encoded, target) == nil
}

func decodeWorkerBoard(value any) (query.BoardSnapshot, bool) {
	model, ok := value.(query.ViewModel)
	if !ok {
		return query.BoardSnapshot{}, false
	}
	var snapshot query.BoardSnapshot
	if json.Unmarshal(model.Data(), &snapshot) != nil {
		return query.BoardSnapshot{}, false
	}
	return snapshot, true
}

func renderWorkerEnvironment(environment map[string]string) string {
	keys := []string{"OMG_PROJECT", "OMG_SESSION_ID", "OMG_TASK_ID", "OMG_CONTROLLER_SESSION_ID", "OMG_HUMAN_ID"}
	var output strings.Builder
	output.WriteString("# Generated by omg worker bootstrap. Source this file; do not execute message or model text.\n")
	for _, key := range keys {
		fmt.Fprintf(&output, "export %s=%s\n", key, shellQuote(environment[key]))
	}
	return output.String()
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
