package cli

import (
	"context"
	"encoding/json"
	"io"

	"github.com/jeremy-merchant/OMG/internal/app"
	"github.com/jeremy-merchant/OMG/internal/app/foundation"
	"github.com/jeremy-merchant/OMG/internal/domain"
	"github.com/jeremy-merchant/OMG/internal/view"
)

func validPreflightRequest(request Request) bool {
	if request.Subcommand != "" || request.Integrity || request.Status || request.Stdio ||
		request.runtimeProvided || request.Runtime != "" || len(request.Command) != 0 ||
		request.outputProvided || request.Output != "" || request.planFileProvided ||
		request.PlanFile != "" || request.approvalFileProvided || request.ApprovalFile != "" ||
		request.idempotencyKeyProvided || request.IdempotencyKey != "" || request.formatProvided ||
		request.Format != "" || request.taskProvided || request.TaskID != "" ||
		request.PayloadProvided || request.Payload != "" || request.PayloadFileProvided ||
		request.PayloadFile != "" || request.PayloadStdin {
		return false
	}
	return !request.sessionProvided || request.SessionID != ""
}

func runPreflight(ctx context.Context, output io.Writer, request Request, application app.CLIService, selection foundation.Selection) int {
	if !validPreflightRequest(request) {
		return writeInvalidRequest(output, request, "preflight request is invalid")
	}
	payload, err := json.Marshal(app.PreflightRequest{SessionID: request.SessionID, Verbose: request.Verbose})
	if err != nil {
		return writeError(output, request.JSON, domain.NewError(domain.CodeInternal, "unable to encode preflight query", false))
	}
	outcome := application.Dispatcher.Dispatch(ctx, app.Request{
		Version:   app.RequestVersion,
		Command:   "preflight.query",
		Project:   selection.Project,
		Workspace: selection.Workspace,
		Store:     selection.Store,
		Payload:   payload,
	})
	if outcome.Error.Code != "" {
		return writeError(output, request.JSON, outcome.Error)
	}
	result, ok := outcome.Data.(app.PreflightView)
	if !ok {
		return writeError(output, request.JSON, domain.NewError(domain.CodeInternal, "preflight query returned an invalid projection", false))
	}
	if request.JSON {
		return writeSuccess(output, true, result)
	}
	_, _ = io.WriteString(output, view.RenderPreflightTTYWithOptions(result, cliTerminalWidth(output), cliTerminalColorEnabled(output)))
	return ExitSuccess
}
