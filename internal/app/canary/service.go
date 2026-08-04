// Package canary records exact-revision verification receipts without
// executing the verification command itself.
package canary

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	handoffapp "github.com/jeremy-merchant/oh-my-group/internal/app/handoff"
	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	coord "github.com/jeremy-merchant/oh-my-group/internal/domain/coordination"
	gitobs "github.com/jeremy-merchant/oh-my-group/internal/domain/git"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
	"github.com/jeremy-merchant/oh-my-group/internal/safety"
)

type Service struct {
	store    ports.Store
	verifier ports.GitVerifier
	now      func() time.Time
}

type StartRequest struct {
	ProjectID              domain.ProjectID
	Directory              string
	HandoffID              string
	ActorSessionID         string
	IntegrationRef         string
	Mode                   string
	CandidateSHA           string
	Command                string
	ExecutionKind          string
	EnvironmentFingerprint string
}

type StartMode string

const (
	ModeLocalIntegration    StartMode = "local_integration"
	ModeReleaseOrProduction StartMode = "release_or_production"
)

const (
	ledgerReconciled = "reconciled"
	ledgerPending    = "pending_ledger_reconciliation"
)

type localIntegrationVerifier interface {
	VerifyLocalIntegration(context.Context, string, string, string) (gitobs.LocalIntegrationEvidence, error)
}

type FinishRequest struct {
	ProjectID      domain.ProjectID
	Directory      string
	CanaryRunID    string
	ActorSessionID string
	ExitCode       int
	PassedCount    int
	FailedCount    int
	SkippedCount   int
	EvidencePath   string
}

func New(store ports.Store, verifier ports.GitVerifier, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{store: store, verifier: verifier, now: now}
}

func invalid() error {
	return domain.NewError(domain.CodeInvalidArgument, "invalid exact-SHA canary request", false)
}
func missing() error {
	return domain.NewError(domain.CodeNotFound, "canary or handoff not found", false)
}
func unavailable() error {
	return domain.NewError(domain.CodeUnavailable, "Git verification is unavailable", true)
}
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	var typed domain.DomainError
	if errors.As(err, &typed) {
		return typed
	}
	return domain.NewError(domain.CodeUnavailable, "canary store unavailable", true)
}
func eventID(prefix string, key domain.IdempotencyKey) string {
	sum := sha256.Sum256([]byte(prefix + "\x00" + string(key)))
	return prefix + "-" + hex.EncodeToString(sum[:16])
}

func startMode(value string) (StartMode, bool) {
	switch StartMode(value) {
	case "", ModeReleaseOrProduction:
		return ModeReleaseOrProduction, true
	case ModeLocalIntegration:
		return ModeLocalIntegration, true
	default:
		return "", false
	}
}

func (s *Service) Start(ctx context.Context, key domain.IdempotencyKey, request StartRequest) (coord.HandoffLifecycleEvent, error) {
	mode, validMode := startMode(request.Mode)
	localCandidateMissing := mode == ModeLocalIntegration && request.CandidateSHA == ""
	strictCandidateSupplied := mode == ModeReleaseOrProduction && request.CandidateSHA != ""
	if s == nil || s.store == nil || s.verifier == nil || !validMode || localCandidateMissing || strictCandidateSupplied || !domain.IsSecretFreeStableMetadata(string(key)) || request.ProjectID == "" || request.Directory == "" || request.HandoffID == "" || request.ActorSessionID == "" || request.IntegrationRef == "" || request.Command == "" || request.EnvironmentFingerprint == "" || request.ExecutionKind != "real" && request.ExecutionKind != "mock" || safety.RejectPrefixed(key, request) != nil {
		return coord.HandoffLifecycleEvent{}, invalid()
	}
	startID := eventID("canary-start", key)
	if existing, found, err := s.existingEvent(ctx, startID); err != nil {
		return coord.HandoffLifecycleEvent{}, err
	} else if found {
		return existing, nil
	}
	events, err := handoffapp.New(s.store, nil).Lifecycle(ctx, request.HandoffID)
	if err != nil {
		return coord.HandoffLifecycleEvent{}, err
	}
	var integrationCommit string
	for _, event := range events {
		if event.IntegrationCommit != "" {
			integrationCommit = event.IntegrationCommit
		}
	}
	var revision gitobs.RevisionEvidence
	note := ""
	if mode == ModeReleaseOrProduction {
		if integrationCommit == "" {
			return coord.HandoffLifecycleEvent{}, invalid()
		}
		revision, err = s.verifier.ResolveRevision(ctx, request.Directory, request.IntegrationRef)
		if err != nil {
			return coord.HandoffLifecycleEvent{}, unavailable()
		}
		if revision.Commit != integrationCommit || revision.Tree == "" || revision.RefFingerprint == "" {
			return coord.HandoffLifecycleEvent{}, domain.NewError(domain.CodeConflict, "integration ref does not match the recorded exact SHA", false)
		}
	} else {
		localVerifier, ok := s.verifier.(localIntegrationVerifier)
		if !ok {
			return coord.HandoffLifecycleEvent{}, unavailable()
		}
		evidence, verifyErr := localVerifier.VerifyLocalIntegration(ctx, request.Directory, request.CandidateSHA, request.IntegrationRef)
		if verifyErr != nil {
			return coord.HandoffLifecycleEvent{}, unavailable()
		}
		if evidence.Candidate.Commit != request.CandidateSHA || evidence.Candidate.Tree == "" {
			return coord.HandoffLifecycleEvent{}, domain.NewError(domain.CodeConflict, "candidate SHA was not supplied as an exact commit", false)
		}
		if !evidence.CandidateReachable {
			return coord.HandoffLifecycleEvent{}, domain.NewError(domain.CodeConflict, "candidate SHA is not reachable from the rolling integration ref", false)
		}
		if !evidence.WorktreeClean {
			return coord.HandoffLifecycleEvent{}, domain.NewError(domain.CodeConflict, "rolling integration worktree is not clean", false)
		}
		revision = evidence.Rolling
		if revision.Commit == "" || revision.Tree == "" || revision.RefFingerprint == "" {
			return coord.HandoffLifecycleEvent{}, unavailable()
		}
		ledgerStatus := ledgerReconciled
		ledgerWarning := ""
		switch {
		case integrationCommit == "":
			ledgerStatus = ledgerPending
			ledgerWarning = "missing_integrated_event"
		case integrationCommit != evidence.Candidate.Commit:
			ledgerStatus = ledgerPending
			ledgerWarning = "integration_commit_mismatch"
		}
		note = fmt.Sprintf("canary_mode=%s;candidate_sha=%s;ledger_status=%s", mode, evidence.Candidate.Commit, ledgerStatus)
		if ledgerWarning != "" {
			note += ";ledger_warning=" + ledgerWarning
		}
	}
	started := s.now().UTC()
	event := coord.HandoffLifecycleEvent{
		ID:                           startID,
		HandoffID:                    request.HandoffID,
		ActorSessionID:               request.ActorSessionID,
		State:                        coord.IntegrationCanaryRunning,
		CanaryRunID:                  eventID("canary", key),
		CanaryIntegrationRef:         request.IntegrationRef,
		CanaryTargetSHA:              revision.Commit,
		CanaryTargetTree:             revision.Tree,
		CanaryCommand:                request.Command,
		CanaryExecutionKind:          request.ExecutionKind,
		CanaryEnvironmentFingerprint: request.EnvironmentFingerprint,
		CanaryHeadBefore:             revision.Commit,
		CanaryRefFingerprintBefore:   revision.RefFingerprint,
		CanaryStartedAt:              &started,
		Note:                         note,
	}
	if mode == ModeLocalIntegration {
		return handoffapp.New(s.store, s.now).AdvanceLocalCanary(ctx, key, event)
	}
	return handoffapp.New(s.store, s.now).Advance(ctx, key, event)
}

func (s *Service) Finish(ctx context.Context, key domain.IdempotencyKey, request FinishRequest) (coord.HandoffLifecycleEvent, error) {
	if s == nil || s.store == nil || s.verifier == nil || !domain.IsSecretFreeStableMetadata(string(key)) || request.ProjectID == "" || request.Directory == "" || request.CanaryRunID == "" || request.ActorSessionID == "" || request.PassedCount < 0 || request.FailedCount < 0 || request.SkippedCount < 0 || safety.RejectPrefixed(key, request) != nil {
		return coord.HandoffLifecycleEvent{}, invalid()
	}
	finishID := eventID("canary-finish", key)
	if existing, found, err := s.existingEvent(ctx, finishID); err != nil {
		return coord.HandoffLifecycleEvent{}, err
	} else if found {
		return existing, nil
	}
	start, found, err := s.findStart(ctx, request.ProjectID, request.CanaryRunID)
	if err != nil {
		return coord.HandoffLifecycleEvent{}, err
	}
	if !found {
		return coord.HandoffLifecycleEvent{}, missing()
	}
	current, err := s.verifier.ResolveRevision(ctx, request.Directory, start.CanaryIntegrationRef)
	if err != nil {
		return coord.HandoffLifecycleEvent{}, unavailable()
	}
	finished := s.now().UTC()
	exitCode := request.ExitCode
	state, result := classify(start, current, request)
	event := coord.HandoffLifecycleEvent{
		ID:                           finishID,
		HandoffID:                    start.HandoffID,
		ActorSessionID:               request.ActorSessionID,
		State:                        state,
		CanaryRunID:                  start.CanaryRunID,
		CanaryIntegrationRef:         start.CanaryIntegrationRef,
		CanaryTargetSHA:              start.CanaryTargetSHA,
		CanaryTargetTree:             start.CanaryTargetTree,
		CanaryResult:                 result,
		CanaryCommand:                start.CanaryCommand,
		CanaryExecutionKind:          start.CanaryExecutionKind,
		CanaryEnvironmentFingerprint: start.CanaryEnvironmentFingerprint,
		CanaryHeadBefore:             start.CanaryHeadBefore,
		CanaryHeadAfter:              current.Commit,
		CanaryRefFingerprintBefore:   start.CanaryRefFingerprintBefore,
		CanaryRefFingerprintAfter:    current.RefFingerprint,
		CanaryExitCode:               &exitCode,
		CanaryPassedCount:            request.PassedCount,
		CanaryFailedCount:            request.FailedCount,
		CanarySkippedCount:           request.SkippedCount,
		CanaryStartedAt:              start.CanaryStartedAt,
		CanaryFinishedAt:             &finished,
		CanaryEvidencePath:           request.EvidencePath,
		Note:                         start.Note,
	}
	return handoffapp.New(s.store, s.now).Advance(ctx, key, event)
}

func (s *Service) existingEvent(ctx context.Context, id string) (coord.HandoffLifecycleEvent, bool, error) {
	var event coord.HandoffLifecycleEvent
	var found bool
	err := s.store.Read(ctx, func(repositories ports.Repositories) error {
		var readErr error
		event, found, readErr = repositories.Coordination().GetHandoffLifecycleEventByID(ctx, id)
		return readErr
	})
	if err != nil {
		return coord.HandoffLifecycleEvent{}, false, mapErr(err)
	}
	return event, found, nil
}

func (s *Service) findStart(ctx context.Context, project domain.ProjectID, runID string) (coord.HandoffLifecycleEvent, bool, error) {
	var start coord.HandoffLifecycleEvent
	found := false
	err := s.store.Read(ctx, func(repositories ports.Repositories) error {
		tasks, readErr := repositories.Coordination().ListTasks(ctx, project)
		if readErr != nil {
			return readErr
		}
		for _, task := range tasks {
			handoffs, readErr := repositories.Coordination().ListHandoffs(ctx, string(task.ID))
			if readErr != nil {
				return readErr
			}
			for _, handoff := range handoffs {
				events, readErr := repositories.Coordination().ListHandoffLifecycleEvents(ctx, handoff.ID)
				if readErr != nil {
					return readErr
				}
				for _, event := range events {
					if event.CanaryRunID != runID {
						continue
					}
					if event.State == coord.IntegrationCanaryRunning {
						start, found = event, true
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return coord.HandoffLifecycleEvent{}, false, mapErr(err)
	}
	return start, found, nil
}

func classify(start coord.HandoffLifecycleEvent, current gitobs.RevisionEvidence, request FinishRequest) (coord.IntegrationState, string) {
	if current.Commit != start.CanaryTargetSHA || current.Tree != start.CanaryTargetTree || current.RefFingerprint == "" || current.RefFingerprint != start.CanaryRefFingerprintBefore {
		return coord.IntegrationCanaryInvalid, "INCONCLUSIVE"
	}
	if request.ExitCode != 0 || request.FailedCount != 0 {
		return coord.IntegrationCanaryFailed, "FAIL"
	}
	if request.PassedCount == 0 {
		if request.SkippedCount != 0 {
			return coord.IntegrationCanarySkipped, "SKIPPED"
		}
		return coord.IntegrationCanaryInvalid, "INCONCLUSIVE"
	}
	if start.CanaryExecutionKind == "mock" {
		return coord.IntegrationCanaryMock, "PASS_MOCK"
	}
	return coord.IntegrationCanaryPassed, "PASS_REAL"
}
