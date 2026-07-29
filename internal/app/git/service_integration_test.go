package git

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jeremy-merchant/OMG/internal/app/testsupport"
	"github.com/jeremy-merchant/OMG/internal/domain"
	gitobs "github.com/jeremy-merchant/OMG/internal/domain/git"
	core "github.com/jeremy-merchant/OMG/internal/domain/lineage"
	"github.com/jeremy-merchant/OMG/internal/ports"
)

type integrationScanner struct {
	observation gitobs.Observation
	err         error
	calls       int
}

func (s *integrationScanner) Scan(_ context.Context, _ string) (gitobs.Observation, error) {
	s.calls++
	return s.observation, s.err
}

func TestServiceScanRejectsDelegationTokenSplitBetweenKeyAndObservedBranchBeforePersistence(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	store, db := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	key := domain.IdempotencyKey("omgdt_")
	observed := serviceObservation("/repo", "head")
	observed.Assets[0].Facts.Branch = "v1_" + strings.Repeat("a", 43)
	scanner := &integrationScanner{observation: observed}
	service := New(store, scanner, func() time.Time { return now })

	if _, err := service.Scan(context.Background(), key, ScanRequest{ProjectID: testsupport.Project, Directory: "/repo"}); err == nil {
		t.Fatal("delegation token split between key and observed branch was accepted")
	}
	if scanner.calls != 1 {
		t.Fatalf("scanner calls = %d, want 1", scanner.calls)
	}
	for _, query := range []string{
		"SELECT COUNT(*) FROM git_observations",
		"SELECT COUNT(*) FROM command_receipts WHERE idempotency_key=?",
		"SELECT COUNT(*) FROM audit_events a JOIN command_receipts r ON r.id=a.receipt_id WHERE r.idempotency_key=?",
	} {
		var count int
		if strings.Contains(query, "idempotency_key") {
			if err := db.QueryRow(query, key).Scan(&count); err != nil {
				t.Fatal(err)
			}
		} else if err := db.QueryRow(query).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("rejected split token persisted %d rows for %q", count, query)
		}
	}
}

func TestServiceGitInventoryScanReplayAndQueries(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	store, _ := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	scanner := &integrationScanner{observation: serviceObservation("/private/first", "head-one")}
	service := New(store, scanner, func() time.Time { return now })
	request := ScanRequest{ProjectID: testsupport.Project, SessionID: "source", TaskID: "a", RunID: "run", Directory: "/private/first"}
	first, err := service.Scan(context.Background(), "git-replay", request)
	if err != nil {
		t.Fatal(err)
	}
	scanner.observation = serviceObservation("/private/second", "head-two")
	replay, err := service.Scan(context.Background(), "git-replay", request)
	if err != nil {
		t.Fatal(err)
	}
	if scanner.calls != 2 {
		t.Fatalf("scanner calls = %d; scan must execute before replay transaction", scanner.calls)
	}
	left := decodeScanSummary(t, first.Data)
	right := decodeScanSummary(t, replay.Data)
	if left != right {
		t.Fatalf("replay summary = %+v; want canonical %+v", right, left)
	}
	got, err := service.Get(context.Background(), testsupport.Project, left.ObservationID)
	if err != nil || got.Observation.Hash != left.Hash {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	latest, err := service.Latest(context.Background(), testsupport.Project)
	if err != nil || latest.ID != left.ObservationID {
		t.Fatalf("Latest = %+v, %v", latest, err)
	}
	history, err := service.History(context.Background(), testsupport.Project)
	if err != nil || len(history) != 1 || history[0].ID != left.ObservationID {
		t.Fatalf("History = %+v, %v", history, err)
	}
	diff, err := service.Diff(context.Background(), testsupport.Project, left.ObservationID, left.ObservationID)
	if err != nil || len(diff.New)+len(diff.Missing)+len(diff.Changed) != 0 {
		t.Fatalf("Diff = %+v, %v", diff, err)
	}
}

func TestServiceGitInventoryScansUnattributedFreshProject(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	store, _ := testsupport.Store(t, now)
	observation := serviceObservation("/private/orphan", "orphan-head")
	observation.Assets[0].Facts.Status.TrackedDirty = 1
	observation.Hash = gitobs.HashObservation(observation)
	service := New(store, &integrationScanner{observation: observation}, func() time.Time { return now })

	result, err := service.Scan(context.Background(), "unattributed-inventory", ScanRequest{ProjectID: testsupport.Project, Directory: "/private/orphan"})
	if err != nil {
		t.Fatal(err)
	}
	summary := decodeScanSummary(t, result.Data)
	snapshot, err := service.Get(context.Background(), testsupport.Project, summary.ObservationID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ActorSessionID != "" || snapshot.TaskID != "" || snapshot.RunID != "" {
		t.Fatalf("unattributed lineage = %#v", snapshot)
	}
	if len(snapshot.Assets) != 1 || !snapshot.Assets[0].Classification.Has(gitobs.ClassDirtyUnowned) {
		t.Fatalf("classified assets = %#v", snapshot.Assets)
	}
}

func TestServiceGitInventoryRejectsPartialOrInvalidAttribution(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	store, _ := testsupport.Store(t, now)
	service := New(store, &integrationScanner{observation: serviceObservation("/private/orphan", "orphan-head")}, func() time.Time { return now })

	for _, request := range []ScanRequest{
		{ProjectID: testsupport.Project, SessionID: "session", Directory: "/private/orphan"},
		{ProjectID: testsupport.Project, TaskID: "task", Directory: "/private/orphan"},
		{ProjectID: testsupport.Project, RunID: "run", Directory: "/private/orphan"},
		{ProjectID: testsupport.Project, SessionID: "missing", TaskID: "task", RunID: "run", Directory: "/private/orphan"},
	} {
		_, err := service.Scan(context.Background(), domain.IdempotencyKey("invalid-attribution-"+string(request.SessionID)+string(request.TaskID)+string(request.RunID)), request)
		if request.SessionID == "missing" {
			assertServiceCode(t, err, domain.CodeNotFound)
		} else {
			assertServiceCode(t, err, domain.CodeInvalidArgument)
		}
	}
}
func TestServiceScanPersistsExactAutomaticOwner(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	store, _ := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	var ownerTaskA, ownerTaskB core.Task
	var ownerSession core.AgentSession
	const path = "/private/persisted-owner"
	_, _, err := store.Write(ctx, "seed-persisted-owner", "test.write", func(r ports.Repositories) (domain.Result, error) {
		c := r.Coordination()
		var createErr error
		ownerTaskA, createErr = c.CreateTask(ctx, core.Task{ID: "owner-task-a", ProjectID: testsupport.Project, Title: "owner A", State: core.TaskClaimed, CreatedBySessionID: "source", CreatedAt: now, UpdatedAt: now})
		if createErr != nil {
			return domain.Result{}, createErr
		}
		ownerSession = core.AgentSession{ID: "owner-session", ProjectID: testsupport.Project, HumanID: "human", Kind: core.HumanDirect, Runtime: "test", Role: "owner", Source: core.SourceHuman, SourceRef: "test", RootID: "owner-session", TaskID: ownerTaskA.ID, WorktreeRef: path, StartedAt: now}
		if createErr := c.CreateSession(ctx, ownerSession); createErr != nil {
			return domain.Result{}, createErr
		}
		ownerTaskB, createErr = c.CreateTask(ctx, core.Task{ID: "owner-task-b", ProjectID: testsupport.Project, Title: "owner B", State: core.TaskClaimed, CreatedBySessionID: "source", CreatedAt: now, UpdatedAt: now})
		if createErr != nil {
			return domain.Result{}, createErr
		}
		for _, ownerRun := range []core.TaskRun{
			{ID: "owner-run-old", TaskID: ownerTaskA.ID, SessionID: "owner-session", State: core.RunRunning, StartedAt: now},
			{ID: "owner-run-tie-a", TaskID: ownerTaskB.ID, SessionID: "owner-session", State: core.RunRunning, StartedAt: now.Add(time.Second)},
			{ID: "owner-run-tie-z", TaskID: ownerTaskB.ID, SessionID: "owner-session", State: core.RunRunning, StartedAt: now.Add(time.Second)},
		} {
			if createErr := c.CreateRun(ctx, ownerRun); createErr != nil {
				return domain.Result{}, createErr
			}
		}
		return domain.Result{ID: "seed-persisted-owner", Outcome: domain.OutcomeOK}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	scanner := &integrationScanner{observation: serviceObservation(path, "owner-head")}
	service := New(store, scanner, func() time.Time { return now })
	result, err := service.Scan(ctx, "persisted-owner-scan", ScanRequest{ProjectID: testsupport.Project, SessionID: "source", TaskID: "a", RunID: "run", Directory: path})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.Get(ctx, testsupport.Project, decodeScanSummary(t, result.Data).ObservationID)
	if err != nil {
		t.Fatal(err)
	}
	asset := snapshot.Assets[0]
	if asset.OwnerSessionID != "owner-session" || asset.OwnerTaskID != "owner-task-b" || asset.OwnerRunID != "owner-run-tie-z" || asset.Facts.Owner.State != gitobs.OwnerActive || asset.FirstSeenAt.IsZero() || asset.LastSeenAt.IsZero() {
		t.Fatalf("persisted owner = %+v", asset)
	}
	replay, err := service.Scan(ctx, "persisted-owner-scan", ScanRequest{ProjectID: testsupport.Project, SessionID: "source", TaskID: "a", RunID: "run", Directory: path})
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := service.Get(ctx, testsupport.Project, decodeScanSummary(t, replay.Data).ObservationID)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ID != snapshot.ID || !replayed.Assets[0].FirstSeenAt.Equal(asset.FirstSeenAt) || !replayed.Assets[0].LastSeenAt.Equal(asset.LastSeenAt) {
		t.Fatalf("owner replay = %+v; want canonical %+v", replayed, snapshot)
	}
}

func TestServiceGitInventoryMapsScannerErrorsAndMissingReads(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	store, _ := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	service := New(store, &integrationScanner{err: errors.New("scanner private output")}, func() time.Time { return now })
	_, err := service.Scan(context.Background(), "scanner-failure", ScanRequest{ProjectID: testsupport.Project, SessionID: "source", TaskID: "a", RunID: "run", Directory: "/repo"})
	assertServiceCode(t, err, domain.CodeUnavailable)
	_, err = service.Get(context.Background(), testsupport.Project, "missing")
	assertServiceCode(t, err, domain.CodeNotFound)
	_, err = service.Latest(context.Background(), testsupport.Project)
	assertServiceCode(t, err, domain.CodeNotFound)
	_, err = service.Get(context.Background(), "", "missing")
	assertServiceCode(t, err, domain.CodeInvalidArgument)
}

func assertServiceCode(t *testing.T, err error, want domain.ErrorCode) {
	t.Helper()
	var got domain.DomainError
	if !errors.As(err, &got) || got.Code != want {
		t.Fatalf("error = %v; want %s", err, want)
	}
}
func serviceObservation(path, head string) gitobs.Observation {
	asset := gitobs.Asset{Facts: gitobs.AssetFacts{Confidence: gitobs.ConfidenceObserved, WorktreePath: path, Branch: "main", DefaultBranch: true, Status: gitobs.Status{Branch: "main", Head: head, Confidence: gitobs.ConfidenceObserved}, Owner: gitobs.OwnerFacts{State: gitobs.OwnerUnknown}}, Worktree: gitobs.Worktree{Path: path, Head: head, Branch: "main"}, Classification: gitobs.AssetClassification{Labels: []gitobs.Classification{gitobs.ClassUnknown}, Confidence: gitobs.ConfidenceObserved}}
	o := gitobs.Observation{Revision: gitobs.ObservationRevision, Repository: gitobs.RepoWorktree, Confidence: gitobs.ConfidenceObserved, CommonDir: path + "/.git", TopLevel: path, DefaultBranch: "main", Assets: []gitobs.Asset{asset}}
	o.Hash = gitobs.HashObservation(o)
	return o
}
func decodeScanSummary(t *testing.T, v any) ScanSummary {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out ScanSummary
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRecordedCurrentAndLiveInspectionKeepGitAuthoritative(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	store, _ := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	scanner := &integrationScanner{observation: serviceObservation("/private/current", "head")}
	service := New(store, scanner, func() time.Time { return now })
	result, err := service.Scan(context.Background(), "current-scan", ScanRequest{ProjectID: testsupport.Project, SessionID: "source", TaskID: "a", RunID: "run", Directory: "/private/current"})
	if err != nil {
		t.Fatal(err)
	}
	id := decodeScanSummary(t, result.Data).ObservationID
	persisted, err := service.Get(context.Background(), testsupport.Project, id)
	if err != nil {
		t.Fatal(err)
	}
	current, err := service.RecordedCurrent(context.Background(), testsupport.Project)
	if err != nil {
		t.Fatal(err)
	}
	if scanner.calls != 1 {
		t.Fatalf("Current invoked scanner %d times", scanner.calls)
	}
	if len(persisted.Assets) != 1 || len(current.Assets) != 1 || !persisted.Assets[0].FirstSeenAt.Equal(current.Assets[0].FirstSeenAt) || !persisted.Assets[0].LastSeenAt.Equal(current.Assets[0].LastSeenAt) {
		t.Fatalf("metadata not preserved: persisted=%+v current=%+v", persisted.Assets, current.Assets)
	}
	live, err := service.Inspect(context.Background(), testsupport.Project, "/private/current")
	if err != nil {
		t.Fatal(err)
	}
	if scanner.calls != 2 || live.Trigger != "live" || len(live.Assets) != 1 || live.ID == persisted.ID {
		t.Fatalf("live inspection = %+v; scanner calls=%d", live, scanner.calls)
	}
	history, err := service.History(context.Background(), testsupport.Project)
	if err != nil || len(history) != 1 {
		t.Fatalf("live inspection persisted history: len=%d err=%v", len(history), err)
	}
	plan, err := service.CleanupPlan(context.Background(), testsupport.Project, "/private/current", "")
	if err != nil || plan.Mutating || len(plan.Blocked) != 1 {
		t.Fatalf("cleanup = %+v, %v", plan, err)
	}
	if scanner.calls != 3 {
		t.Fatalf("CleanupPlan scanner calls = %d", scanner.calls)
	}
	_, err = service.CleanupPlan(context.Background(), testsupport.Project, "/private/current", "unknown")
	assertServiceCode(t, err, domain.CodeNotFound)
}
