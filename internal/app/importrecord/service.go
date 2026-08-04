// Package importrecord applies normalized external records to canonical coordination state.
package importrecord

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	core "github.com/jeremy-merchant/oh-my-group/internal/domain/lineage"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
	"github.com/jeremy-merchant/oh-my-group/internal/safety"
)

type State string

const (
	StatePlanned   State = "planned"
	StateActive    State = "active"
	StateBlocked   State = "blocked"
	StateAmbiguous State = "ambiguous"
)

type Classification string

const (
	ClassificationImportedVerified   Classification = "imported_verified"
	ClassificationImportedUnverified Classification = "imported_unverified"
)

// Record is an opaque, normalized input. Its source record identifier is retained
// only in the private AgentSession.SourceRef field.
type Record struct {
	SourceRecordID string
	SourceState    State
	Title          string
	Runtime        string
	Role           string
	ParentTaskID   core.ID
}

type Result struct {
	SessionID      core.ID        `json:"session_id"`
	TaskID         core.ID        `json:"task_id"`
	State          core.TaskState `json:"state"`
	Classification Classification `json:"classification"`
}

type Service struct {
	store ports.Store
	now   func() time.Time
}

func New(store ports.Store, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{store: store, now: now}
}

func (r Record) validate() (core.TaskState, Classification, error) {
	if strings.TrimSpace(r.SourceRecordID) == "" || !domain.IsSecretFreeStableMetadata(r.SourceRecordID) || (r.ParentTaskID != "" && !domain.IsSecretFreeStableMetadata(string(r.ParentTaskID))) || strings.TrimSpace(r.Title) == "" || strings.TrimSpace(r.Runtime) == "" || strings.TrimSpace(r.Role) == "" {
		return "", "", invalid()
	}
	switch r.SourceState {
	case StatePlanned:
		return core.TaskReady, ClassificationImportedVerified, nil
	case StateActive:
		return core.TaskInProgress, ClassificationImportedVerified, nil
	case StateBlocked:
		return core.TaskBlocked, ClassificationImportedVerified, nil
	case StateAmbiguous:
		return core.TaskReady, ClassificationImportedUnverified, nil
	default:
		return "", "", invalid()
	}
}

// Apply creates the imported session and its task in one receipt-backed Store.Write.
func (s *Service) Apply(ctx context.Context, key domain.IdempotencyKey, project core.ID, record Record) (Result, error) {
	if !domain.IsSecretFreeStableMetadata(string(key)) || safety.RejectPrefixed(key, record, project) != nil {
		return Result{}, invalid()
	}
	state, classification, err := record.validate()
	if err != nil || project == "" || s == nil || s.store == nil {
		return Result{}, invalid()
	}
	sessionID, err := newID("session_")
	if err != nil {
		return Result{}, unavailable()
	}
	taskID, err := newID("task_")
	if err != nil {
		return Result{}, unavailable()
	}
	now := s.now().UTC()
	session := core.AgentSession{
		ID: sessionID, ProjectID: project, Kind: core.Imported, Runtime: record.Runtime, Role: record.Role,
		Source: core.SourceImport, SourceRef: record.SourceRecordID, RootID: sessionID, TaskID: taskID,
		StartedAt: now, NativeAccessState: core.NativeAccessUnsupported,
	}
	task := core.Task{
		ID: taskID, ProjectID: project, DisplayNumber: 1, Title: record.Title, State: state, CreatedBySessionID: sessionID,
		ParentTaskID: record.ParentTaskID, CreatedAt: now, UpdatedAt: now,
	}
	if session.Validate() != nil || task.Validate() != nil {
		return Result{}, invalid()
	}
	wanted := Result{SessionID: sessionID, TaskID: taskID, State: state, Classification: classification}
	_, persisted, err := s.store.Write(ctx, key, "import.record", func(repositories ports.Repositories) (domain.Result, error) {
		if record.ParentTaskID != "" {
			parent, ok, getErr := repositories.Coordination().GetTask(ctx, record.ParentTaskID)
			if getErr != nil {
				return domain.Result{}, getErr
			}
			if !ok || parent.ProjectID != project {
				return domain.Result{}, invalid()
			}
		}
		if createErr := repositories.Coordination().CreateSession(ctx, session); createErr != nil {
			return domain.Result{}, createErr
		}
		created, createErr := repositories.Coordination().CreateTask(ctx, task)
		if createErr != nil {
			return domain.Result{}, createErr
		}
		wanted.TaskID = created.ID
		wanted.State = created.State
		return domain.Result{ID: domain.ResultID(created.ID), Outcome: domain.OutcomeOK, Data: wanted}, nil
	})
	if err != nil {
		return Result{}, mapErr(err)
	}
	var out Result
	encoded, marshalErr := json.Marshal(persisted.Data)
	if marshalErr != nil || json.Unmarshal(encoded, &out) != nil || out.SessionID == "" || out.TaskID == "" {
		return Result{}, unavailable()
	}
	return out, nil
}

func newID(prefix string) (core.ID, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return core.ID(prefix + base64.RawURLEncoding.EncodeToString(bytes)), nil
}

func invalid() error {
	return domain.NewError(domain.CodeInvalidArgument, "invalid import record", false)
}

func unavailable() error {
	return domain.NewError(domain.CodeUnavailable, "import record store unavailable", true)
}

func mapErr(err error) error {
	var domainErr domain.DomainError
	if errors.As(err, &domainErr) {
		return domainErr
	}
	return unavailable()
}
