package app

import (
	"context"

	"github.com/jeremy-merchant/oh-my-group/internal/app/foundation"
	reservationapp "github.com/jeremy-merchant/oh-my-group/internal/app/reservation"
	workersetupapp "github.com/jeremy-merchant/oh-my-group/internal/app/workersetup"
	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	"github.com/jeremy-merchant/oh-my-group/internal/domain/lineage"
	res "github.com/jeremy-merchant/oh-my-group/internal/domain/reservation"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
)

type workerSetupPayload struct {
	HumanID             string                       `json:"human_id"`
	ControllerSessionID string                       `json:"controller_session_id"`
	SessionID           string                       `json:"session_id"`
	Runtime             string                       `json:"runtime"`
	Role                string                       `json:"role"`
	TaskID              string                       `json:"task_id"`
	TaskTitle           string                       `json:"task_title"`
	ParentTaskID        string                       `json:"parent_task_id,omitempty"`
	CompletionPolicy    string                       `json:"completion_policy,omitempty"`
	ParentRequirement   string                       `json:"parent_requirement,omitempty"`
	RunID               string                       `json:"run_id"`
	Reservations        []reserveBatchAddItemPayload `json:"reservations"`
}

func (d *ServiceDispatcher) dispatchWorker(ctx context.Context, request Request, selection foundation.Selection) (Outcome, bool) {
	if request.Command != "worker.setup" {
		return Outcome{}, false
	}
	if request.IdempotencyKey == "" {
		return Outcome{Error: invalidRequest()}, true
	}
	var payload workerSetupPayload
	if !decodePayload(request.Payload, &payload) {
		return Outcome{Error: invalidRequest()}, true
	}
	items := make([]reservationapp.BatchCreateItem, len(payload.Reservations))
	for i, item := range payload.Reservations {
		pattern, err := res.NewPattern(res.PatternKind(item.PatternKind), item.Pattern, res.CaseSensitivity(item.CaseSensitivity))
		if err != nil {
			return Outcome{Error: invalidRequest()}, true
		}
		items[i] = reservationapp.BatchCreateItem{
			ID: item.ID, Pattern: pattern, Mode: res.Mode(item.Mode), Intent: item.Intent,
			TTL: durationFromSeconds(item.TTLSeconds),
		}
	}
	var result workersetupapp.Result
	err := d.service.WithCurrentStore(ctx, selection, func(resolved ports.ResolvedStore, store ports.Store) error {
		var setupErr error
		result, setupErr = workersetupapp.New(store, nil).Setup(ctx, domain.IdempotencyKey(request.IdempotencyKey), workersetupapp.Request{
			ProjectID: domain.ProjectID(resolved.Project), ProjectRoot: resolved.ProjectRoot,
			HumanID: lineage.ID(payload.HumanID), ControllerSessionID: lineage.ID(payload.ControllerSessionID),
			SessionID: lineage.ID(payload.SessionID), Runtime: payload.Runtime, Role: payload.Role,
			TaskID: lineage.ID(payload.TaskID), TaskTitle: payload.TaskTitle, ParentTaskID: lineage.ID(payload.ParentTaskID),
			CompletionPolicy:  lineage.TaskCompletionPolicy(payload.CompletionPolicy),
			ParentRequirement: lineage.TaskParentRequirement(payload.ParentRequirement),
			RunID:             lineage.ID(payload.RunID), Reservations: items,
		})
		return setupErr
	})
	return outcome(result, err), true
}
