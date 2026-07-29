package handoff

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	appgit "github.com/jeremy-merchant/OMG/internal/app/git"
	"github.com/jeremy-merchant/OMG/internal/app/testsupport"
	"github.com/jeremy-merchant/OMG/internal/domain"
	coord "github.com/jeremy-merchant/OMG/internal/domain/coordination"
	gitobs "github.com/jeremy-merchant/OMG/internal/domain/git"
	core "github.com/jeremy-merchant/OMG/internal/domain/lineage"
	"github.com/jeremy-merchant/OMG/internal/ports"
)

type adoptionScanner struct{ observation gitobs.Observation }

func (s adoptionScanner) Scan(context.Context, string) (gitobs.Observation, error) {
	return s.observation, nil
}
func adoptionObservation(path string, assets []gitobs.Asset) gitobs.Observation {
	o := gitobs.Observation{Revision: gitobs.ObservationRevision, Repository: gitobs.RepoWorktree, Confidence: gitobs.ConfidenceObserved, CommonDir: path + "/.git", TopLevel: path, DefaultBranch: "main", Assets: assets}
	o.Hash = gitobs.HashObservation(o)
	return o
}
func adoptionAsset(path string) gitobs.Asset {
	return gitobs.Asset{Facts: gitobs.AssetFacts{Confidence: gitobs.ConfidenceObserved, WorktreePath: path, Branch: "topic", Status: gitobs.Status{Branch: "topic", Head: "head", Confidence: gitobs.ConfidenceObserved}, Owner: gitobs.OwnerFacts{State: gitobs.OwnerUnknown}}, Worktree: gitobs.Worktree{Path: path, Head: "head", Branch: "topic"}, Classification: gitobs.AssetClassification{Labels: []gitobs.Classification{gitobs.ClassUnknown}, Confidence: gitobs.ConfidenceObserved}}
}
func seedAdoptionSnapshot(t *testing.T, store ports.Store, now time.Time, key, path string, assets []gitobs.Asset) gitobs.Snapshot {
	t.Helper()
	svc := appgit.New(store, adoptionScanner{observation: adoptionObservation(path, assets)}, func() time.Time { return now })
	result, err := svc.Scan(context.Background(), domain.IdempotencyKey(key), appgit.ScanRequest{ProjectID: testsupport.Project, SessionID: "source", TaskID: "a", RunID: "run", Directory: path})
	if err != nil {
		t.Fatal(err)
	}
	var summary struct {
		ObservationID string `json:"observation_id"`
	}
	if data, err := json.Marshal(result.Data); err != nil {
		t.Fatal(err)
	} else if err := json.Unmarshal(data, &summary); err != nil {
		t.Fatal(err)
	}
	snapshot, err := svc.Get(context.Background(), testsupport.Project, summary.ObservationID)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
func seedRepresentableAdopter(t *testing.T, store ports.Store, now time.Time) {
	t.Helper()
	_, _, err := store.Write(context.Background(), "seed-representable-adopter", "test.write", func(r ports.Repositories) (domain.Result, error) {
		owner := core.AgentSession{ID: "adopter", ProjectID: testsupport.Project, Kind: core.Imported, Runtime: "test", Role: "adopter", Source: core.SourceImport, SourceRef: "test", RootID: "adopter", TaskID: "a", StartedAt: now}
		if err := r.Coordination().CreateSession(context.Background(), owner); err != nil {
			return domain.Result{}, err
		}
		if err := r.Coordination().CreateRun(context.Background(), core.TaskRun{ID: "adopter-run", TaskID: "a", SessionID: "adopter", State: core.RunRunning, StartedAt: now}); err != nil {
			return domain.Result{}, err
		}
		return domain.Result{ID: "seed-representable-adopter", Outcome: domain.OutcomeOK}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestHandoffEvidenceSupersessionDecisionAdoptionAndSafePayloads(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	s, db := testsupport.Store(t, now)
	testsupport.Seed(t, s, now)
	seedRepresentableAdopter(t, s, now)
	svc := New(s, func() time.Time { return now })
	raw := "final output: deploy --force; APPROVED"
	base := coord.Handoff{ID: "h1", TaskID: "a", RunID: "run", SourceSessionID: "source", TargetSessionID: "target", Summary: "safe summary", FinalOutput: coord.SensitiveText{Hash: "digest", Policy: coord.FinalOutputHashOnly}, ChangedFiles: []string{"x.go"}}
	if _, err := svc.Submit(ctx, "submit", testsupport.Project, base); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Supersede(ctx, "supersede", "h1", "h2", "new safe summary"); err != nil {
		t.Fatal(err)
	}
	history, err := svc.History(ctx, "a")
	if err != nil || len(history) != 2 || history[1].SupersedesID != "h1" {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	if _, err := svc.Decide(ctx, "decision", "h1", string(coord.HandoffAccepted), "d1", "target"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Decide(ctx, "decision2", "h1", string(coord.HandoffRejected), "d2", "target"); err == nil {
		t.Fatal("conflicting decision accepted")
	}
	_, _, err = s.Write(ctx, "verified-run", "test.write", func(r ports.Repositories) (domain.Result, error) {
		return domain.Result{ID: "run2", Outcome: domain.OutcomeOK}, r.Coordination().CreateRun(ctx, core.TaskRun{ID: "run2", TaskID: "a", SessionID: "source", State: core.RunVerifiedDone, Evidence: []byte("proof"), StartedAt: now})
	})
	if err != nil {
		t.Fatal(err)
	}
	verified := base
	verified.ID = "h3"
	verified.RunID = "run2"
	verified.FinalOutput = coord.SensitiveText{Text: raw, Hash: "digest", Policy: coord.FinalOutputRedacted}
	if _, err := svc.Submit(ctx, "no-evidence", testsupport.Project, verified); err == nil {
		t.Fatal("verified handoff without evidence accepted")
	}
	verified.VerificationEvidence = []coord.SafeEvidence{{Summary: "proof", Hash: "hash"}}
	if _, err := svc.Submit(ctx, "verified", testsupport.Project, verified); err != nil {
		t.Fatal(err)
	}
	adoption := coord.Adoption{ID: "adopt", ProjectID: testsupport.Project, HandoffID: "h3", NewOwnerSessionID: "adopter", Reason: "parent lost"}
	if _, err := svc.Adopt(ctx, "adopt", adoption); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Adopt(ctx, "adopt2", coord.Adoption{ID: "bad", ProjectID: testsupport.Project, HandoffID: "h3", TaskID: "a", NewOwnerSessionID: "adopter", Reason: "two targets"}); err == nil {
		t.Fatal("multi-target adoption accepted")
	}
	for _, q := range []string{"SELECT result_json FROM command_receipts", "SELECT payload_json FROM audit_events"} {
		rows, err := db.Query(q)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var payload string
			if err := rows.Scan(&payload); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(payload, raw) {
				t.Fatal("raw final output leaked")
			}
		}
		rows.Close()
	}
}
func TestHandoffReplayReturnsCanonicalRecords(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	s, db := testsupport.Store(t, now)
	testsupport.Seed(t, s, now)
	seedRepresentableAdopter(t, s, now)
	svc := New(s, func() time.Time { return now })
	base := coord.Handoff{ID: "original", TaskID: "a", RunID: "run", SourceSessionID: "source", TargetSessionID: "target", Summary: "canonical", FinalOutput: coord.SensitiveText{Hash: "digest", Policy: coord.FinalOutputHashOnly}}
	submitted, err := svc.Submit(ctx, "submit-replay", testsupport.Project, base)
	if err != nil {
		t.Fatal(err)
	}
	changed := base
	changed.ID = "new-local-id"
	changed.Summary = "different private caller content"
	replay, err := svc.Submit(ctx, "submit-replay", testsupport.Project, changed)
	if err != nil {
		t.Fatal(err)
	}
	if replay.ID != submitted.ID || replay.Summary != submitted.Summary {
		t.Fatalf("submit replay=%+v want canonical=%+v", replay, submitted)
	}
	superseded, err := svc.Supersede(ctx, "supersede-replay", submitted.ID, "superseded", "canonical supersession")
	if err != nil {
		t.Fatal(err)
	}
	supersedeReplay, err := svc.Supersede(ctx, "supersede-replay", "new-local-id", "different-local-id", "different caller content")
	if err != nil {
		t.Fatal(err)
	}
	if supersedeReplay.ID != superseded.ID || supersedeReplay.Summary != superseded.Summary {
		t.Fatalf("supersede replay=%+v want canonical=%+v", supersedeReplay, superseded)
	}
	decided, err := svc.Decide(ctx, "decide-replay", submitted.ID, string(coord.HandoffAccepted), "decision-original", "target")
	if err != nil {
		t.Fatal(err)
	}
	decideReplay, err := svc.Decide(ctx, "decide-replay", "new-local-id", string(coord.HandoffAccepted), "decision-local", "source")
	if err != nil {
		t.Fatal(err)
	}
	if decideReplay.ID != decided.ID || decideReplay.HandoffID != decided.HandoffID || decideReplay.Decision != decided.Decision {
		t.Fatalf("accept replay=%+v want canonical=%+v", decideReplay, decided)
	}
	if _, err := svc.Decide(ctx, "decide-replay", "new-local-id", string(coord.HandoffRejected), "decision-local", "source"); err == nil {
		t.Fatal("reject reused accept key")
	}
	var decisionCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM handoff_decisions WHERE handoff_id = ?", submitted.ID).Scan(&decisionCount); err != nil {
		t.Fatal(err)
	}
	if decisionCount != 1 {
		t.Fatalf("cross-command decision mutated handoff: count=%d", decisionCount)
	}
	adoption := coord.Adoption{ID: "adoption-original", ProjectID: testsupport.Project, HandoffID: submitted.ID, NewOwnerSessionID: "adopter", Reason: "canonical reason"}
	adopted, err := svc.Adopt(ctx, "adopt-replay", adoption)
	if err != nil {
		t.Fatal(err)
	}
	changedAdoption := adoption
	changedAdoption.ID = "adoption-local"
	changedAdoption.Reason = "different private reason"
	adoptReplay, err := svc.Adopt(ctx, "adopt-replay", changedAdoption)
	if err != nil {
		t.Fatal(err)
	}
	if adoptReplay.ID != adopted.ID || adoptReplay.Reason != adopted.Reason {
		t.Fatalf("adopt replay=%+v want canonical=%+v", adoptReplay, adopted)
	}
	if _, err := svc.Adopt(ctx, "adopt-replay", coord.Adoption{ID: "git-adoption-local", ProjectID: testsupport.Project, GitAssetID: "unknown-asset", NewOwnerSessionID: "adopter", Reason: "cross-command"}); err == nil {
		t.Fatal("git adoption reused handoff adoption key")
	}
	var handoffs, decisions, adoptions, receipts int
	if err := db.QueryRow("SELECT COUNT(*) FROM handoffs WHERE id IN ('original','new-local-id','superseded','different-local-id')").Scan(&handoffs); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM handoff_decisions WHERE id IN ('decision-original','decision-local')").Scan(&decisions); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM adoptions WHERE id IN ('adoption-original','adoption-local')").Scan(&adoptions); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM command_receipts WHERE idempotency_key IN ('submit-replay','supersede-replay','decide-replay','adopt-replay')").Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if handoffs != 2 || decisions != 1 || adoptions != 1 || receipts != 4 {
		t.Fatalf("handoffs=%d decisions=%d adoptions=%d receipts=%d", handoffs, decisions, adoptions, receipts)
	}
}

func TestHandoffLifecyclePersistsIntegrationCanaryAndCleanupEvidence(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	store, db := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	clock := now
	svc := New(store, func() time.Time { return clock })
	handoff := coord.Handoff{ID: "lifecycle-handoff", TaskID: "a", RunID: "run", SourceSessionID: "source", TargetSessionID: "target", Summary: "ready", FinalOutput: coord.SensitiveText{Policy: coord.FinalOutputNone}, SourceCommit: "source-commit", SourceTree: "source-tree"}
	if _, err := svc.Submit(ctx, "lifecycle-submit", testsupport.Project, handoff); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Second)
	if _, err := svc.Advance(ctx, "lifecycle-review", coord.HandoffLifecycleEvent{ID: "lifecycle-review", HandoffID: handoff.ID, ActorSessionID: "target", State: coord.IntegrationReviewing}); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Second)
	if _, err := svc.Decide(ctx, "lifecycle-accept", handoff.ID, string(coord.HandoffAccepted), "lifecycle-decision", "target"); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Second)
	if _, err := svc.Advance(ctx, "lifecycle-integrated", coord.HandoffLifecycleEvent{ID: "lifecycle-integrated", HandoffID: handoff.ID, ActorSessionID: "target", State: coord.IntegrationIntegrated, IntegrationCommit: "integration-commit"}); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Second)
	started := clock
	if _, err := svc.Advance(ctx, "lifecycle-canary-start", coord.HandoffLifecycleEvent{ID: "lifecycle-canary-start", HandoffID: handoff.ID, ActorSessionID: "target", State: coord.IntegrationCanaryRunning, CanaryRunID: "canary-run", CanaryIntegrationRef: "refs/heads/main", CanaryTargetSHA: "integration-commit", CanaryTargetTree: "integration-tree", CanaryCommand: "go test ./...", CanaryExecutionKind: "real", CanaryEnvironmentFingerprint: "env-fingerprint", CanaryHeadBefore: "integration-commit", CanaryRefFingerprintBefore: "ref-fingerprint", CanaryStartedAt: &started}); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Second)
	finished, exitCode := clock, 0
	if _, err := svc.Advance(ctx, "lifecycle-canary", coord.HandoffLifecycleEvent{ID: "lifecycle-canary", HandoffID: handoff.ID, ActorSessionID: "target", State: coord.IntegrationCanaryPassed, CanaryRunID: "canary-run", CanaryIntegrationRef: "refs/heads/main", CanaryTargetSHA: "integration-commit", CanaryTargetTree: "integration-tree", CanaryResult: "PASS_REAL", CanaryCommand: "go test ./...", CanaryExecutionKind: "real", CanaryEnvironmentFingerprint: "env-fingerprint", CanaryHeadBefore: "integration-commit", CanaryHeadAfter: "integration-commit", CanaryRefFingerprintBefore: "ref-fingerprint", CanaryRefFingerprintAfter: "ref-fingerprint", CanaryExitCode: &exitCode, CanaryPassedCount: 1, CanaryStartedAt: &started, CanaryFinishedAt: &finished}); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Second)
	if _, err := svc.Advance(ctx, "lifecycle-cleaned", coord.HandoffLifecycleEvent{ID: "lifecycle-cleaned", HandoffID: handoff.ID, ActorSessionID: "target", State: coord.IntegrationSourceCleaned, SourceWorktreeCleaned: true, SourceBranchCleaned: true}); err != nil {
		t.Fatal(err)
	}
	events, err := svc.Lifecycle(ctx, handoff.ID)
	if err != nil || len(events) != 7 || coord.CurrentIntegrationState(events, nil) != coord.IntegrationSourceCleaned {
		t.Fatalf("lifecycle=%+v err=%v", events, err)
	}
	if _, err := db.Exec(`UPDATE handoff_lifecycle_events SET state='BLOCKED' WHERE id='lifecycle-integrated'`); err == nil {
		t.Fatal("append-only lifecycle event was updated")
	}
}

func TestLatestGitAdoptionUsesParsedTimestampOrdering(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	store, _ := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	snapshot := seedAdoptionSnapshot(t, store, now, "timestamp-provenance", "/repo/timestamp", []gitobs.Asset{adoptionAsset("/repo/timestamp")})
	fingerprint := snapshot.Assets[0].Fingerprint
	_, _, err := store.Write(ctx, "seed-git-adoption-order", "test.write", func(r ports.Repositories) (domain.Result, error) {
		first := coord.Adoption{ID: "adoption-a", ProjectID: testsupport.Project, GitAssetID: fingerprint, NewOwnerSessionID: "target", Reason: "first", CreatedAt: now.Add(100 * time.Millisecond)}
		second := coord.Adoption{ID: "adoption-b", ProjectID: testsupport.Project, GitAssetID: fingerprint, NewOwnerSessionID: "target", Reason: "second", CreatedAt: now.Add(110 * time.Millisecond)}
		if err := r.Coordination().CreateAdoption(ctx, first); err != nil {
			return domain.Result{}, err
		}
		if err := r.Coordination().CreateAdoption(ctx, second); err != nil {
			return domain.Result{}, err
		}
		return domain.Result{ID: "seed-git-adoption-order", Outcome: domain.OutcomeOK}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var latest coord.Adoption
	err = store.Read(ctx, func(r ports.Repositories) error {
		var ok bool
		var err error
		latest, ok, err = r.Coordination().LatestGitAdoption(ctx, testsupport.Project, fingerprint)
		if err != nil {
			return err
		}
		if !ok {
			t.Fatal("latest adoption missing")
		}
		return nil
	})
	if err != nil || latest.ID != "adoption-b" {
		t.Fatalf("latest = %+v, %v", latest, err)
	}
}
func TestGitAdoptionFailuresLeaveNoReceiptOrSuccessAudit(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	store, db := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	testsupport.SeedForeign(t, store, db, now)
	service := New(store, func() time.Time { return now })
	for _, adoption := range []coord.Adoption{
		{ID: "missing-fingerprint", ProjectID: testsupport.Project, GitAssetID: "missing", NewOwnerSessionID: "target", Reason: "missing"},
		{ID: "foreign-adopter", ProjectID: testsupport.Project, GitAssetID: "missing", NewOwnerSessionID: "foreign-session", Reason: "foreign"},
	} {
		if _, err := service.Adopt(ctx, domain.IdempotencyKey(adoption.ID), adoption); err == nil {
			t.Fatalf("Adopt(%s) succeeded", adoption.ID)
		}
	}
	for _, query := range []string{
		"SELECT COUNT(*) FROM command_receipts WHERE idempotency_key IN ('missing-fingerprint','foreign-adopter')",
		"SELECT COUNT(*) FROM audit_events a JOIN command_receipts r ON r.id=a.receipt_id WHERE r.idempotency_key IN ('missing-fingerprint','foreign-adopter') AND a.event_type='adoption.created'",
	} {
		var count int
		if err := db.QueryRow(query).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("failed Git adoptions recorded success (%s): %d", query, count)
		}
	}
}

func TestNonGitAdoptionAllowsProjectTargetWithoutTaskRunLineage(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	store, _ := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	service := New(store, func() time.Time { return now })
	handoff := coord.Handoff{ID: "unrepresentable-owner", TaskID: "a", RunID: "run", SourceSessionID: "source", Summary: "handoff", FinalOutput: coord.SensitiveText{Hash: "hash", Policy: coord.FinalOutputHashOnly}}
	if _, err := service.Submit(ctx, "submit-unrepresentable-owner", testsupport.Project, handoff); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Adopt(ctx, "adopt-unrepresentable-owner", coord.Adoption{ID: "unrepresentable-owner-adoption", ProjectID: testsupport.Project, HandoffID: handoff.ID, NewOwnerSessionID: "target", Reason: "project target without task/run"}); err != nil {
		t.Fatalf("non-Git adoption rejected: %v", err)
	}
}

func TestLatestGitAdoptionFailsClosedForCorruptNewestRow(t *testing.T) {
	for name, corrupt := range map[string]string{
		"foreign-adopter":      "UPDATE adoptions SET adopter_session_id='foreign-session' WHERE id='new'",
		"malformed-created-at": "UPDATE adoptions SET created_at='not-a-timestamp' WHERE id='new'",
		"empty-git-asset-ref":  "UPDATE adoptions SET git_asset_ref='' WHERE id='new'",
		"wrong-git-asset-ref":  "UPDATE adoptions SET git_asset_ref=' ' WHERE id='new'",
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
			store, db := testsupport.Store(t, now)
			testsupport.Seed(t, store, now)
			snapshot := seedAdoptionSnapshot(t, store, now, "corrupt-provenance", "/repo/corrupt", []gitobs.Asset{adoptionAsset("/repo/corrupt")})
			fingerprint := snapshot.Assets[0].Fingerprint
			testsupport.SeedForeign(t, store, db, now)
			_, _, err := store.Write(ctx, "seed-adoptions", "test.write", func(r ports.Repositories) (domain.Result, error) {
				for _, a := range []coord.Adoption{
					{ID: "old", ProjectID: testsupport.Project, GitAssetID: fingerprint, NewOwnerSessionID: "target", Reason: "old", CreatedAt: now},
					{ID: "new", ProjectID: testsupport.Project, GitAssetID: fingerprint, NewOwnerSessionID: "target", Reason: "new", CreatedAt: now.Add(time.Second)},
				} {
					if err := r.Coordination().CreateAdoption(ctx, a); err != nil {
						return domain.Result{}, err
					}
				}
				return domain.Result{ID: "seed-adoptions", Outcome: domain.OutcomeOK}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("DROP TRIGGER adoptions_no_update"); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(corrupt); err != nil {
				t.Fatal(err)
			}
			var got coord.Adoption
			var ok bool
			err = store.Read(ctx, func(r ports.Repositories) error {
				got, ok, err = r.Coordination().LatestGitAdoption(ctx, testsupport.Project, fingerprint)
				return err
			})
			if err == nil || ok || got != (coord.Adoption{}) {
				t.Fatalf("corrupt newest adoption returned %+v, %t, %v", got, ok, err)
			}
		})
	}
}

func TestGitAdoptionExplicitOwnerOverridesAmbiguityAndReplaysCanonically(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	store, db := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	seedRepresentableAdopter(t, store, now)
	const path = "/repo/ambiguous"
	_, _, err := store.Write(ctx, "seed-automatic-owners", "test.write", func(r ports.Repositories) (domain.Result, error) {
		for _, id := range []string{"auto-a", "auto-b"} {
			session := core.AgentSession{ID: core.ID(id), ProjectID: testsupport.Project, Kind: core.Imported, Runtime: "test", Role: "owner", Source: core.SourceImport, SourceRef: "test", RootID: core.ID(id), TaskID: "a", WorktreeRef: path, StartedAt: now}
			if err := r.Coordination().CreateSession(ctx, session); err != nil {
				return domain.Result{}, err
			}
			if err := r.Coordination().CreateRun(ctx, core.TaskRun{ID: core.ID(id + "-run"), TaskID: "a", SessionID: session.ID, State: core.RunRunning, StartedAt: now}); err != nil {
				return domain.Result{}, err
			}
		}
		return domain.Result{ID: "seed-automatic-owners", Outcome: domain.OutcomeOK}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := seedAdoptionSnapshot(t, store, now, "ambiguous-scan", path, []gitobs.Asset{adoptionAsset(path)})
	fingerprint := snapshot.Assets[0].Fingerprint
	service := New(store, func() time.Time { return now })
	adoption := coord.Adoption{ID: "explicit-owner", ProjectID: testsupport.Project, GitAssetID: fingerprint, NewOwnerSessionID: "adopter", Reason: "explicit overrides ambiguity"}
	first, err := service.Adopt(ctx, "explicit-owner-key", adoption)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := service.Adopt(ctx, "explicit-owner-key", coord.Adoption{ID: "different-id", ProjectID: testsupport.Project, GitAssetID: fingerprint, NewOwnerSessionID: "adopter", Reason: "different"})
	if err != nil || replay.ID != first.ID || !replay.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("replay=%+v err=%v want=%+v", replay, err, first)
	}
	current, err := appgit.New(store, nil, func() time.Time { return now }).RecordedCurrent(ctx, testsupport.Project)
	if err != nil {
		t.Fatal(err)
	}
	asset := current.Assets[0]
	if asset.OwnerSessionID != "adopter" || asset.OwnerTaskID != "a" || asset.OwnerRunID != "adopter-run" || asset.Facts.Owner.State != gitobs.OwnerActive {
		t.Fatalf("explicit owner not current: %+v", asset)
	}
	var adoptions, receipts, audits int
	if err := db.QueryRow("SELECT COUNT(*) FROM adoptions WHERE id IN ('explicit-owner','different-id')").Scan(&adoptions); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM command_receipts WHERE idempotency_key='explicit-owner-key'").Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM audit_events a JOIN command_receipts r ON r.id=a.receipt_id WHERE r.idempotency_key='explicit-owner-key' AND a.event_type='adoption.created'").Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if adoptions != 1 || receipts != 1 || audits > 1 {
		t.Fatalf("adoptions=%d receipts=%d audits=%d", adoptions, receipts, audits)
	}
}

func TestGitAdoptionRejectsForeignAndStaleLatestFingerprint(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	store, db := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	seedRepresentableAdopter(t, store, now)
	snapshot := seedAdoptionSnapshot(t, store, now, "first-scan", "/repo/stale", []gitobs.Asset{adoptionAsset("/repo/stale")})
	fingerprint := snapshot.Assets[0].Fingerprint
	testsupport.SeedForeign(t, store, db, now)
	_, _, err := store.Write(ctx, "foreign-adopter-lineage", "test.write", func(r ports.Repositories) (domain.Result, error) {
		session := core.AgentSession{ID: "foreign-adopter", ProjectID: "foreign", Kind: core.Imported, Runtime: "test", Role: "owner", Source: core.SourceImport, SourceRef: "test", RootID: "foreign-adopter", TaskID: "foreign-task", StartedAt: now}
		if err := r.Coordination().CreateSession(ctx, session); err != nil {
			return domain.Result{}, err
		}
		if err := r.Coordination().CreateRun(ctx, core.TaskRun{ID: "foreign-adopter-run", TaskID: "foreign-task", SessionID: "foreign-adopter", State: core.RunRunning, StartedAt: now}); err != nil {
			return domain.Result{}, err
		}
		return domain.Result{ID: "foreign-adopter-lineage", Outcome: domain.OutcomeOK}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	service := New(store, func() time.Time { return now })
	if _, err := service.Adopt(ctx, "foreign-fingerprint", coord.Adoption{ID: "foreign-fingerprint-adoption", ProjectID: "foreign", GitAssetID: fingerprint, NewOwnerSessionID: "foreign-adopter", Reason: "foreign fingerprint"}); err == nil {
		t.Fatal("foreign project adopted project A fingerprint")
	}
	seedAdoptionSnapshot(t, store, now.Add(time.Second), "second-scan", "/repo/stale", nil)
	if _, err := service.Adopt(ctx, "stale-fingerprint", coord.Adoption{ID: "stale-fingerprint-adoption", ProjectID: testsupport.Project, GitAssetID: fingerprint, NewOwnerSessionID: "adopter", Reason: "stale fingerprint"}); err == nil {
		t.Fatal("stale fingerprint adoption succeeded")
	}
	for _, key := range []string{"foreign-fingerprint", "stale-fingerprint"} {
		var receipts, audits int
		if err := db.QueryRow("SELECT COUNT(*) FROM command_receipts WHERE idempotency_key=?", key).Scan(&receipts); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow("SELECT COUNT(*) FROM audit_events a JOIN command_receipts r ON r.id=a.receipt_id WHERE r.idempotency_key=? AND a.event_type='adoption.created'", key).Scan(&audits); err != nil {
			t.Fatal(err)
		}
		if receipts != 0 || audits != 0 {
			t.Fatalf("%s receipts=%d audits=%d", key, receipts, audits)
		}
	}
}

func TestGitAdoptionRejectsSnapshotObservedAfterAdoptionTime(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 22, 12, 0, 0, 100_000_000, time.UTC)
	store, db := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	seedRepresentableAdopter(t, store, now)
	snapshot := seedAdoptionSnapshot(t, store, now.Add(10*time.Millisecond), "future-scan", "/repo/future", []gitobs.Asset{adoptionAsset("/repo/future")})
	service := New(store, func() time.Time { return now })
	key := "future-observation-adoption"
	if _, err := service.Adopt(ctx, domain.IdempotencyKey(key), coord.Adoption{ID: "future-adoption", ProjectID: testsupport.Project, GitAssetID: snapshot.Assets[0].Fingerprint, NewOwnerSessionID: "adopter", Reason: "observation is newer"}); err == nil {
		t.Fatal("adoption succeeded before its target observation existed")
	}
	var receipts int
	if err := db.QueryRow("SELECT COUNT(*) FROM command_receipts WHERE idempotency_key=?", key).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 0 {
		t.Fatalf("failed future adoption persisted %d receipts", receipts)
	}
}

func TestLatestGitAdoptionRejectsObservationAfterAdoptionWithinSameSecond(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	store, _ := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	snapshot := seedAdoptionSnapshot(t, store, now.Add(110*time.Millisecond), "late-provenance", "/repo/late", []gitobs.Asset{adoptionAsset("/repo/late")})
	fingerprint := snapshot.Assets[0].Fingerprint
	_, _, err := store.Write(ctx, "early-adoption", "test.write", func(r ports.Repositories) (domain.Result, error) {
		return domain.Result{ID: "early-adoption", Outcome: domain.OutcomeOK}, r.Coordination().CreateAdoption(ctx, coord.Adoption{ID: "early-adoption", ProjectID: testsupport.Project, GitAssetID: fingerprint, NewOwnerSessionID: "target", Reason: "before observation", CreatedAt: now.Add(100 * time.Millisecond)})
	})
	if err != nil {
		t.Fatal(err)
	}
	var got coord.Adoption
	var ok bool
	err = store.Read(ctx, func(r ports.Repositories) error {
		got, ok, err = r.Coordination().LatestGitAdoption(ctx, testsupport.Project, fingerprint)
		return err
	})
	if err == nil || ok || got != (coord.Adoption{}) {
		t.Fatalf("future observation accepted adoption: %+v, %t, %v", got, ok, err)
	}
}
