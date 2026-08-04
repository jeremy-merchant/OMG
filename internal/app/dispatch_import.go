package app

import (
	"context"
	"github.com/jeremy-merchant/oh-my-group/internal/app/foundation"
	importapp "github.com/jeremy-merchant/oh-my-group/internal/app/importrecord"
	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	core "github.com/jeremy-merchant/oh-my-group/internal/domain/lineage"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
)

// dispatchImport handles the "import.record" command. Returns (Outcome{}, false)
// when the idempotency key is blank so the caller can fall through to other slices;
// otherwise enforces strict payload and delegates to the importrecord service.
func (d *ServiceDispatcher) dispatchImport(ctx context.Context, request Request, selection foundation.Selection) (Outcome, bool) {
	if request.IdempotencyKey == "" {
		return Outcome{Error: invalidRequest()}, true
	}

	var payload ImportRecordPayload
	if !decodePayload(request.Payload, &payload) || payload.SourceRecordID == "" ||
		payload.SourceState == "" || payload.Title == "" || payload.Runtime == "" || payload.Role == "" {
		return Outcome{Error: invalidRequest()}, true
	}

	var result ImportRecordResult
	err := d.service.WithCurrentStore(ctx, selection, func(resolved ports.ResolvedStore, store ports.Store) error {
		res, err := importapp.New(store, nil).Apply(ctx, domain.IdempotencyKey(request.IdempotencyKey), core.ID(resolved.Project), importapp.Record{
			SourceRecordID: payload.SourceRecordID,
			SourceState:    importapp.State(payload.SourceState),
			Title:          payload.Title,
			Runtime:        payload.Runtime,
			Role:           payload.Role,
			ParentTaskID:   core.ID(payload.ParentTaskID),
		})
		if err != nil {
			return err
		}
		result = ImportRecordResult{
			SessionID:      string(res.SessionID),
			TaskID:         string(res.TaskID),
			State:          string(res.State),
			Classification: string(res.Classification),
		}
		return nil
	})
	return outcome(result, err), true
}

// ImportRecordPayload mirrors the fields accepted by importrecord.Apply.
// decodePayload already rejects unknown keys, so no extra whitelist is needed.
type ImportRecordPayload struct {
	SourceRecordID string `json:"source_record_id"`
	SourceState    string `json:"source_state"`
	Title          string `json:"title"`
	Runtime        string `json:"runtime"`
	Role           string `json:"role"`
	ParentTaskID   string `json:"parent_task_id,omitempty"`
}

// ImportRecordResult is the privacy-safe DTO returned to all transports.
type ImportRecordResult struct {
	SessionID      string `json:"session_id"`
	TaskID         string `json:"task_id"`
	State          string `json:"state"`
	Classification string `json:"classification"`
}
