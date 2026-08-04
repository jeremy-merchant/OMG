package cli

import (
	"context"
	"github.com/jeremy-merchant/oh-my-group/internal/app"
	"io"

	"github.com/jeremy-merchant/oh-my-group/internal/app/foundation"
)

func runRestorePlan(ctx context.Context, output io.Writer, input io.Reader, request Request, service app.Foundation, selection foundation.Selection) int {
	if request.Name != "backup" || request.Subcommand != "restore" ||
		request.IdempotencyKey != "" || request.Integrity || request.Status || request.Stdio || request.Runtime != "" ||
		len(request.Command) != 0 || request.Output != "" || request.PlanFile != "" || request.ApprovalFile != "" ||
		request.Format != "" || request.SessionID != "" || request.TaskID != "" {
		return writeInvalidRequest(output, request, "backup restore plan request is invalid")
	}
	rawPayload, err := loadApplicationPayload(request, input)
	if err != nil {
		return writeError(output, request.JSON, invalid("backup restore plan payload is invalid"))
	}
	var payload foundation.RestorePlanRequest
	if !decodePayload(rawPayload, &payload) {
		return writeError(output, request.JSON, invalid("backup restore plan payload is invalid"))
	}
	plan, planErr := service.PlanRestore(ctx, selection, payload)
	if planErr.Code != "" {
		return writeError(output, request.JSON, planErr)
	}
	return writeSuccess(output, request.JSON, plan)
}
