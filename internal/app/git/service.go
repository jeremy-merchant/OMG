// Package git persists read-only Git observations as advisory evidence.
package git

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jeremy-merchant/OMG/internal/domain"
	coord "github.com/jeremy-merchant/OMG/internal/domain/coordination"
	gitobs "github.com/jeremy-merchant/OMG/internal/domain/git"
	"github.com/jeremy-merchant/OMG/internal/domain/lineage"
	"github.com/jeremy-merchant/OMG/internal/ports"
	"github.com/jeremy-merchant/OMG/internal/safety"
)

type Service struct {
	store   ports.Store
	scanner ports.Scanner
	now     func() time.Time
}

func New(store ports.Store, scanner ports.Scanner, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{store: store, scanner: scanner, now: now}
}

type ScanRequest struct {
	ProjectID                domain.ProjectID
	SessionID, TaskID, RunID lineage.ID
	Directory                string
}
type ScanSummary struct {
	ObservationID   string            `json:"observation_id"`
	Hash            string            `json:"hash"`
	RepositoryState gitobs.RepoState  `json:"repository_state"`
	Confidence      gitobs.Confidence `json:"confidence"`
	AssetCount      int               `json:"asset_count"`
}

func invalid() error {
	return domain.NewError(domain.CodeInvalidArgument, "invalid git scan request", false)
}
func missing() error {
	return domain.NewError(domain.CodeNotFound, "git scan actor lineage not found", false)
}
func unavailable() error {
	return domain.NewError(domain.CodeUnavailable, "git observation unavailable", true)
}
func id() (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	return "git-observation-" + hex.EncodeToString(b[:]), nil
}

// ReconcileCurrent overlays canonical lineage and Git adoption facts onto an
// immutable observed snapshot. It performs no scan and persists no changes.
// Read-only projections, including boards, share this function with Current.
func ReconcileCurrent(ctx context.Context, r ports.Repositories, snapshot gitobs.Snapshot) (gitobs.Snapshot, error) {
	sessions, err := r.Coordination().ListSessions(ctx, lineage.ID(snapshot.ProjectID))
	if err != nil {
		return snapshot, err
	}
	candidates := make([]gitobs.OwnershipCandidate, 0, len(sessions))
	for _, session := range sessions {
		runs, err := r.Coordination().ListRunsForSession(ctx, session.ProjectID, session.ID)
		if err != nil {
			return snapshot, err
		}
		var selected lineage.TaskRun
		foundRun := false
		for _, run := range runs {
			if run.SessionID != session.ID {
				continue
			}
			if !foundRun || run.StartedAt.After(selected.StartedAt) || (run.StartedAt.Equal(selected.StartedAt) && run.ID > selected.ID) {
				selected, foundRun = run, true
			}
		}
		if !foundRun || selected.TaskID == "" {
			continue
		}
		task, ok, err := r.Coordination().GetTask(ctx, selected.TaskID)
		if err != nil {
			return snapshot, err
		}
		if !ok || task.ProjectID != session.ProjectID {
			continue
		}
		candidateSession := session
		candidateSession.TaskID = selected.TaskID
		candidates = append(candidates, gitobs.OwnershipCandidate{Session: candidateSession, Task: task, Run: selected})
	}
	adoptions := make([]coord.Adoption, 0, len(snapshot.Observation.Assets))
	for _, asset := range snapshot.Observation.Assets {
		adoption, ok, err := r.Coordination().LatestGitAdoption(ctx, string(snapshot.ProjectID), asset.StableFingerprint())
		if err != nil {
			return snapshot, err
		}
		if ok {
			adoptions = append(adoptions, adoption)
		}
	}
	observation, assets := gitobs.ReconcileOwnership(snapshot.Observation, candidates, adoptions)
	previous := make(map[string]gitobs.AssetRecord, len(snapshot.Assets))
	for _, record := range snapshot.Assets {
		previous[record.Fingerprint] = record
	}
	for i := range assets {
		if record, ok := previous[assets[i].Fingerprint]; ok {
			assets[i].FirstSeenAt, assets[i].LastSeenAt = record.FirstSeenAt, record.LastSeenAt
		}
	}
	snapshot.Observation, snapshot.Assets = observation, assets
	return snapshot, nil
}

// Scan runs Git before the transaction, then optionally validates complete
// actor lineage and commits one immutable observation with a safe receipt summary.
func (s *Service) Scan(ctx context.Context, key domain.IdempotencyKey, q ScanRequest) (domain.Result, error) {
	if !domain.IsSecretFreeStableMetadata(string(key)) || safety.RejectPrefixed(key, q) != nil {
		return domain.Result{}, invalid()
	}
	hasAttribution := q.SessionID != "" || q.TaskID != "" || q.RunID != ""
	if s == nil || s.store == nil || s.scanner == nil || key == "" || q.ProjectID == "" || q.Directory == "" || (hasAttribution && (q.SessionID == "" || q.TaskID == "" || q.RunID == "")) {
		return domain.Result{}, invalid()
	}
	observed, e := s.scanner.Scan(ctx, q.Directory)
	if e != nil {
		return domain.Result{}, unavailable()
	}
	if safety.RejectPrefixed(key, q, observed) != nil {
		return domain.Result{}, invalid()
	}
	if observed.Revision != gitobs.ObservationRevision {
		return domain.Result{}, unavailable()
	}
	oid, e := id()
	if e != nil {
		return domain.Result{}, unavailable()
	}
	at := s.now().UTC()
	_, out, e := s.store.Write(ctx, key, "git.inventory", func(r ports.Repositories) (domain.Result, error) {
		if hasAttribution {
			c := r.Coordination()
			session, ok, e := c.GetSession(ctx, q.SessionID)
			if e != nil {
				return domain.Result{}, e
			}
			if !ok || session.ProjectID != lineage.ID(q.ProjectID) {
				return domain.Result{}, missing()
			}
			task, ok, e := c.GetTask(ctx, q.TaskID)
			if e != nil {
				return domain.Result{}, e
			}
			if !ok || task.ProjectID != lineage.ID(q.ProjectID) {
				return domain.Result{}, missing()
			}
			run, ok, e := c.GetRun(ctx, q.RunID)
			if e != nil {
				return domain.Result{}, e
			}
			if !ok || run.TaskID != q.TaskID || run.SessionID != q.SessionID {
				return domain.Result{}, missing()
			}
		}
		pending, e := ReconcileCurrent(ctx, r, gitobs.Snapshot{ID: oid, ProjectID: q.ProjectID, IdempotencyKey: key, ActorSessionID: q.SessionID, TaskID: q.TaskID, RunID: q.RunID, Trigger: "scan", ObservedAt: at, Observation: observed})
		if e != nil {
			return domain.Result{}, e
		}
		snapshot, e := r.Git().CreateSnapshot(ctx, pending)
		if e != nil {
			return domain.Result{}, e
		}
		summary := ScanSummary{ObservationID: snapshot.ID, Hash: snapshot.Observation.Hash, RepositoryState: snapshot.Observation.Repository, Confidence: snapshot.Observation.Confidence, AssetCount: len(snapshot.Assets)}
		return domain.Result{ID: domain.ResultID(snapshot.ID), Outcome: domain.OutcomeOK, Data: summary}, nil
	})
	if e != nil {
		return domain.Result{}, mapErr(e)
	}
	return out, nil
}
func mapErr(e error) error {
	if e == nil {
		return nil
	}
	var d domain.DomainError
	if errors.As(e, &d) {
		return d
	}
	return unavailable()
}

func (s *Service) Get(ctx context.Context, project domain.ProjectID, id string) (gitobs.Snapshot, error) {
	if s == nil || s.store == nil || project == "" || id == "" {
		return gitobs.Snapshot{}, invalid()
	}
	var out gitobs.Snapshot
	err := s.store.Read(ctx, func(r ports.Repositories) error {
		var ok bool
		var err error
		out, ok, err = r.Git().GetSnapshot(ctx, project, id)
		if err != nil {
			return err
		}
		if !ok {
			return missing()
		}
		return nil
	})
	return out, mapErr(err)
}
func (s *Service) Latest(ctx context.Context, project domain.ProjectID) (gitobs.Snapshot, error) {
	if s == nil || s.store == nil || project == "" {
		return gitobs.Snapshot{}, invalid()
	}
	var out gitobs.Snapshot
	err := s.store.Read(ctx, func(r ports.Repositories) error {
		var ok bool
		var err error
		out, ok, err = r.Git().LatestSnapshot(ctx, project)
		if err != nil {
			return err
		}
		if !ok {
			return missing()
		}
		return nil
	})
	return out, mapErr(err)
}

// Current overlays latest durable scanner facts with current canonical lineage;
// it does not invoke the scanner or mutate Git.
func (s *Service) Current(ctx context.Context, project domain.ProjectID) (gitobs.Snapshot, error) {
	if s == nil || s.store == nil || project == "" {
		return gitobs.Snapshot{}, invalid()
	}
	var out gitobs.Snapshot
	err := s.store.Read(ctx, func(r ports.Repositories) error {
		snapshot, ok, err := r.Git().LatestSnapshot(ctx, project)
		if err != nil {
			return err
		}
		if !ok {
			return missing()
		}
		out, err = ReconcileCurrent(ctx, r, snapshot)
		return err
	})
	return out, mapErr(err)
}

// CleanupPlan returns advisory candidates and blocks only. It never invokes a
// scanner and contains no cleanup application capability.
func (s *Service) CleanupPlan(ctx context.Context, project domain.ProjectID, fingerprint string) (gitobs.CleanupPlan, error) {
	current, err := s.Current(ctx, project)
	if err != nil {
		return gitobs.CleanupPlan{}, err
	}
	facts := make([]gitobs.AssetFacts, 0, len(current.Assets))
	if fingerprint != "" {
		found := false
		for _, asset := range current.Assets {
			if asset.Fingerprint == fingerprint {
				facts = append(facts, asset.Facts)
				found = true
				break
			}
		}
		if !found {
			return gitobs.CleanupPlan{}, missing()
		}
	} else {
		for _, asset := range current.Assets {
			facts = append(facts, asset.Facts)
		}
	}
	return gitobs.Plan(facts), nil
}
func (s *Service) History(ctx context.Context, project domain.ProjectID) ([]gitobs.Snapshot, error) {
	if s == nil || s.store == nil || project == "" {
		return nil, invalid()
	}
	var out []gitobs.Snapshot
	err := s.store.Read(ctx, func(r ports.Repositories) error { var err error; out, err = r.Git().History(ctx, project); return err })
	return out, mapErr(err)
}
func (s *Service) Diff(ctx context.Context, project domain.ProjectID, before, after string) (gitobs.Diff, error) {
	left, err := s.Get(ctx, project, before)
	if err != nil {
		return gitobs.Diff{}, err
	}
	right, err := s.Get(ctx, project, after)
	if err != nil {
		return gitobs.Diff{}, err
	}
	return gitobs.ExactDiff(left.Observation, right.Observation), nil
}
