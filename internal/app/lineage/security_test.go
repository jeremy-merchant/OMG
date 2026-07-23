package lineage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"example.invalid/coordledger/internal/domain"
	core "example.invalid/coordledger/internal/domain/lineage"
	"example.invalid/coordledger/internal/ports"
	"example.invalid/coordledger/internal/store/sqlite"
	_ "modernc.org/sqlite"
)

func TestDelegationTokenSecurity(t *testing.T) {
	ctx := context.Background()
	service, _, path, closeStore, now := securityService(t, 8)
	defer closeStore()
	root := securityRoot(t, ctx, service, "project-a")

	issued, err := service.IssueToken(ctx, "issue-1", "project-a", "", root.ID, time.Hour)
	if err != nil {
		t.Fatal("issue token failed")
	}
	if !strings.HasPrefix(issued.RawToken, "omgdt_v1_") || len(issued.RawToken) != len("omgdt_v1_")+43 || strings.ContainsAny(issued.RawToken, "+/=") {
		t.Fatal("token is not a >=256-bit URL-safe transport token")
	}
	if issued.Token.Algorithm != "PBKDF2-HMAC-SHA256" || issued.Token.Iterations < tokenIterations || len(issued.Token.Salt) != 0 || len(issued.Token.Verifier) != 0 {
		t.Fatal("returned token metadata exposed secret material or weak parameters")
	}
	if safe, err := json.Marshal(issued.Token); err != nil || containsSecret(safe, issued.RawToken) {
		t.Fatal("safe token metadata serializes raw token")
	}

	duplicate, err := service.IssueToken(ctx, "issue-1", "project-a", "", root.ID, time.Hour)
	if err != nil || duplicate.Token.ID != issued.Token.ID || duplicate.RawToken != "" {
		t.Fatal("duplicate token issuance was not idempotent")
	}
	if count := securityCount(t, path, "delegation_tokens"); count != 1 {
		t.Fatalf("token rows = %d; want 1", count)
	}

	second, err := service.IssueToken(ctx, "issue-2", "project-a", "", root.ID, time.Hour)
	if err != nil {
		t.Fatal("second token issue failed")
	}
	stored := securityTokenMetadata(t, path)
	if len(stored) != 2 || stored[0].Algorithm != "PBKDF2-HMAC-SHA256" || stored[0].Iterations < tokenIterations || len(stored[0].Salt) < 16 || len(stored[0].Verifier) != 32 || string(stored[0].Salt) == string(stored[1].Salt) || string(stored[0].Verifier) == string(stored[1].Verifier) {
		t.Fatal("stored token verifier metadata is invalid or not independently derived")
	}
	securityAssertSecretAbsent(t, path, issued.RawToken)
	securityAssertSecretAbsent(t, path, second.RawToken)

	child, err := service.RegisterDelegated(ctx, "redeem-1", issued.RawToken, delegatedSession("child"), "project-a", "", root.ID)
	if err != nil {
		t.Fatal("correct token redemption failed")
	}
	if child.HumanID != root.HumanID || child.RootID != root.ID || child.ParentID != root.ID {
		t.Fatal("child did not inherit exact root and human ancestry")
	}
	if _, err := service.RegisterDelegated(ctx, "redeem-replay", issued.RawToken, delegatedSession("replay"), "project-a", "", root.ID); securityCode(err) != domain.CodeConflict {
		t.Fatal("token replay did not return safe conflict")
	}
	if count := securityCount(t, path, "agent_sessions"); count != 2 {
		t.Fatalf("sessions after replay = %d; want 2", count)
	}

	grandToken, err := service.IssueToken(ctx, "issue-grand", "project-a", "", child.ID, time.Hour)
	if err != nil {
		t.Fatal("grandchild token issue failed")
	}
	grandchild, err := service.RegisterDelegated(ctx, "redeem-grand", grandToken.RawToken, delegatedSession("grandchild"), "project-a", "", child.ID)
	if err != nil {
		t.Fatal("grandchild redemption failed")
	}
	if grandchild.HumanID != root.HumanID || grandchild.RootID != root.ID || grandchild.ParentID != child.ID {
		t.Fatal("grandchild ancestry is not human→root→child→grandchild")
	}

	expired, err := service.IssueToken(ctx, "issue-expired", "project-a", "", root.ID, time.Minute)
	if err != nil {
		t.Fatal("expired token issue failed")
	}
	*now = now.Add(2 * time.Minute)
	securityRejectsWithoutSession(t, service, "expired", expired.RawToken, "project-a", "", root.ID, domain.CodeConflict, path)
	*now = now.Add(-2 * time.Minute)

	revoked, err := service.IssueToken(ctx, "issue-revoked", "project-a", "", root.ID, time.Hour)
	if err != nil {
		t.Fatal("revoked token issue failed")
	}
	if err := service.RevokeToken(ctx, "revoke", revoked.Token.ID); err != nil {
		t.Fatal("revoke token failed")
	}
	securityRejectsWithoutSession(t, service, "revoked", revoked.RawToken, "project-a", "", root.ID, domain.CodeConflict, path)
	securityRejectsWithoutSession(t, service, "malformed", "not-a-token", "project-a", "", root.ID, domain.CodeConflict, path)
	securityRejectsWithoutSession(t, service, "guessed", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "project-a", "", root.ID, domain.CodeConflict, path)
	securityRejectsWithoutSession(t, service, "wrong-project", second.RawToken, "project-b", "", root.ID, domain.CodeNotFound, path)
	securityRejectsWithoutSession(t, service, "wrong-parent", second.RawToken, "project-a", "", child.ID, domain.CodeConflict, path)
}

func TestDelegationTokenConcurrentRedemptionHasOneWinner(t *testing.T) {
	ctx := context.Background()
	service, _, path, closeStore, _ := securityService(t, 8)
	defer closeStore()
	root := securityRoot(t, ctx, service, "project-a")
	issued, err := service.IssueToken(ctx, "race-issue", "project-a", "", root.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	type redemption struct {
		session core.AgentSession
		err     error
	}
	const workers = 32
	start := make(chan struct{})
	results := make(chan redemption, workers)
	var group sync.WaitGroup
	for worker := range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			suffix := fmt.Sprintf("%02d", worker)
			session, redeemErr := service.RegisterDelegated(ctx, domain.IdempotencyKey("race-redeem-"+suffix), issued.RawToken,
				delegatedSession("race-child-"+suffix), "project-a", "", root.ID)
			results <- redemption{session: session, err: redeemErr}
		}()
	}
	close(start)
	group.Wait()
	close(results)

	successes, conflicts := 0, 0
	var winner core.AgentSession
	for result := range results {
		switch {
		case result.err == nil:
			successes++
			winner = result.session
		case securityCode(result.err) == domain.CodeConflict:
			conflicts++
		default:
			t.Fatalf("unexpected redemption error: %v", result.err)
		}
	}
	if successes != 1 || conflicts != workers-1 || winner.ParentID != root.ID {
		t.Fatalf("redemptions successes=%d conflicts=%d winner=%+v", successes, conflicts, winner)
	}
	if count := securityCount(t, path, "agent_sessions"); count != 2 {
		t.Fatalf("sessions after concurrent redemption = %d; want 2", count)
	}
	securityAssertSecretAbsent(t, path, issued.RawToken)
}

func TestNativeLineagePersistsWithoutReceiptOrAuditLeakage(t *testing.T) {
	ctx := context.Background()
	service, store, path, closeStore, _ := securityService(t, 8)
	defer closeStore()
	human, err := service.CreateHuman(ctx, "native-human", core.Human{DisplayName: "Native Human", Confidence: core.ConfidenceExplicit})
	if err != nil {
		t.Fatal(err)
	}
	rootInput := nativeTestSession("root-native")
	rootInput.ProjectID, rootInput.HumanID = "project-a", human.ID
	root, err := service.RegisterHumanDirect(ctx, "native-root", rootInput)
	if err != nil {
		t.Fatal(err)
	}
	resumeInput := nativeTestSession("resume-native")
	resumeInput.ContinuationOfID, resumeInput.NativeParentSessionID = root.ID, "native-parent-different"
	resumed, err := service.Resume(ctx, "native-resume", resumeInput)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.ContinuationOfID != root.ID || resumed.NativeParentSessionID != "native-parent-different" {
		t.Fatal("OMG continuation and native parent were conflated")
	}
	issued, err := service.IssueToken(ctx, "native-delegation-key", "project-a", "", root.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	delegated, err := service.RegisterDelegated(ctx, "native-delegated", issued.RawToken, nativeTestSession("delegated-native"), "project-a", "", root.ID)
	if err != nil {
		t.Fatal(err)
	}
	adoptInput := nativeTestSession("adopted-native")
	adoptInput.ParentID = root.ID
	adopted, err := service.Adopt(ctx, "native-adopted", adoptInput)
	if err != nil {
		t.Fatal(err)
	}
	importInput := nativeTestSession("imported-native")
	importInput.ProjectID = "project-a"
	imported, err := service.Import(ctx, "native-imported", importInput)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []core.ID{root.ID, resumed.ID, delegated.ID, adopted.ID, imported.ID} {
		var loaded core.AgentSession
		if err := store.Read(ctx, func(r ports.Repositories) error {
			var ok bool
			var err error
			loaded, ok, err = r.Coordination().GetSession(ctx, id)
			if err != nil {
				return err
			}
			if !ok {
				t.Fatal("persisted session not found")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if loaded.NativeSessionID == "" || loaded.NativeSessionFingerprint == "" || loaded.NativeSessionRef == "" || loaded.NativeAccessState != core.NativeAccessAvailable {
			t.Fatalf("native lineage did not load for %s: %+v", id, loaded)
		}
	}
	private := []string{"/private/native-runtime", "opaque-native-ref", "root-native", "resume-native", "delegated-native", "adopted-native", "imported-native"}
	db := securityDB(t, path)
	defer db.Close()
	for _, table := range []string{"command_receipts", "audit_events"} {
		rows, err := db.Query("SELECT " + map[string]string{"command_receipts": "result_json", "audit_events": "payload_json"}[table] + " FROM " + table)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var payload []byte
			if err := rows.Scan(&payload); err != nil {
				t.Fatal(err)
			}
			for _, value := range private {
				if strings.Contains(string(payload), value) {
					t.Fatalf("%s leaked private native value %q", table, value)
				}
			}
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		data, err := os.ReadFile(path + suffix)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		for _, value := range private {
			if strings.Contains(string(data), value) && !strings.Contains(string(data), "agent_sessions") {
				t.Fatalf("private native value %q appeared outside session storage", value)
			}
		}
	}
}

func TestResumeAndAdoptReplayIgnoreHostileAncestry(t *testing.T) {
	ctx := context.Background()
	service, _, path, closeStore, _ := securityService(t, 8)
	defer closeStore()
	root := securityRoot(t, ctx, service, "project-a")

	resume, err := service.Resume(ctx, "resume-replay", core.AgentSession{ID: "resume-original", ContinuationOfID: root.ID, Runtime: "test", Role: "owner", SourceRef: "resume-private-original"})
	if err != nil {
		t.Fatal(err)
	}
	replayedResume, err := service.Resume(ctx, "resume-replay", core.AgentSession{ID: "resume-caller-local", ProjectID: "project-b", ContinuationOfID: "missing-continuation", Runtime: "test", Role: "owner", SourceRef: "resume-private-replay"})
	if err != nil || replayedResume.ID != resume.ID || replayedResume.ContinuationOfID != root.ID {
		t.Fatalf("resume replay=%+v err=%v", replayedResume, err)
	}
	if count := securityCount(t, path, "agent_sessions"); count != 2 {
		t.Fatalf("resume replay created %d sessions; want 2", count)
	}
	if _, err := service.Adopt(ctx, "resume-replay", core.AgentSession{ID: "adopt-cross-command", ParentID: root.ID, Runtime: "test", Role: "owner", SourceRef: "adopt-cross-command"}); err == nil {
		t.Fatal("adopt reused resume key")
	}
	if count := securityCount(t, path, "agent_sessions"); count != 2 {
		t.Fatalf("cross-command adoption created %d sessions; want 2", count)
	}

	adopted, err := service.Adopt(ctx, "adopt-replay", core.AgentSession{ID: "adopt-original", ParentID: root.ID, Runtime: "test", Role: "owner", SourceRef: "adopt-private-original"})
	if err != nil {
		t.Fatal(err)
	}
	replayedAdopt, err := service.Adopt(ctx, "adopt-replay", core.AgentSession{ID: "adopt-caller-local", ProjectID: "project-b", ParentID: "missing-parent", Runtime: "test", Role: "owner", SourceRef: "adopt-private-replay"})
	if err != nil || replayedAdopt.ID != adopted.ID || replayedAdopt.ParentID != root.ID {
		t.Fatalf("adopt replay=%+v err=%v", replayedAdopt, err)
	}
	if count := securityCount(t, path, "agent_sessions"); count != 3 {
		t.Fatalf("adopt replay created %d sessions; want 3", count)
	}

	db := securityDB(t, path)
	defer db.Close()
	for _, query := range []string{"SELECT result_json FROM command_receipts", "SELECT payload_json FROM audit_events"} {
		rows, err := db.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var value string
			if err := rows.Scan(&value); err != nil {
				t.Fatal(err)
			}
			for _, private := range []string{"resume-private-original", "resume-private-replay", "adopt-private-original", "adopt-private-replay"} {
				if strings.Contains(value, private) {
					t.Fatalf("receipt or audit leaked private value %q", private)
				}
			}
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestResumeAndAdoptRejectLiveDelegationTokensBeforeAnyMutation(t *testing.T) {
	ctx := context.Background()
	service, _, path, closeStore, _ := securityService(t, 8)
	defer closeStore()
	root := securityRoot(t, ctx, service, "project-a")
	issued, err := service.IssueToken(ctx, "issue-live-delegation", root.ProjectID, "", root.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name    string
		key     domain.IdempotencyKey
		session core.AgentSession
		run     func(context.Context, domain.IdempotencyKey, core.AgentSession) (core.AgentSession, error)
	}{
		{
			name:    "resume source ref",
			key:     "resume-token-source-ref",
			session: core.AgentSession{ContinuationOfID: root.ID, Runtime: "test", Role: "owner", SourceRef: issued.RawToken},
			run:     service.Resume,
		},
		{
			name:    "resume idempotency key",
			key:     domain.IdempotencyKey(issued.RawToken),
			session: core.AgentSession{ContinuationOfID: root.ID, Runtime: "test", Role: "owner", SourceRef: "benign"},
			run:     service.Resume,
		},
		{
			name:    "adopt source ref",
			key:     "adopt-token-source-ref",
			session: core.AgentSession{ParentID: root.ID, Runtime: "test", Role: "owner", SourceRef: issued.RawToken},
			run:     service.Adopt,
		},
		{
			name:    "adopt idempotency key",
			key:     domain.IdempotencyKey(issued.RawToken),
			session: core.AgentSession{ParentID: root.ID, Runtime: "test", Role: "owner", SourceRef: "benign"},
			run:     service.Adopt,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := securityMutationCounts(t, path)
			if _, err := test.run(ctx, test.key, test.session); securityCode(err) != domain.CodeInvalidArgument {
				t.Fatalf("credential-bearing request error = %v; want invalid argument", err)
			}
			if after := securityMutationCounts(t, path); after != before {
				t.Fatalf("credential-bearing request mutated state: before=%v after=%v", before, after)
			}
		})
	}

	for _, test := range []struct {
		name    string
		key     domain.IdempotencyKey
		session core.AgentSession
		run     func(context.Context, domain.IdempotencyKey, core.AgentSession) (core.AgentSession, error)
	}{
		{name: "resume benign", key: "resume-benign", session: core.AgentSession{ContinuationOfID: root.ID, Runtime: "test", Role: "owner", SourceRef: "benign"}, run: service.Resume},
		{name: "adopt benign", key: "adopt-benign", session: core.AgentSession{ParentID: root.ID, Runtime: "test", Role: "owner", SourceRef: "benign"}, run: service.Adopt},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.run(ctx, test.key, test.session); err != nil {
				t.Fatalf("benign request failed: %v", err)
			}
		})
	}
}

func TestCreateHumanRejectsDelegationTokenSplitAcrossKeyAndDisplayName(t *testing.T) {
	ctx := context.Background()
	service, _, path, closeStore, _ := securityService(t, 8)
	defer closeStore()
	root := securityRoot(t, ctx, service, "project-a")
	issued, err := service.IssueToken(ctx, "issue-split-live", root.ProjectID, "", root.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.TrimPrefix(issued.RawToken, "omgdt_v1_")
	if len(body) != 43 {
		t.Fatalf("delegation token body length=%d, want 43", len(body))
	}
	before := securityMutationCounts(t, path)
	_, err = service.CreateHuman(ctx, "omgdt_", core.Human{
		ID:          "human-split",
		DisplayName: "v1_" + body,
		Confidence:  core.ConfidenceVerified,
	})
	if securityCode(err) != domain.CodeInvalidArgument {
		t.Fatalf("split delegation token error=%v, want invalid argument", err)
	}
	if after := securityMutationCounts(t, path); after != before {
		t.Fatalf("split delegation token mutated state: before=%v after=%v", before, after)
	}
}

func nativeTestSession(nativeID string) core.AgentSession {
	x := core.AgentSession{Runtime: "codex", Role: "owner", SourceRef: "native", NativeAccessState: core.NativeAccessAvailable, RuntimeHome: "/private/native-runtime", NativeSessionID: nativeID, NativeSessionRef: "opaque-native-ref"}
	x.NativeSessionFingerprint = core.NativeSessionFingerprint(x.Runtime, x.NativeSessionID, x.NativeSessionRef, nil)
	return x
}

func TestDelegationTaskBindingAndMaximumDepth(t *testing.T) {
	ctx := context.Background()
	service, store, path, closeStore, now := securityService(t, 2)
	defer closeStore()
	root := securityRoot(t, ctx, service, "project-a")
	if _, err := service.IssueToken(ctx, "over-ttl", "project-a", "", root.ID, core.MaxDelegationTTL+time.Second); securityCode(err) != domain.CodeInvalidArgument {
		t.Fatal("overlong delegation token TTL was accepted")
	}
	if count := securityCount(t, path, "delegation_tokens"); count != 0 {
		t.Fatal("overlong delegation token TTL created a token")
	}
	task, err := service.CreateTask(ctx, "task", core.Task{ProjectID: "project-a", Title: "bound"})
	if err != nil {
		t.Fatal("create task failed")
	}
	issued, err := service.IssueToken(ctx, "bound-issue", "project-a", task.ID, root.ID, time.Hour)
	if err != nil {
		t.Fatal("bound token issue failed")
	}
	securityRejectsWithoutSession(t, service, "wrong-task", issued.RawToken, "project-a", "other-task", root.ID, domain.CodeConflict, path)
	child, err := service.RegisterDelegated(ctx, "bound-redeem", issued.RawToken, delegatedSession("bound-child"), "project-a", task.ID, root.ID)
	if err != nil {
		t.Fatal("bound token redemption failed")
	}
	grandToken, err := service.IssueToken(ctx, "grand-issue", "project-a", "", child.ID, time.Hour)
	if err != nil {
		t.Fatal("grand token issue failed")
	}
	strict := NewWithOptions(store, func() time.Time { return *now }, Options{MaxDelegationDepth: 1})
	if _, err := strict.IssueToken(ctx, "over-depth-issue", "project-a", "", child.ID, time.Hour); securityCode(err) != domain.CodeConflict {
		t.Fatal("issuance beyond maximum delegation depth was accepted")
	}
	securityRejectsWithoutSession(t, strict, "over-depth-redeem", grandToken.RawToken, "project-a", "", child.ID, domain.CodeConflict, path)
}

func TestSessionCreateAndImportUseDistinctReceiptOperations(t *testing.T) {
	ctx := context.Background()
	service, _, path, closeStore, _ := securityService(t, 8)
	defer closeStore()
	securityRoot(t, ctx, service, "project-a")
	before := securityCount(t, path, "agent_sessions")

	_, err := service.Import(ctx, "root", core.AgentSession{
		ID:        "imported-with-reused-key",
		ProjectID: "project-a",
		Runtime:   "test",
		Role:      "imported",
		SourceRef: "test",
	})
	if securityCode(err) != domain.CodeConflict {
		t.Fatalf("cross-command key reuse error=%v, want conflict", err)
	}
	if after := securityCount(t, path, "agent_sessions"); after != before {
		t.Fatalf("cross-command key reuse mutated sessions: before=%d after=%d", before, after)
	}
}

func TestTransitionRunMissingRunDoesNotCreateReplayableReceipt(t *testing.T) {
	ctx := context.Background()
	service, _, path, closeStore, _ := securityService(t, 8)
	defer closeStore()
	root := securityRoot(t, ctx, service, "project-a")
	task, err := service.CreateTask(ctx, "task", core.Task{ProjectID: "project-a", Title: "run owner"})
	if err != nil {
		t.Fatal(err)
	}

	before := securityMutationCounts(t, path)
	run, err := service.TransitionRun(ctx, "missing-run-transition", "run-missing", core.RunWaiting, nil)
	if securityCode(err) != domain.CodeNotFound || run.ID != "" {
		t.Fatalf("missing transition returned run=%+v err=%v", run, err)
	}
	if after := securityMutationCounts(t, path); after != before {
		t.Fatalf("missing transition persisted artifacts: before=%+v after=%+v", before, after)
	}

	created, err := service.CreateRun(ctx, "create-run-after-missing-transition", core.TaskRun{
		ID:        "run-missing",
		TaskID:    task.ID,
		SessionID: root.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.State != core.RunRunning {
		t.Fatalf("created run state=%s; want %s", created.State, core.RunRunning)
	}

	replay, err := service.TransitionRun(ctx, "missing-run-transition", "run-missing", core.RunWaiting, nil)
	if err != nil || replay.State != core.RunWaiting {
		t.Fatalf("retry after failed missing transition was replayed instead of executed: run=%+v err=%v", replay, err)
	}
}

func TestMarkParentLossMissingRunRollsBackAndDoesNotPoisonKey(t *testing.T) {
	ctx := context.Background()
	service, _, path, closeStore, _ := securityService(t, 8)
	defer closeStore()
	root := securityRoot(t, ctx, service, "project-a")
	task, err := service.CreateTask(ctx, "parent-loss-missing-task", core.Task{ProjectID: "project-a", Title: "parent loss missing"})
	if err != nil {
		t.Fatal(err)
	}

	before := parentLossMutationCounts(t, path)
	run, err := service.MarkParentLoss(ctx, "parent-loss-missing", "missing-parent-loss-run", core.Heartbeat{
		SessionID: root.ID,
		Liveness:  core.Interrupted,
		Detail:    []byte("{}"),
	})
	if securityCode(err) != domain.CodeNotFound || run.ID != "" {
		t.Fatalf("missing parent loss returned run=%+v err=%v", run, err)
	}
	if after := parentLossMutationCounts(t, path); after != before {
		t.Fatalf("missing parent loss persisted artifacts: before=%+v after=%+v", before, after)
	}

	if _, err := service.CreateRun(ctx, "parent-loss-missing-create", core.TaskRun{
		ID:        "missing-parent-loss-run",
		TaskID:    task.ID,
		SessionID: root.ID,
	}); err != nil {
		t.Fatal(err)
	}
	retry, err := service.MarkParentLoss(ctx, "parent-loss-missing", "missing-parent-loss-run", core.Heartbeat{
		SessionID: root.ID,
		Liveness:  core.Interrupted,
		Detail:    []byte("{}"),
	})
	if err != nil || retry.State != core.RunInterrupted {
		t.Fatalf("retry after failed missing parent loss was replayed instead of executed: run=%+v err=%v", retry, err)
	}
}

type parentLossMutationCountsSnapshot struct {
	heartbeats int
	receipts   int
	events     int
}

func parentLossMutationCounts(t *testing.T, path string) parentLossMutationCountsSnapshot {
	t.Helper()
	return parentLossMutationCountsSnapshot{
		heartbeats: securityCount(t, path, "session_heartbeats"),
		receipts:   securityCount(t, path, "command_receipts"),
		events:     securityCount(t, path, "audit_events"),
	}
}

func TestMarkParentLossPersistsParentLossTimestamp(t *testing.T) {
	ctx := context.Background()
	service, _, _, closeStore, now := securityService(t, 8)
	defer closeStore()
	root := securityRoot(t, ctx, service, "project-a")
	task, err := service.CreateTask(ctx, "parent-loss-task", core.Task{ProjectID: "project-a", Title: "parent loss"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.CreateRun(ctx, "parent-loss-run-create", core.TaskRun{ID: "parent-loss-run", TaskID: task.ID, SessionID: root.ID}); err != nil {
		t.Fatal(err)
	}
	got, err := service.MarkParentLoss(ctx, "parent-loss-mark", "parent-loss-run", core.Heartbeat{SessionID: root.ID, Liveness: core.Interrupted, Detail: []byte("{}")})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != core.RunInterrupted || got.ParentLostAt == nil || !got.ParentLostAt.Equal(*now) || got.EndedAt == nil || !got.EndedAt.Equal(*now) {
		t.Fatalf("parent loss did not persist atomically: %+v", got)
	}
}

func TestParentLossRejectsMismatchedOwnerWithoutPoisoningKey(t *testing.T) {
	ctx := context.Background()
	service, _, path, closeStore, _ := securityService(t, 8)
	defer closeStore()
	root := securityRoot(t, ctx, service, "project-a")
	other, err := service.RegisterHumanDirect(ctx, "other-owner", core.AgentSession{ProjectID: "project-a", HumanID: root.HumanID, Runtime: "test", Role: "other", SourceRef: "other"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := service.CreateTask(ctx, "parent-loss-mismatch-task", core.Task{ProjectID: "project-a", Title: "parent loss mismatch"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateRun(ctx, "parent-loss-mismatch-run", core.TaskRun{ID: "parent-loss-mismatch-run", TaskID: task.ID, SessionID: root.ID}); err != nil {
		t.Fatal(err)
	}

	before := parentLossMutationCounts(t, path)
	if _, err := service.MarkParentLoss(ctx, "parent-loss-mismatch", "parent-loss-mismatch-run", core.Heartbeat{SessionID: other.ID, Liveness: core.Interrupted, Detail: []byte("{}")}); err == nil {
		t.Fatal("mismatched parent loss succeeded")
	}
	if after := parentLossMutationCounts(t, path); after != before {
		t.Fatalf("mismatched parent loss persisted artifacts: before=%+v after=%+v", before, after)
	}
	got, err := service.MarkParentLoss(ctx, "parent-loss-mismatch", "parent-loss-mismatch-run", core.Heartbeat{SessionID: root.ID, Liveness: core.Interrupted, Detail: []byte("{}")})
	if err != nil || got.State != core.RunInterrupted {
		t.Fatalf("valid retry after mismatch = %+v, %v", got, err)
	}
}

func TestStaleRunOwnerCannotCompleteButCanRepair(t *testing.T) {
	ctx := context.Background()
	service, _, path, closeStore, _ := securityService(t, 8)
	defer closeStore()
	root := securityRoot(t, ctx, service, "project-a")
	task, err := service.CreateTask(ctx, "stale-owner-task", core.Task{ProjectID: "project-a", Title: "stale owner"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateRun(ctx, "stale-owner-run", core.TaskRun{ID: "stale-owner-run", TaskID: task.ID, SessionID: root.ID}); err != nil {
		t.Fatal(err)
	}
	if err := service.Checkpoint(ctx, "stale-owner-checkpoint", core.Heartbeat{ID: "stale-owner-heartbeat", SessionID: root.ID, Liveness: core.Stale, Detail: []byte("{}")}); err != nil {
		t.Fatal(err)
	}

	before := securityMutationCounts(t, path)
	if _, err := service.TransitionRun(ctx, "stale-owner-complete", "stale-owner-run", core.RunWorkComplete, nil); err == nil {
		t.Fatal("stale owner completed run")
	}
	if _, err := service.TransitionRun(ctx, "stale-owner-verify", "stale-owner-run", core.RunVerifiedDone, []byte("evidence")); err == nil {
		t.Fatal("stale owner verified run")
	}
	if after := securityMutationCounts(t, path); after != before {
		t.Fatalf("blocked completion persisted artifacts: before=%+v after=%+v", before, after)
	}
	repaired, err := service.TransitionRun(ctx, "stale-owner-repair", "stale-owner-run", core.RunWaiting, nil)
	if err != nil || repaired.State != core.RunWaiting {
		t.Fatalf("stale owner repair = %+v, %v", repaired, err)
	}
	if _, err := service.TransitionRun(ctx, "stale-owner-resume", "stale-owner-run", core.RunRunning, nil); err != nil {
		t.Fatal(err)
	}
	if err := service.Interrupt(ctx, "owner-interrupt", core.Heartbeat{ID: "owner-interrupted", SessionID: root.ID, Detail: []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	before = securityMutationCounts(t, path)
	if err := service.Checkpoint(ctx, "interrupted-owner-revival", core.Heartbeat{ID: "owner-revival", SessionID: root.ID, Liveness: core.Alive, Detail: []byte("{}")}); err == nil {
		t.Fatal("interrupted owner accepted revival heartbeat")
	}
	if _, err := service.TransitionRun(ctx, "interrupted-owner-complete", "stale-owner-run", core.RunWorkComplete, nil); err == nil {
		t.Fatal("interrupted owner completed run")
	}
	if after := securityMutationCounts(t, path); after != before {
		t.Fatalf("interrupted owner rejection persisted artifacts: before=%+v after=%+v", before, after)
	}
}

func TestCreateRunRejectsTerminalTaskAndInterruptedSession(t *testing.T) {
	ctx := context.Background()

	t.Run("terminal task", func(t *testing.T) {
		service, _, _, closeStore, _ := securityService(t, 8)
		defer closeStore()
		root := securityRoot(t, ctx, service, "project-a")
		task, err := service.CreateTask(ctx, "terminal-task-create", core.Task{ProjectID: "project-a", Title: "terminal"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = service.Claim(ctx, "terminal-task-claim", task.ID, root.ID); err != nil {
			t.Fatal(err)
		}
		if _, err = service.TransitionTask(ctx, "terminal-task-complete", task.ID, core.TaskWorkComplete, nil); err != nil {
			t.Fatal(err)
		}
		if _, err = service.TransitionTask(ctx, "terminal-task-verify", task.ID, core.TaskVerifiedDone, []byte("proof")); err != nil {
			t.Fatal(err)
		}

		_, err = service.CreateRun(ctx, "terminal-task-run", core.TaskRun{ID: "terminal-task-run", TaskID: task.ID, SessionID: root.ID})
		if securityCode(err) != domain.CodeConflict {
			t.Fatalf("terminal task CreateRun error=%v; want conflict", err)
		}
	})

	t.Run("interrupted session", func(t *testing.T) {
		service, _, _, closeStore, _ := securityService(t, 8)
		defer closeStore()
		root := securityRoot(t, ctx, service, "project-a")
		task, err := service.CreateTask(ctx, "interrupted-session-task", core.Task{ProjectID: "project-a", Title: "interrupted"})
		if err != nil {
			t.Fatal(err)
		}
		if err = service.Interrupt(ctx, "interrupted-session", core.Heartbeat{ID: "interrupted-session-heartbeat", SessionID: root.ID, Detail: []byte("{}")}); err != nil {
			t.Fatal(err)
		}

		_, err = service.CreateRun(ctx, "interrupted-session-run", core.TaskRun{ID: "interrupted-session-run", TaskID: task.ID, SessionID: root.ID})
		if securityCode(err) != domain.CodeConflict {
			t.Fatalf("interrupted session CreateRun error=%v; want conflict", err)
		}
	})
}

func TestTransitionRunMapsForbiddenAndUnknownStatesToClientErrors(t *testing.T) {
	ctx := context.Background()
	service, _, _, closeStore, _ := securityService(t, 8)
	defer closeStore()
	root := securityRoot(t, ctx, service, "project-a")
	task, err := service.CreateTask(ctx, "transition-client-error-task", core.Task{ProjectID: "project-a", Title: "transition errors"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.CreateRun(ctx, "transition-client-error-run", core.TaskRun{ID: "transition-client-error-run", TaskID: task.ID, SessionID: root.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.TransitionRun(ctx, "transition-client-error-complete", "transition-client-error-run", core.RunWorkComplete, nil); err != nil {
		t.Fatal(err)
	}

	if _, err = service.TransitionRun(ctx, "transition-client-error-forbidden", "transition-client-error-run", core.RunRunning, nil); securityCode(err) != domain.CodeConflict {
		t.Fatalf("WORK_COMPLETE→RUNNING error=%v; want conflict", err)
	}
	if _, err = service.TransitionRun(ctx, "transition-client-error-unknown", "transition-client-error-run", core.RunState("UNKNOWN"), nil); securityCode(err) != domain.CodeInvalidArgument {
		t.Fatalf("unknown run state error=%v; want invalid argument", err)
	}
}
func securityService(t *testing.T, maxDepth int) (*Service, *sqlite.SQLiteStore, string, func(), *time.Time) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	store, _, err := sqlite.Open(ctx, path, sqlite.OpenOptions{WALEligible: func(string) bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := store.PlanMigrations(ctx, "project-a")
	if err != nil {
		t.Fatal(err)
	}
	backup, err := store.CreateMigrationBackup(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	approvedAt := time.Now().UTC()
	approval := sqlite.Approval{ApprovalID: "approval-project-a", ApprovedBy: "test", EvidenceReference: "test", PlanID: plan.ID, Project: plan.Project, FromVersion: plan.FromVersion, ToVersion: plan.ToVersion, Checksums: plan.Checksums, BackupLocation: backup.Location, BackupChecksum: backup.Checksum, Command: "omg migration apply", Timestamp: approvedAt, ExpiresAt: approvedAt.Add(5 * time.Minute)}
	if err := store.ApplyMigrations(ctx, plan, approval); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	return NewWithOptions(store, func() time.Time { return now }, Options{MaxDelegationDepth: maxDepth}), store, path, func() { _ = store.Close() }, &now
}

func securityRoot(t *testing.T, ctx context.Context, service *Service, project core.ID) core.AgentSession {
	t.Helper()
	human, err := service.CreateHuman(ctx, "human", core.Human{DisplayName: "Test Human", Confidence: core.ConfidenceExplicit})
	if err != nil {
		t.Fatal(err)
	}
	root, err := service.RegisterHumanDirect(ctx, "root", core.AgentSession{ProjectID: project, HumanID: human.ID, Runtime: "test", Role: "owner", SourceRef: "test"})
	if err != nil {
		t.Fatal("create root session failed")
	}
	return root
}

func delegatedSession(ref string) core.AgentSession {
	return core.AgentSession{Runtime: "test", Role: "delegate", SourceRef: ref}
}

func securityRejectsWithoutSession(t *testing.T, service *Service, key domain.IdempotencyKey, raw string, project, task, parent core.ID, want domain.ErrorCode, path string) {
	t.Helper()
	before := securityCount(t, path, "agent_sessions")
	_, err := service.RegisterDelegated(context.Background(), key, raw, delegatedSession(string(key)), project, task, parent)
	if securityCode(err) != want {
		t.Fatal("token rejection did not return the expected safe error")
	}
	if after := securityCount(t, path, "agent_sessions"); after != before {
		t.Fatal("rejected token created a partial session")
	}
}

func securityCode(err error) domain.ErrorCode {
	var de domain.DomainError
	if errors.As(err, &de) {
		return de.Code
	}
	return ""
}

type securityMutationCountsSnapshot struct {
	sessions int
	receipts int
	events   int
}

func securityMutationCounts(t *testing.T, path string) securityMutationCountsSnapshot {
	t.Helper()
	return securityMutationCountsSnapshot{
		sessions: securityCount(t, path, "agent_sessions"),
		receipts: securityCount(t, path, "command_receipts"),
		events:   securityCount(t, path, "audit_events"),
	}
}
func securityCount(t *testing.T, path, table string) int {
	t.Helper()
	db := securityDB(t, path)
	defer db.Close()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func securityTokenMetadata(t *testing.T, path string) []core.DelegationToken {
	t.Helper()
	db := securityDB(t, path)
	defer db.Close()
	rows, err := db.Query("SELECT algorithm,iterations,salt,verifier FROM delegation_tokens ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []core.DelegationToken
	for rows.Next() {
		var token core.DelegationToken
		if err := rows.Scan(&token.Algorithm, &token.Iterations, &token.Salt, &token.Verifier); err != nil {
			t.Fatal(err)
		}
		out = append(out, token)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
func securityDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	return db
}
func containsSecret(data []byte, secret string) bool {
	return secret != "" && strings.Contains(string(data), secret)
}
func securityAssertSecretAbsent(t *testing.T, path, secret string) {
	t.Helper()
	paths := []string{path, path + "-wal", path + "-shm"}
	backupDir := filepath.Join(filepath.Dir(path), "backups")
	if entries, err := os.ReadDir(backupDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				paths = append(paths, filepath.Join(backupDir, entry.Name()))
			}
		}
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, candidate := range paths {
		data, err := os.ReadFile(candidate)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if containsSecret(data, secret) {
			t.Fatal("raw token persisted")
		}
	}
	db := securityDB(t, path)
	defer db.Close()
	var receipt string
	if err := db.QueryRow("SELECT result_json FROM command_receipts LIMIT 1").Scan(&receipt); err != nil {
		t.Fatal(err)
	}
	if containsSecret([]byte(receipt), secret) {
		t.Fatal("raw token persisted in command receipt")
	}
}

func TestCreateHumanReplayReturnsCanonicalAndRedactsPrivateInput(t *testing.T) {
	ctx := context.Background()
	service, _, path, closeStore, _ := securityService(t, 8)
	defer closeStore()
	first, err := service.CreateHuman(ctx, "human-replay", core.Human{ID: "human-original", DisplayName: "private-original-name", Confidence: core.ConfidenceExplicit})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := service.CreateHuman(ctx, "human-replay", core.Human{ID: "human-caller-local", DisplayName: "private-replay-name", Confidence: core.ConfidenceVerified})
	if err != nil || replay.ID != first.ID || replay.DisplayName != first.DisplayName {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	if securityCount(t, path, "humans") != 1 {
		t.Fatal("duplicate human row")
	}
	db := securityDB(t, path)
	defer db.Close()
	for _, q := range []string{"SELECT result_json FROM command_receipts", "SELECT payload_json FROM audit_events"} {
		rows, err := db.Query(q)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err != nil {
				t.Fatal(err)
			}
			for _, private := range []string{"private-original-name", "private-replay-name"} {
				if strings.Contains(v, private) {
					t.Fatalf("private value leaked: %s", private)
				}
			}
		}
		rows.Close()
	}
}
