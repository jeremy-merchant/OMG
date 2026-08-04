package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	gitobs "github.com/jeremy-merchant/oh-my-group/internal/domain/git"
	core "github.com/jeremy-merchant/oh-my-group/internal/domain/lineage"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
)

func TestGitInventoryMigratedRoundTripAndCanonicalReplay(t *testing.T) {
	s, now := gitStoreFixture(t)
	first := hostileObservation("/private/first", "private-first-ref", "head-first")
	snapshot := gitSnapshot("obs-one", "replay-key", now, first)
	got := saveGitSnapshot(t, s, snapshot)
	if got.SequenceNo != 1 || len(got.Assets) != 4 {
		t.Fatalf("sequence/assets = %d/%d", got.SequenceNo, len(got.Assets))
	}
	if got.Observation.Hash != first.Hash || got.Assets[0].FirstSeenAt != now || got.Assets[0].LastSeenAt != now {
		t.Fatalf("round trip did not preserve hash and timestamps: %+v", got)
	}
	// Replay deliberately contains distinct, private scanner inputs. Persistence returns
	// the original canonical snapshot and never lets those strings into receipt/audit data.
	replay := gitSnapshot("obs-hostile", "replay-key", now.Add(time.Hour), hostileObservation("/private/second", "private-second-ref", "head-second"))
	replayed := saveGitSnapshot(t, s, replay)
	if replayed.ID != got.ID || replayed.Observation.Hash != got.Observation.Hash || replayed.SequenceNo != 1 {
		t.Fatalf("replay = %+v; want canonical %+v", replayed, got)
	}
	assertCount(t, s.db, "git_observations", 1)
	assertCount(t, s.db, "git_observation_assets", 4)
	for _, tableColumn := range []struct{ table, column string }{{"command_receipts", "result_json"}, {"audit_events", "payload_json"}} {
		var all sql.NullString
		if err := s.db.QueryRow("SELECT group_concat(" + tableColumn.column + ",'|') FROM " + tableColumn.table).Scan(&all); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(all.String, "private-first") || strings.Contains(all.String, "private-second") {
			t.Fatalf("%s leaked private scanner content: %q", tableColumn.table, all.String)
		}
	}
}

func TestGitInventoryIdempotencyKeyIsScopedByProject(t *testing.T) {
	s, now := gitStoreFixture(t)
	first := saveGitSnapshot(t, s, gitSnapshot("obs-project-p", "shared-scan-key", now, hostileObservation("/private/p", "p-branch", "p-head")))
	if _, err := s.db.Exec(`INSERT INTO projects(id,created_at) VALUES('q',?)`, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	secondSnapshot := gitSnapshot("obs-project-q", "shared-scan-key", now, hostileObservation("/private/q", "q-branch", "q-head"))
	secondSnapshot.ProjectID = "q"
	secondSnapshot.ActorSessionID = ""
	secondSnapshot.TaskID = ""
	secondSnapshot.RunID = ""
	second := saveGitSnapshot(t, s, secondSnapshot)
	if first.ID == second.ID || second.ProjectID != "q" || second.SequenceNo != 1 {
		t.Fatalf("project-scoped snapshots = first %+v, second %+v", first, second)
	}
	assertCount(t, s.db, "git_observations", 2)
}

func TestScopedGitRepositoryRejectsForeignProjects(t *testing.T) {
	s := migratedStore(t, OpenOptions{})
	defer s.Close()
	ctx := context.Background()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	const projectA = domain.ProjectID("project-a")
	const projectB = domain.ProjectID("project-b")
	if _, err := s.db.Exec(`INSERT INTO projects(id,created_at) VALUES(?,?)`, projectA, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	scoped := s.Scope(projectA)
	snapshot := gitobs.Snapshot{ID: "snapshot-a", ProjectID: projectA, IdempotencyKey: "scan-a", Trigger: "scan", ObservedAt: now, Observation: hostileObservation("/repo-a", "main", "head-a")}
	if _, _, err := scoped.Write(ctx, "write-a", "test.write", func(r ports.Repositories) (domain.Result, error) {
		_, err := r.Git().CreateSnapshot(ctx, snapshot)
		return domain.Result{ID: "snapshot-a", Outcome: domain.OutcomeOK}, err
	}); err != nil {
		t.Fatal(err)
	}
	if err := scoped.Read(ctx, func(r ports.Repositories) error {
		repo := r.Git()
		if got, ok, err := repo.GetSnapshot(ctx, projectA, snapshot.ID); err != nil || !ok || got.ID != snapshot.ID {
			t.Fatalf("same-project GetSnapshot = %+v, %v, %v", got, ok, err)
		}
		if got, ok, err := repo.LatestSnapshot(ctx, projectA); err != nil || !ok || got.ID != snapshot.ID {
			t.Fatalf("same-project LatestSnapshot = %+v, %v, %v", got, ok, err)
		}
		if got, err := repo.History(ctx, projectA); err != nil || len(got) != 1 || got[0].ID != snapshot.ID {
			t.Fatalf("same-project History = %+v, %v", got, err)
		}
		if got, err := repo.LatestSequence(ctx, projectA); err != nil || got != 1 {
			t.Fatalf("same-project LatestSequence = %d, %v", got, err)
		}
		for _, call := range []func() error{
			func() error { _, _, err := repo.GetSnapshot(ctx, projectB, snapshot.ID); return err },
			func() error { _, _, err := repo.LatestSnapshot(ctx, projectB); return err },
			func() error { _, err := repo.History(ctx, projectB); return err },
			func() error { _, err := repo.LatestSequence(ctx, projectB); return err },
		} {
			if err := call(); err == nil {
				t.Fatal("foreign project read succeeded")
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	foreign := snapshot
	foreign.ID, foreign.ProjectID, foreign.IdempotencyKey = "snapshot-b", projectB, "scan-b"
	if _, _, err := scoped.Write(ctx, "write-b", "test.write", func(r ports.Repositories) (domain.Result, error) {
		_, err := r.Git().CreateSnapshot(ctx, foreign)
		return domain.Result{ID: "snapshot-b", Outcome: domain.OutcomeOK}, err
	}); err == nil {
		t.Fatal("foreign project CreateSnapshot succeeded")
	}
	assertCount(t, s.db, "git_observations", 1)
}

func TestGitInventoryOrderedHistoryPreservesSeenAndEqualTimeOrder(t *testing.T) {
	s, now := gitStoreFixture(t)
	ctx := context.Background()
	first := saveGitSnapshot(t, s, gitSnapshot("first", "key-one", now, hostileObservation("/repo", "refs/heads/main", "head-one")))
	second := saveGitSnapshot(t, s, gitSnapshot("second", "key-two", now, hostileObservation("/repo", "refs/heads/main", "head-two")))
	if first.SequenceNo != 1 || second.SequenceNo != 2 {
		t.Fatalf("sequence = %d,%d", first.SequenceNo, second.SequenceNo)
	}
	if second.Assets[0].FirstSeenAt != first.Assets[0].FirstSeenAt || second.Assets[0].LastSeenAt != now {
		t.Fatalf("seen lineage = %+v", second.Assets[0])
	}
	if err := s.Read(ctx, func(r ports.Repositories) error {
		latest, ok, err := r.Git().LatestSnapshot(ctx, "p")
		if err != nil || !ok || latest.ID != "second" {
			t.Fatalf("latest = %+v ok=%v err=%v", latest, ok, err)
		}
		history, err := r.Git().History(ctx, "p")
		if err != nil {
			return err
		}
		if len(history) != 2 || history[0].ID != "first" || history[1].ID != "second" {
			t.Fatalf("history order = %+v", history)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestGitInventoryRejectsDuplicateAndEmptyAssetIdentity(t *testing.T) {
	s, now := gitStoreFixture(t)
	duplicate := hostileObservation("/repo", "main", "head")
	duplicate.Assets = append(duplicate.Assets, duplicate.Assets[0])
	duplicate.Hash = gitobs.HashObservation(duplicate)
	if _, err := createGitSnapshot(s, gitSnapshot("duplicate", "duplicate-key", now, duplicate)); err == nil {
		t.Fatal("duplicate stable fingerprint accepted")
	}
	empty := hostileObservation("/repo", "main", "head")
	empty.Assets = []gitobs.Asset{{Facts: gitobs.AssetFacts{Confidence: gitobs.ConfidenceObserved, BranchOnly: true, Owner: gitobs.OwnerFacts{State: gitobs.OwnerUnknown}}, Classification: gitobs.AssetClassification{Labels: []gitobs.Classification{gitobs.ClassUnknown}, Confidence: gitobs.ConfidenceObserved}}}
	empty.Hash = gitobs.HashObservation(empty)
	if _, err := createGitSnapshot(s, gitSnapshot("empty", "empty-key", now, empty)); err == nil {
		t.Fatal("empty asset identity accepted")
	}
}

func TestGitInventoryRejectsPartialRegisteredOwnerLineage(t *testing.T) {
	s, now := gitStoreFixture(t)
	observation := hostileObservation("/repo", "main", "head")
	observation.Assets = observation.Assets[:1]
	observation.Assets[0].Facts.Owner = gitobs.OwnerFacts{Registered: true, State: gitobs.OwnerActive}
	observation.Assets[0].Classification = gitobs.Classify(observation.Assets[0].Facts)
	observation.Hash = gitobs.HashObservation(observation)
	snapshot := gitSnapshot("partial-owner", "partial-owner-key", now, observation)
	snapshot.Assets = []gitobs.AssetRecord{{
		Asset:          observation.Assets[0],
		Fingerprint:    observation.Assets[0].StableFingerprint(),
		OwnerSessionID: "s",
	}}
	if _, err := createGitSnapshot(s, snapshot); err == nil {
		t.Fatal("registered owner with partial session-only lineage accepted")
	}
}

func TestGitInventorySchemaRejectsHostileWritesAndCorruptReadsFailClosed(t *testing.T) {
	s, now := gitStoreFixture(t)
	good := saveGitSnapshot(t, s, gitSnapshot("good", "good-key", now, hostileObservation("/repo", "main", "head")))
	for _, query := range []string{
		"UPDATE git_observations SET observed_at='2026-02-30T12:00:00.000000000Z' WHERE id='good'",
		"DELETE FROM git_observations WHERE id='good'",
		"UPDATE git_observation_assets SET classification_json='[\"bad\"]' WHERE observation_id='good'",
		"DELETE FROM git_observation_assets WHERE observation_id='good'",
	} {
		if _, err := s.db.Exec(query); err == nil {
			t.Fatalf("hostile SQL succeeded: %s", query)
		}
	}
	// Direct invalid actor/task/run lineage is rejected before a row can exist.
	_, err := s.db.Exec(`INSERT INTO git_observations(id,project_id,idempotency_key,actor_session_id,task_id,run_id,trigger_kind,observed_at,revision,observation_hash,repository_state,confidence,common_dir,top_level,default_branch,sequence_no) VALUES('bad-lineage','p','bad-lineage','other','t','run','scan','2026-07-22T12:00:00.999999999Z','git-observation/v1',?,'worktree','observed','','/repo','main',9)`, strings.Repeat("a", 64))
	if err == nil {
		t.Fatal("foreign actor lineage accepted")
	}
	// Bypass immutability solely to simulate on-disk corruption; adapter reads must fail closed.
	if _, err := s.db.Exec("DROP TRIGGER git_assets_no_update"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("PRAGMA ignore_check_constraints=ON"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("UPDATE git_observation_assets SET owner_registered=2 WHERE observation_id='good'"); err != nil {
		t.Fatal(err)
	}
	err = s.Read(context.Background(), func(r ports.Repositories) error {
		_, _, e := r.Git().GetSnapshot(context.Background(), "p", good.ID)
		return e
	})
	if err == nil {
		t.Fatal("non-boolean owner_registered read succeeded")
	}
	if _, err := s.db.Exec("UPDATE git_observation_assets SET owner_registered=0,classification_json='[\"not-an-enum\"]' WHERE observation_id='good'"); err != nil {
		t.Fatal(err)
	}
	err = s.Read(context.Background(), func(r ports.Repositories) error {
		_, _, e := r.Git().GetSnapshot(context.Background(), "p", good.ID)
		return e
	})
	if err == nil {
		t.Fatal("corrupt classification read succeeded")
	}
	if _, err := s.db.Exec("UPDATE git_observation_assets SET owner_registered=1,owner_state='active',owner_session_id=NULL,owner_task_id=NULL,owner_run_id=NULL,classification_json='[\"unknown\"]' WHERE observation_id='good'"); err != nil {
		t.Fatal(err)
	}
	err = s.Read(context.Background(), func(r ports.Repositories) error {
		_, _, e := r.Git().GetSnapshot(context.Background(), "p", good.ID)
		return e
	})
	if err == nil {
		t.Fatal("registered owner without lineage read succeeded")
	}
}

func gitStoreFixture(t *testing.T) (*SQLiteStore, time.Time) {
	t.Helper()
	s := migratedStore(t, OpenOptions{})
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()
	if _, err := s.db.Exec("INSERT INTO projects(id,created_at) VALUES('p',?)", now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Write(ctx, "git-lineage", "test.write", func(r ports.Repositories) (domain.Result, error) {
		c := r.Coordination()
		h := core.Human{ID: "h", DisplayName: "human", Confidence: core.ConfidenceExplicit, CreatedAt: now}
		if err := c.CreateHuman(ctx, h); err != nil {
			return domain.Result{}, err
		}
		if err := c.CreateSession(ctx, core.AgentSession{ID: "s", ProjectID: "p", HumanID: "h", Kind: core.HumanDirect, Runtime: "test", Role: "owner", Source: core.SourceHuman, SourceRef: "fixture", RootID: "s", StartedAt: now}); err != nil {
			return domain.Result{}, err
		}
		if _, err := c.CreateTask(ctx, core.Task{ID: "t", ProjectID: "p", Title: "task", State: core.TaskClaimed, CreatedAt: now, UpdatedAt: now}); err != nil {
			return domain.Result{}, err
		}
		if err := c.CreateRun(ctx, core.TaskRun{ID: "run", TaskID: "t", SessionID: "s", State: core.RunWorkComplete, StartedAt: now}); err != nil {
			return domain.Result{}, err
		}
		return domain.Result{ID: "lineage", Outcome: domain.OutcomeOK}, nil
	}); err != nil {
		t.Fatal(err)
	}
	return s, now
}
func createGitSnapshot(s *SQLiteStore, snapshot gitobs.Snapshot) (gitobs.Snapshot, error) {
	var got gitobs.Snapshot
	_, _, err := s.Write(context.Background(), domain.IdempotencyKey("write-"+string(snapshot.ID)), "test.write", func(r ports.Repositories) (domain.Result, error) {
		var err error
		got, err = r.Git().CreateSnapshot(context.Background(), snapshot)
		return domain.Result{ID: domain.ResultID(snapshot.ID), Outcome: domain.OutcomeOK}, err
	})
	return got, err
}
func saveGitSnapshot(t *testing.T, s *SQLiteStore, snapshot gitobs.Snapshot) gitobs.Snapshot {
	t.Helper()
	got, err := createGitSnapshot(s, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
func gitSnapshot(id, key string, at time.Time, o gitobs.Observation) gitobs.Snapshot {
	return gitobs.Snapshot{ID: id, ProjectID: "p", IdempotencyKey: domain.IdempotencyKey(key), ActorSessionID: "s", TaskID: "t", RunID: "run", Trigger: "scan", ObservedAt: at, Observation: o}
}
func hostileObservation(path, branch, head string) gitobs.Observation {
	assets := []gitobs.Asset{
		{Facts: gitobs.AssetFacts{Confidence: gitobs.ConfidenceObserved, WorktreePath: path, Branch: branch, DefaultBranch: true, Status: gitobs.Status{Branch: branch, Head: head, Upstream: "origin/" + branch, Ahead: 2, Behind: 1, TrackedDirty: 3, Untracked: 4, Ignored: 5, Confidence: gitobs.ConfidenceObserved}, Fingerprint: gitobs.FingerprintFacts{MergeBaseKnown: true, MergeBaseEqualsHead: false, DefaultCountsKnown: true}, Owner: gitobs.OwnerFacts{State: gitobs.OwnerUnknown}, DefaultAhead: 2, DefaultBehind: 1}, Worktree: gitobs.Worktree{Path: path, Head: head, Branch: branch, Locked: true, Prunable: true, PruneReason: "private-prune-reason"}, Classification: gitobs.AssetClassification{Labels: []gitobs.Classification{gitobs.ClassDirtyUnowned, gitobs.ClassUnpushed}, Confidence: gitobs.ConfidenceObserved}},
		{Facts: gitobs.AssetFacts{Confidence: gitobs.ConfidenceObserved, WorktreePath: path + "/linked", Branch: "feature", Status: gitobs.Status{Branch: "feature", Head: "linked-head", Confidence: gitobs.ConfidenceObserved}, Owner: gitobs.OwnerFacts{State: gitobs.OwnerUnknown}}, Worktree: gitobs.Worktree{Path: path + "/linked", Head: "linked-head", Branch: "feature"}, Classification: gitobs.AssetClassification{Labels: []gitobs.Classification{gitobs.ClassUnknown}, Confidence: gitobs.ConfidenceObserved}},
		{Facts: gitobs.AssetFacts{Confidence: gitobs.ConfidenceObserved, Branch: "branch-only", BranchOnly: true, Status: gitobs.Status{Branch: "branch-only", Head: "branch-head", Confidence: gitobs.ConfidenceObserved}, Owner: gitobs.OwnerFacts{State: gitobs.OwnerUnknown}}, Worktree: gitobs.Worktree{Branch: "branch-only", Bare: true}, Classification: gitobs.AssetClassification{Labels: []gitobs.Classification{gitobs.ClassBranchOnly}, Confidence: gitobs.ConfidenceObserved}},
		{Facts: gitobs.AssetFacts{Confidence: gitobs.ConfidenceUnknown, WorktreePath: path + "/detached", Detached: true, Status: gitobs.Status{Head: "detached-head", Detached: true, Confidence: gitobs.ConfidenceUnknown}, Owner: gitobs.OwnerFacts{State: gitobs.OwnerUnknown}}, Worktree: gitobs.Worktree{Path: path + "/detached", Head: "detached-head", Detached: true}, Classification: gitobs.AssetClassification{Labels: []gitobs.Classification{gitobs.ClassDetachedUnowned}, Confidence: gitobs.ConfidenceUnknown}},
	}
	o := gitobs.Observation{Revision: gitobs.ObservationRevision, Repository: gitobs.RepoWorktree, Confidence: gitobs.ConfidenceObserved, CommonDir: path + "/.git", TopLevel: path, DefaultBranch: "main", Assets: assets}
	o.Hash = gitobs.HashObservation(o)
	return o
}
