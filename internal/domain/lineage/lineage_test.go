package lineage

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRunCompletionNeedsEvidence(t *testing.T) {
	if CanTransitionRun(RunRunning, RunVerifiedDone, nil) {
		t.Fatal("verified done accepted without evidence")
	}
	if !CanTransitionRun(RunRunning, RunWorkComplete, nil) {
		t.Fatal("work complete rejected")
	}
	if ParentLossState(Heartbeat{Liveness: Interrupted}) == RunWorkComplete {
		t.Fatal("parent loss marked work complete")
	}
}
func TestOperationalStatesResumeAndTerminalStatesFailClosed(t *testing.T) {
	if !CanTransitionRun(RunWaiting, RunRunning, nil) || !CanTransitionRun(RunBlocked, RunRunning, nil) || !CanTransitionRun(RunRework, RunRunning, nil) {
		t.Fatal("operational run state cannot resume")
	}
	if CanTransitionRun(RunFailed, RunRunning, nil) || CanTransitionTask(TaskAbandoned, TaskInProgress, nil) {
		t.Fatal("terminal state reopened")
	}
	if CanTransitionTask(TaskWorkComplete, TaskVerifiedDone, nil) || !CanTransitionTask(TaskWorkComplete, TaskVerifiedDone, []byte(`{"proof":true}`)) {
		t.Fatal("task evidence gate broken")
	}
}
func TestLineageKindsValidate(t *testing.T) {
	for _, kind := range []LineageKind{HumanDirect, AgentDelegated, Resumed, Adopted, Imported} {
		if !validKind(kind) {
			t.Fatalf("kind %q invalid", kind)
		}
	}
}

func TestNativeSessionFingerprintIsLengthDelimited(t *testing.T) {
	first := NativeSessionFingerprint("codex", "ab", "c", nil)
	second := NativeSessionFingerprint("codex", "a", "bc", nil)
	if first == second {
		t.Fatal("field-boundary ambiguity produced the same fingerprint")
	}
	if first != NativeSessionFingerprint("codex", "ab", "c", nil) {
		t.Fatal("fingerprint is not deterministic")
	}
}

func TestNativeMetadataValidation(t *testing.T) {
	started := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	session := validNativeSession()
	session.NativeSessionStartedAt = &started
	session.NativeSessionFingerprint = NativeSessionFingerprint(session.Runtime, session.NativeSessionID, session.NativeSessionRef, session.NativeSessionStartedAt)
	if err := session.Validate(); err != nil {
		t.Fatalf("valid native metadata rejected: %v", err)
	}
	session.NativeSessionFingerprint = "ABC"
	if err := session.Validate(); err == nil {
		t.Fatal("uppercase short fingerprint accepted")
	}
	session = validNativeSession()
	session.NativeAccessState = NativeAccessUnsupported
	session.NativeSessionID = ""
	session.NativeSessionRef = ""
	session.NativeSessionFingerprint = ""
	if err := session.Validate(); err != nil {
		t.Fatalf("unsupported native metadata rejected: %v", err)
	}
	session.NativeSessionID = "contradiction"
	if err := session.Validate(); err == nil {
		t.Fatal("unsupported native locator accepted")
	}
}

func TestAgentSessionJSONOmitsPrivateLocators(t *testing.T) {
	session := AgentSession{
		SourceRef: "private-source-ref", WorktreeRef: "/private/worktree",
		NativeAccessState: NativeAccessAvailable, RuntimeHome: "/private/runtime",
		NativeSessionID: "native-id", NativeSessionRef: "opaque-native-ref",
		NativeSessionFingerprint: strings.Repeat("a", 64), NativeParentSessionID: "native-parent",
	}
	encoded, err := json.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"private-source-ref", "/private/worktree", "/private/runtime", "native-id", "opaque-native-ref", strings.Repeat("a", 64), "native-parent"} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("private session locator leaked: %q in %s", private, encoded)
		}
	}
	if !strings.Contains(string(encoded), `"native_access_state":"available"`) {
		t.Fatalf("safe access state missing: %s", encoded)
	}
}

func validNativeSession() AgentSession {
	return AgentSession{
		ID: "session-1", ProjectID: "project-1", HumanID: "human-1",
		Kind: HumanDirect, Runtime: "codex", Role: "operator", Source: SourceHuman,
		SourceRef: "human-1", RootID: "session-1", StartedAt: time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC),
		NativeAccessState: NativeAccessAvailable, NativeSessionID: "native-1", NativeSessionRef: "opaque-ref",
	}
}

func TestDelegationTokenRejectsLongTTL(t *testing.T) {
	issued := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	token := DelegationToken{
		ID: "token-1", ProjectID: "project-1", ParentSessionID: "session-1",
		Algorithm: "PBKDF2-HMAC-SHA256", Iterations: 100000, Salt: make([]byte, 16), Verifier: make([]byte, 32),
		IssuedAt: issued, ExpiresAt: issued.Add(MaxDelegationTTL + time.Second),
	}
	if token.Validate() == nil {
		t.Fatal("delegation token over maximum TTL accepted")
	}
}

func TestStableEntityIDsAndLinksRejectSecretLikeValues(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	session := validNativeSession()
	for _, mutate := range []func(*AgentSession){
		func(v *AgentSession) { v.ID = "password-session" },
		func(v *AgentSession) { v.ParentID = "/Users/me/private-parent" },
		func(v *AgentSession) { v.RootID = "token-root" },
		func(v *AgentSession) { v.TaskID = "C:\\private\\task" },
	} {
		candidate := session
		mutate(&candidate)
		if candidate.Validate() == nil {
			t.Fatalf("secret-like session identifier accepted: %+v", candidate)
		}
	}
	task := Task{ID: "task-1", ProjectID: "project-1", DisplayNumber: 1, Title: "password is allowed in a title", State: TaskReady, CreatedAt: now, UpdatedAt: now, ParentTaskID: "parent-1"}
	if err := task.Validate(); err != nil {
		t.Fatalf("benign task identifiers or title rejected: %v", err)
	}
	task.ParentTaskID = "secret-parent"
	if err := task.Validate(); err == nil {
		t.Fatal("secret-like task link accepted")
	}
	run := TaskRun{ID: "run-1", TaskID: "task-1", SessionID: "session-1", State: RunRunning, StartedAt: now, Supersedes: "/private/run"}
	if err := run.Validate(); err == nil {
		t.Fatal("private-path run link accepted")
	}
	heartbeat := Heartbeat{ID: "heartbeat-1", SessionID: "password-session", ObservedAt: now, Liveness: Alive}
	if err := heartbeat.Validate(); err == nil {
		t.Fatal("secret-like heartbeat link accepted")
	}
}
