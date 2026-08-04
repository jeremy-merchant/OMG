package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jeremy-merchant/oh-my-group/internal/app/foundation"
	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	gitobs "github.com/jeremy-merchant/oh-my-group/internal/domain/git"
	"github.com/jeremy-merchant/oh-my-group/internal/domain/lineage"
	res "github.com/jeremy-merchant/oh-my-group/internal/domain/reservation"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
	"github.com/jeremy-merchant/oh-my-group/internal/store/sqlite"
)

type recoveryScanner struct{ calls int }

func (s *recoveryScanner) Scan(context.Context, string) (gitobs.Observation, error) {
	s.calls++
	return gitobs.Observation{Revision: gitobs.ObservationRevision, Hash: "safe-observation", Repository: gitobs.RepoWorktree, Confidence: gitobs.ConfidenceObserved}, nil
}

type recoveryPathInspector struct{}

func (recoveryPathInspector) FreshDestination(string) bool {
	return false
}

func (recoveryPathInspector) SameDirectory(candidate, selected string) bool {
	return candidate == selected
}

type recoveryResolver struct{ resolved ports.ResolvedStore }

func (r recoveryResolver) Resolve(context.Context, ports.ResolveRequest) (ports.ResolvedStore, error) {
	return r.resolved, nil
}

func recoverySQLiteOpen(ctx context.Context, path string, options ports.OpenOptions) (ports.FoundationStore, ports.OpenStatus, error) {
	return sqlite.Open(ctx, path, options)
}

func recoveryDispatcher(t *testing.T) (*ServiceDispatcher, foundation.Selection, *recoveryScanner) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	selection := foundation.Selection{Project: root}
	service := foundation.New(foundation.Dependencies{Resolver: recoveryResolver{resolved: ports.ResolvedStore{Path: root + "/state.db", Project: "recovery-dispatch", ProjectRoot: root}}, Open: recoverySQLiteOpen})
	store, _, openErr := sqlite.Open(ctx, root+"/state.db", sqlite.OpenOptions{Now: time.Now})
	if openErr != nil {
		t.Fatal(openErr)
	}
	plan, planErr := store.PlanMigrations(ctx, "recovery-dispatch")
	if planErr != nil {
		t.Fatal(planErr)
	}
	backup, backupErr := store.CreateMigrationBackup(ctx, plan)
	if backupErr != nil {
		t.Fatal(backupErr)
	}
	now := time.Now().UTC()
	approval := sqlite.Approval{ApprovalID: "approval-dispatch-recovery", ApprovedBy: "test", EvidenceReference: "dispatch-recovery", PlanID: plan.ID, Project: plan.Project, FromVersion: plan.FromVersion, ToVersion: plan.ToVersion, Checksums: plan.Checksums, BackupLocation: plan.BackupLocation, BackupChecksum: backup.Checksum, Command: "omg migration apply", Timestamp: now, ExpiresAt: now.Add(5 * time.Minute)}
	if applyErr := store.ApplyMigrations(ctx, plan, approval); applyErr != nil {
		t.Fatal(applyErr)
	}
	if closeErr := store.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	seedRecoveryDispatcherLineage(t, service, selection)
	scanner := &recoveryScanner{}
	return NewDispatcherWithGitScanner(service, scanner, recoveryPathInspector{}), selection, scanner
}

func seedRecoveryDispatcherLineage(t *testing.T, service *foundation.Service, selection foundation.Selection) {
	t.Helper()
	err := service.WithCurrentStore(context.Background(), selection, func(resolved ports.ResolvedStore, store ports.Store) error {
		_, _, err := store.Write(context.Background(), "seed-dispatch-recovery", "test.write", func(r ports.Repositories) (domain.Result, error) {
			c, now := r.Coordination(), time.Now().UTC()
			if err := c.CreateHuman(context.Background(), lineage.Human{ID: "human", DisplayName: "Human", Confidence: lineage.ConfidenceVerified, CreatedAt: now}); err != nil {
				return domain.Result{}, err
			}
			session := lineage.AgentSession{ID: "session", ProjectID: lineage.ID(resolved.Project), HumanID: "human", Kind: lineage.HumanDirect, Runtime: "test", Role: "owner", Source: lineage.SourceHuman, SourceRef: "test", RootID: "session", StartedAt: now, NativeAccessState: lineage.NativeAccessUnsupported}
			if err := c.CreateSession(context.Background(), session); err != nil {
				return domain.Result{}, err
			}
			task, err := c.CreateTask(context.Background(), lineage.Task{ID: "task", ProjectID: lineage.ID(resolved.Project), DisplayNumber: 1, Title: "Recover", State: lineage.TaskReady, CreatedBySessionID: session.ID, CreatedAt: now, UpdatedAt: now})
			if err != nil {
				return domain.Result{}, err
			}
			if err := c.CreateRun(context.Background(), lineage.TaskRun{ID: "run", TaskID: task.ID, SessionID: session.ID, State: lineage.RunRunning, StartedAt: now}); err != nil {
				return domain.Result{}, err
			}
			return domain.Result{ID: "seed-dispatch-recovery", Outcome: domain.OutcomeOK}, nil
		})
		return err
	})
	if err.Code != "" {
		t.Fatal(err)
	}
}

func TestDispatchRecoveryEnforcesReservationQueryAndMutationKeys(t *testing.T) {
	dispatcher, selection, _ := recoveryDispatcher(t)
	base := Request{Version: RequestVersion, Project: selection.Project}
	query := base
	query.Command, query.Payload = "reserve.list", []byte(`{}`)
	if result, owned := dispatcher.dispatchRecovery(context.Background(), query, selection); !owned || result.Error.Code != "" {
		t.Fatalf("query result=%+v owned=%t", result, owned)
	}
	query.IdempotencyKey = "not-allowed"
	if result, owned := dispatcher.dispatchRecovery(context.Background(), query, selection); !owned || result.Error.Code != domain.CodeInvalidArgument {
		t.Fatalf("keyed query result=%+v owned=%t", result, owned)
	}
	mutation := base
	mutation.Command, mutation.Payload = "reserve.add", []byte(`{"id":"reserve","pattern_kind":"exact","pattern":"internal/recovery.go","case_sensitivity":"sensitive","mode":"exclusive","human_id":"human","session_id":"session","task_id":"task","run_id":"run","intent":"repair","ttl_seconds":60}`)
	if result, owned := dispatcher.dispatchRecovery(context.Background(), mutation, selection); !owned || result.Error.Code != domain.CodeInvalidArgument {
		t.Fatalf("unkeyed mutation result=%+v owned=%t", result, owned)
	}
	mutation.IdempotencyKey = "reserve-add"
	if result, owned := dispatcher.dispatchRecovery(context.Background(), mutation, selection); !owned || result.Error.Code != "" {
		t.Fatalf("keyed mutation result=%+v owned=%t", result, owned)
	}
}

func TestSafeReservationsRedactsSecretLikePatternsWithoutMutatingRecords(t *testing.T) {
	records := []res.Reservation{
		{ID: "secret-pattern", Pattern: res.Pattern{Kind: res.Exact, Value: "api_key=release", CaseSensitivity: res.CaseSensitive}, Mode: res.Exclusive},
		{ID: "safe-pattern", Pattern: res.Pattern{Kind: res.Exact, Value: "internal/app/recovery.go", CaseSensitivity: res.CaseSensitive}, Mode: res.Exclusive},
	}
	projectedBeforeJSON := safeReservations(records)
	if projectedBeforeJSON[0].ID != records[0].ID || projectedBeforeJSON[0].Kind != string(records[0].Pattern.Kind) || projectedBeforeJSON[0].Mode != string(records[0].Mode) {
		t.Fatalf("projection changed reservation identity or lifecycle metadata: %#v", projectedBeforeJSON[0])
	}
	encoded, err := json.Marshal(projectedBeforeJSON)
	if err != nil {
		t.Fatal(err)
	}
	var projected []ReservationResult
	if err := json.Unmarshal(encoded, &projected); err != nil {
		t.Fatal(err)
	}
	patterns := map[string]string{}
	for _, item := range projected {
		patterns[item.ID] = item.Pattern
	}
	if patterns["secret-pattern"] == "api_key=release" || patterns["secret-pattern"] == "" {
		t.Fatalf("secret pattern leaked or erased: %#v", patterns)
	}
	if patterns["safe-pattern"] != "internal/app/recovery.go" {
		t.Fatalf("safe pattern = %q", patterns["safe-pattern"])
	}
	if records[0].Pattern.Value != "api_key=release" {
		t.Fatalf("projection mutated persisted pattern: %#v", records[0])
	}
}

func TestDispatchRecoveryGitCurrentIsLiveAndLedgerFree(t *testing.T) {
	dispatcher, selection, scanner := recoveryDispatcher(t)
	current := Request{Version: RequestVersion, Command: "git.current", Project: selection.Project, Payload: []byte(`{}`)}
	result, owned := dispatcher.dispatchRecovery(context.Background(), current, selection)
	live, ok := result.Data.(GitSnapshotResult)
	if !owned || result.Error.Code != "" || !ok {
		t.Fatalf("current result=%+v owned=%t", result, owned)
	}
	if live.Source != "git_live" || live.Durable || live.AuthoritativeSource != "git" || scanner.calls != 1 {
		t.Fatalf("live result=%+v scanner calls=%d", live, scanner.calls)
	}

	history := Request{Version: RequestVersion, Command: "git.history", Project: selection.Project, Payload: []byte(`{}`)}
	historyResult, owned := dispatcher.dispatchRecovery(context.Background(), history, selection)
	items, ok := historyResult.Data.([]GitSnapshotResult)
	if !owned || historyResult.Error.Code != "" || !ok || len(items) != 0 {
		t.Fatalf("current persisted observation: result=%+v owned=%t", historyResult, owned)
	}
}

func TestDispatchRecoveryGitCleanupPlanUsesLiveGitWithoutPersisting(t *testing.T) {
	dispatcher, selection, scanner := recoveryDispatcher(t)
	inventory := Request{Version: RequestVersion, Command: "git.inventory", Project: selection.Project, IdempotencyKey: "inventory", Payload: recoveryInventoryPayload(t, selection.Project, true)}
	if result, owned := dispatcher.dispatchRecovery(context.Background(), inventory, selection); !owned || result.Error.Code != "" {
		t.Fatalf("inventory result=%+v owned=%t", result, owned)
	}
	calls := scanner.calls
	cleanup := Request{Version: RequestVersion, Command: "git.cleanup-plan", Project: selection.Project, Payload: []byte(`{"fingerprint":""}`)}
	result, owned := dispatcher.dispatchRecovery(context.Background(), cleanup, selection)
	if !owned || result.Error.Code != "" {
		t.Fatalf("cleanup result=%+v owned=%t", result, owned)
	}
	if scanner.calls != calls+1 {
		t.Fatalf("cleanup scanner calls: before=%d after=%d", calls, scanner.calls)
	}
	data, ok := result.Data.(GitCleanupPlanResult)
	if !ok || !data.Advisory {
		t.Fatalf("cleanup data=%#v", result.Data)
	}
}

func TestDispatchRecoveryGitInventoryReplaysCanonicalResultWithoutDuplicateSnapshot(t *testing.T) {
	dispatcher, selection, _ := recoveryDispatcher(t)
	inventory := Request{
		Version:        RequestVersion,
		Command:        "git.inventory",
		Project:        selection.Project,
		IdempotencyKey: "replayed-inventory",
		Payload:        recoveryInventoryPayload(t, selection.Project, true),
	}

	first, owned := dispatcher.dispatchRecovery(context.Background(), inventory, selection)
	if !owned || first.Error.Code != "" {
		t.Fatalf("first inventory result=%+v owned=%t", first, owned)
	}
	second, owned := dispatcher.dispatchRecovery(context.Background(), inventory, selection)
	if !owned || second.Error.Code != "" {
		t.Fatalf("replayed inventory result=%+v owned=%t", second, owned)
	}
	firstSnapshot, ok := first.Data.(GitSnapshotResult)
	if !ok {
		t.Fatalf("first data = %T; want GitSnapshotResult", first.Data)
	}
	secondSnapshot, ok := second.Data.(GitSnapshotResult)
	if !ok {
		t.Fatalf("replayed data = %T; want GitSnapshotResult", second.Data)
	}
	if secondSnapshot != firstSnapshot {
		t.Fatalf("replayed snapshot = %+v; want %+v", secondSnapshot, firstSnapshot)
	}

	history := Request{Version: RequestVersion, Command: "git.history", Project: selection.Project, Payload: []byte(`{}`)}
	result, owned := dispatcher.dispatchRecovery(context.Background(), history, selection)
	if !owned || result.Error.Code != "" {
		t.Fatalf("history result=%+v owned=%t", result, owned)
	}
	snapshots, ok := result.Data.([]GitSnapshotResult)
	if !ok || len(snapshots) != 1 {
		t.Fatalf("history data = %#v; want one snapshot", result.Data)
	}
	if snapshots[0].ObservationID != firstSnapshot.ObservationID {
		t.Fatalf("persisted snapshot id = %q; want %q", snapshots[0].ObservationID, firstSnapshot.ObservationID)
	}
}

func TestDispatchRecoveryGitQueriesAcceptCompatibilityHintAndDefaultDiffBounds(t *testing.T) {
	dispatcher, selection, scanner := recoveryDispatcher(t)
	inventory := func(key string) GitSnapshotResult {
		t.Helper()
		result, owned := dispatcher.dispatchRecovery(context.Background(), Request{
			Version:        RequestVersion,
			Command:        "git.inventory",
			Project:        selection.Project,
			IdempotencyKey: key,
			Payload:        recoveryInventoryPayload(t, selection.Project, true),
		}, selection)
		snapshot, ok := result.Data.(GitSnapshotResult)
		if !owned || result.Error.Code != "" || !ok {
			t.Fatalf("inventory result=%+v owned=%t", result, owned)
		}
		return snapshot
	}
	first := inventory("git-query-first")
	second := inventory("git-query-second")

	current, owned := dispatcher.dispatchRecovery(context.Background(), Request{
		Version: RequestVersion,
		Command: "git.current",
		Project: selection.Project,
		Payload: []byte(`{"session_id":"compatibility-hint"}`),
	}, selection)
	live, ok := current.Data.(GitSnapshotResult)
	if !owned || current.Error.Code != "" || !ok || live.Source != "git_live" || live.Durable || live.AuthoritativeSource != "git" {
		t.Fatalf("current result=%+v owned=%t", current, owned)
	}
	if scanner.calls != 3 {
		t.Fatalf("current did not inspect live Git: calls=%d", scanner.calls)
	}

	latest, owned := dispatcher.dispatchRecovery(context.Background(), Request{
		Version: RequestVersion,
		Command: "git.latest",
		Project: selection.Project,
		Payload: []byte(`{"session_id":"compatibility-hint"}`),
	}, selection)
	if snapshot, ok := latest.Data.(GitSnapshotResult); !owned || latest.Error.Code != "" || !ok || snapshot.ObservationID != second.ObservationID || snapshot.Source != "recorded_evidence" || !snapshot.Durable || snapshot.AuthoritativeSource != "git" {
		t.Fatalf("latest result=%+v owned=%t", latest, owned)
	}

	result, owned := dispatcher.dispatchRecovery(context.Background(), Request{
		Version: RequestVersion,
		Command: "git.diff",
		Project: selection.Project,
		Payload: []byte(`{}`),
	}, selection)
	diff, ok := result.Data.(GitDiffResult)
	if !owned || result.Error.Code != "" || !ok {
		t.Fatalf("diff result=%+v owned=%t", result, owned)
	}
	if diff.Before != first.ObservationID || diff.After != second.ObservationID {
		t.Fatalf("diff bounds=%+v; want %q -> %q", diff, first.ObservationID, second.ObservationID)
	}
}

func TestDispatchRecoveryGitInventoryAllowsUnattributedAndRejectsPartialLineage(t *testing.T) {
	dispatcher, selection, scanner := recoveryDispatcher(t)
	unattributed := Request{Version: RequestVersion, Command: "git.inventory", Project: selection.Project, IdempotencyKey: "unattributed-inventory", Payload: recoveryInventoryPayload(t, selection.Project, false)}
	if result, owned := dispatcher.dispatchRecovery(context.Background(), unattributed, selection); !owned || result.Error.Code != "" {
		t.Fatalf("unattributed inventory result=%+v owned=%t", result, owned)
	}
	if scanner.calls != 1 {
		t.Fatalf("scanner calls = %d; want 1", scanner.calls)
	}
	partialPayload, err := json.Marshal(gitInventoryPayload{SessionID: "session", Directory: selection.Project})
	if err != nil {
		t.Fatal(err)
	}
	partial := Request{Version: RequestVersion, Command: "git.inventory", Project: selection.Project, IdempotencyKey: "partial-inventory", Payload: partialPayload}
	if result, owned := dispatcher.dispatchRecovery(context.Background(), partial, selection); !owned || result.Error.Code != domain.CodeInvalidArgument {
		t.Fatalf("partial inventory result=%+v owned=%t", result, owned)
	}
	if scanner.calls != 1 {
		t.Fatalf("partial attribution invoked scanner: calls=%d", scanner.calls)
	}
}

func TestDispatchRecoveryGitInventoryRejectsUnselectedRepository(t *testing.T) {
	dispatcher, selection, scanner := recoveryDispatcher(t)
	other := t.TempDir()
	request := Request{
		Version:        RequestVersion,
		Command:        "git.inventory",
		Project:        selection.Project,
		IdempotencyKey: "cross-project-inventory",
		Payload:        recoveryInventoryPayload(t, other, false),
	}
	result, owned := dispatcher.dispatchRecovery(context.Background(), request, selection)
	if !owned || result.Error.Code != domain.CodeInvalidArgument {
		t.Fatalf("cross-project inventory result=%+v owned=%t", result, owned)
	}
	if scanner.calls != 0 {
		t.Fatalf("cross-project inventory invoked scanner: calls=%d", scanner.calls)
	}
}

func recoveryInventoryPayload(t *testing.T, directory string, attributed bool) []byte {
	t.Helper()
	payload := gitInventoryPayload{Directory: directory}
	if attributed {
		payload.SessionID, payload.TaskID, payload.RunID = "session", "task", "run"
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
