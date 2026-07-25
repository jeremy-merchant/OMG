package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode"

	"example.invalid/coordledger/internal/app"
	"example.invalid/coordledger/internal/app/foundation"
	"example.invalid/coordledger/internal/app/query"
	"example.invalid/coordledger/internal/bootstrap"
	"example.invalid/coordledger/internal/domain"
	"example.invalid/coordledger/internal/domain/lineage"
	"example.invalid/coordledger/internal/platform"
	"example.invalid/coordledger/internal/ports"
	"example.invalid/coordledger/internal/store/sqlite"
)

type recordingDispatcher struct {
	requests []app.Request
	outcome  app.Outcome
}

func (d *recordingDispatcher) Dispatch(_ context.Context, request app.Request) app.Outcome {
	d.requests = append(d.requests, request)
	return d.outcome
}

func run(t *testing.T, args ...string) (int, string) {
	t.Helper()
	isolateCLIUserConfig(t)
	var output bytes.Buffer
	return Run(args, "test-version", &output), output.String()
}

func isolateCLIUserConfig(t *testing.T) {
	t.Helper()
	const marker = "OMG_CLI_TEST_CONFIG_ISOLATED"
	if os.Getenv(marker) != "" {
		return
	}
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	t.Setenv("APPDATA", filepath.Join(root, "AppData", "Roaming"))
	t.Setenv(marker, "1")
}

func TestHelpTextAndJSON(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}} {
		exit, output := run(t, args...)
		if exit != ExitSuccess || !strings.Contains(output, "Usage:") || !strings.Contains(output, "omg <command>") || !strings.Contains(output, "test-version") {
			t.Fatalf("help %v exit=%d output=%q", args, exit, output)
		}
	}

	exit, output := run(t, "--help", "--json")
	if exit != ExitSuccess {
		t.Fatalf("JSON help exit=%d output=%q", exit, output)
	}
	var result struct {
		Version string `json:"version"`
		Usage   string `json:"usage"`
	}
	decodeData(t, output, &result)
	if result.Version != "test-version" || !strings.Contains(result.Usage, "Usage:") || !strings.Contains(result.Usage, "docs/COMMAND_REFERENCE.md") {
		t.Fatalf("JSON help result=%+v", result)
	}
}

func TestEmptyInvocationShowsConciseDiscoveryWithoutDispatch(t *testing.T) {
	dispatcher := &recordingDispatcher{}
	service := bootstrap.CLIService(bootstrap.Foundation())
	service.Dispatcher = dispatcher
	var output bytes.Buffer
	exit := RunWithApplication(
		context.Background(),
		nil,
		"test-version",
		strings.NewReader(""),
		&output,
		io.Discard,
		service,
	)
	if exit != ExitSuccess {
		t.Fatalf("empty invocation exit=%d output=%q", exit, output.String())
	}
	got := output.String()
	for _, want := range []string{
		"OMG  OPERATOR LEDGER", "Usage:", "WORKFLOWS", "First run", "Start work", "Share state", "Recover safely",
		"COMMAND FAMILIES", "START + VERIFY", "COORDINATE WORK", "INSPECT + INTEGRATE",
		"COMMON OPTIONS", "Short terminal view", "omg <command> --help",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("empty invocation missing %q:\n%s", want, got)
		}
	}
	for _, forbidden := range []string{"✘ OMG  ERROR", "a command is required", "GLOBAL OPTIONS", "Create local canonical state."} {
		if strings.Contains(got, forbidden) {
			t.Errorf("empty invocation contains expanded/error text %q", forbidden)
		}
	}
	if len(dispatcher.requests) != 0 {
		t.Fatalf("empty invocation dispatched application requests: %+v", dispatcher.requests)
	}
	if lines := strings.Count(got, "\n"); lines > 32 {
		t.Fatalf("empty discovery remains too tall: %d lines\n%s", lines, got)
	}
}

func TestBareParentCommandsOpenContextualHelpWithoutDispatch(t *testing.T) {
	for _, command := range []string{
		"release", "migration", "backup", "board", "integration", "shell-init", "completion",
		"human", "session", "delegate", "task", "progress", "dependency", "message",
		"handoff", "reserve", "git", "import", "mcp", "receipt",
	} {
		t.Run(command, func(t *testing.T) {
			dispatcher := &recordingDispatcher{}
			service := bootstrap.CLIService(bootstrap.Foundation())
			service.Dispatcher = dispatcher
			var output bytes.Buffer
			exit := RunWithApplication(context.Background(), []string{command}, "test-version", strings.NewReader(""), &output, io.Discard, service)
			if exit != ExitSuccess {
				t.Fatalf("bare %s exit=%d output=%q", command, exit, output.String())
			}
			got := output.String()
			for _, want := range []string{"Usage:", "SUBCOMMANDS", "omg " + command} {
				if !strings.Contains(got, want) {
					t.Errorf("bare %s help missing %q: %s", command, want, got)
				}
			}
			if strings.Contains(got, "✘ OMG  ERROR") || strings.Contains(got, "subcommand is required") {
				t.Errorf("bare %s still renders an error: %s", command, got)
			}
			if len(dispatcher.requests) != 0 {
				t.Fatalf("bare %s dispatched requests: %+v", command, dispatcher.requests)
			}
		})
	}
}

func TestParentCommandWithAdditionalIntentStillUsesValidation(t *testing.T) {
	for _, args := range [][]string{{"task", "--json"}, {"task", "--project", "project"}, {"task", "unknown"}} {
		exit, output := run(t, args...)
		if exit == ExitSuccess || strings.Contains(output, "OMG / TASK") && !strings.Contains(output, "ERROR") {
			t.Fatalf("parent command with additional intent bypassed validation: %v exit=%d output=%q", args, exit, output)
		}
	}
}

func TestDecodeStillRejectsMissingCommandForInternalCallers(t *testing.T) {
	request, err := Decode(nil)
	if !reflect.DeepEqual(request, Request{}) || err.Code == "" || err.Message != "a command is required" {
		t.Fatalf("Decode(nil) = (%+v, %+v)", request, err)
	}
}

func TestHelpRecognitionRespectsOptionGrammar(t *testing.T) {
	parent := t.TempDir()
	if err := os.Mkdir(filepath.Join(parent, "help"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(parent)

	exit, output := run(t, "init", "--project", "help")
	if exit != ExitSuccess || strings.Contains(output, "Usage:") {
		t.Fatalf("project named help must reach init dispatch: exit=%d output=%q", exit, output)
	}

	service := bootstrap.CLIService(bootstrap.Foundation())
	var got app.CLIRuntimeRequest
	service.Runtime = func(_ context.Context, request app.CLIRuntimeRequest) (app.CLIRuntimeResult, error) {
		got = request
		return app.CLIRuntimeResult{Runtime: request.Runtime, Status: "succeeded"}, nil
	}
	var runtimeOutput bytes.Buffer
	exit = RunWithApplication(
		context.Background(),
		[]string{"run", "--runtime", "help", "--", "child", "--help"},
		"test-version",
		strings.NewReader(""),
		&runtimeOutput,
		io.Discard,
		service,
	)
	if exit != ExitSuccess || got.Runtime != "help" || !reflect.DeepEqual(got.Argv, []string{"child", "--help"}) {
		t.Fatalf("runtime help value must reach runtime dispatch: exit=%d request=%+v output=%q", exit, got, runtimeOutput.String())
	}

	for _, args := range [][]string{{"init", "help"}, {"run", "--help"}} {
		exit, output = run(t, args...)
		if exit != ExitSuccess || !strings.Contains(output, "Usage:") {
			t.Fatalf("actual help %v exit=%d output=%q", args, exit, output)
		}
	}
}

func TestHelpRecognitionMatchesDecodeOptionSemantics(t *testing.T) {
	for _, args := range [][]string{
		{"init", "--project=help"},
		{"run", "--runtime=help"},
		{"init", "--project"},
		{"init", "--project", "--help"},
		{"run", "--runtime", "help", "--", "child", "--help"},
	} {
		if helpRequested(args) {
			t.Fatalf("help recognition treated an option value or child argument as help: %v", args)
		}
	}
}

func TestPlainCLIUsesOperatorHierarchy(t *testing.T) {
	exit, help := run(t, "--help")
	if exit != ExitSuccess {
		t.Fatalf("help exit=%d output=%q", exit, help)
	}
	for _, want := range []string{"OMG  OPERATOR LEDGER", "START + VERIFY", "COORDINATE WORK", "INSPECT + INTEGRATE", "Usage:", "❯ board"} {
		if !strings.Contains(help, want) {
			t.Errorf("help missing operator hierarchy %q:\n%s", want, help)
		}
	}

	var success bytes.Buffer
	if exit := writeSuccess(&success, false, terminalPresentationResult{Message: "ready"}); exit != ExitSuccess {
		t.Fatalf("success exit=%d", exit)
	}
	if !strings.Contains(success.String(), "✔ OMG  VERIFIED") || !strings.Contains(success.String(), "ready") {
		t.Errorf("plain success lacks hierarchy: %q", success.String())
	}

	var failure bytes.Buffer
	err := domain.NewError(domain.CodeConflict, "resource conflicts", true)
	if exit := writeError(&failure, false, err); exit != ExitConflict {
		t.Fatalf("error exit=%d", exit)
	}
	for _, want := range []string{"✘ OMG  ERROR", "conflict", "resource conflicts", "retryable", "next", "omg board all"} {
		if !strings.Contains(failure.String(), want) {
			t.Errorf("plain error missing %q: %q", want, failure.String())
		}
	}
}

func decodeData(t *testing.T, text string, value any) {
	t.Helper()
	var envelope struct {
		OK   bool            `json:"ok"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(text), &envelope); err != nil || !envelope.OK {
		t.Fatalf("success envelope = %q: %v", text, err)
	}
	if err := json.Unmarshal(envelope.Data, value); err != nil {
		t.Fatal(err)
	}
}
func decodeEnvelope(t *testing.T, output string, wantOK bool) map[string]json.RawMessage {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(output))
	var envelope map[string]json.RawMessage
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("decode envelope: %v; output=%q", err, output)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("JSON output has stray stdout: %q", output)
	}
	if len(envelope) != 4 {
		t.Fatalf("envelope fields = %v", envelope)
	}
	var ok bool
	if err := json.Unmarshal(envelope["ok"], &ok); err != nil || ok != wantOK {
		t.Fatalf("envelope ok = %q: %v", envelope["ok"], err)
	}
	var meta Metadata
	if err := json.Unmarshal(envelope["meta"], &meta); err != nil ||
		meta.SchemaVersion != EnvelopeSchemaVersion || meta.CommandVersion != CommandSchemaVersion {
		t.Fatalf("envelope meta = %q: %+v, %v", envelope["meta"], meta, err)
	}
	var warnings []string
	if err := json.Unmarshal(envelope["warnings"], &warnings); err != nil || warnings == nil {
		t.Fatalf("envelope warnings = %q: %v", envelope["warnings"], err)
	}
	if wantOK {
		if envelope["data"] == nil || envelope["error"] != nil {
			t.Fatalf("success envelope fields = %v", envelope)
		}
	} else if envelope["error"] == nil || envelope["data"] != nil {
		t.Fatalf("error envelope fields = %v", envelope)
	}
	return envelope
}

type terminalPresentationResult struct {
	Message string `json:"message"`
}

func assertNoTerminalControls(t *testing.T, output string) {
	t.Helper()
	if !strings.HasSuffix(output, "\n") {
		t.Fatalf("plain output must end with a newline: %q", output)
	}
	for _, value := range output[:len(output)-1] {
		if value == '\n' {
			continue
		}
		if unicode.IsControl(value) {
			t.Fatalf("plain output contains terminal control %U: %q", value, output)
		}
	}
}

func TestWriteSuccessNeutralizesTerminalControlsInTypedResults(t *testing.T) {
	unsafe := "Clipboard \x1b]52;c;clipboard\x07\u009dC1\u009c"
	result := terminalPresentationResult{Message: unsafe}

	var plain bytes.Buffer
	if exit := writeSuccess(&plain, false, result); exit != ExitSuccess {
		t.Fatalf("plain exit = %d", exit)
	}
	if !strings.Contains(plain.String(), "Clipboard") || !strings.Contains(plain.String(), "clipboard") {
		t.Fatalf("plain output lost readable text: %q", plain.String())
	}
	assertNoTerminalControls(t, plain.String())

	var jsonOutput bytes.Buffer
	if exit := writeSuccess(&jsonOutput, true, result); exit != ExitSuccess {
		t.Fatalf("JSON exit = %d", exit)
	}
	if !json.Valid(jsonOutput.Bytes()) || strings.ContainsAny(jsonOutput.String(), "\x1b\x07") {
		t.Fatalf("JSON output is not safely escaped: %q", jsonOutput.String())
	}
	var envelope struct {
		Data terminalPresentationResult `json:"data"`
	}
	if err := json.Unmarshal(jsonOutput.Bytes(), &envelope); err != nil || envelope.Data.Message != unsafe {
		t.Fatalf("JSON output = %q: %+v, %v", jsonOutput.String(), envelope, err)
	}
}

func TestJSONEnvelopesHaveStableProtocolShape(t *testing.T) {
	exit, output := run(t, "version", "--json")
	if exit != ExitSuccess {
		t.Fatalf("version exit = %d", exit)
	}
	envelope := decodeEnvelope(t, output, true)
	var version struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(envelope["data"], &version); err != nil || version.Version != "test-version" {
		t.Fatalf("success data = %q: %+v, %v", envelope["data"], version, err)
	}

	cases := []struct {
		name          string
		invoke        func() (int, string)
		wantExit      int
		wantCode      domain.ErrorCode
		wantRetryable bool
	}{
		{
			name:     "invalid argument",
			invoke:   func() (int, string) { return run(t, "board", "--json") },
			wantExit: ExitUsage,
			wantCode: domain.CodeInvalidArgument,
		},
		{
			name:     "unavailable",
			invoke:   func() (int, string) { return run(t, "board", "all", "--project", t.TempDir(), "--json") },
			wantExit: ExitUnavailable,
			wantCode: domain.CodeUninitialized,
		},
		{
			name: "conflict",
			invoke: func() (int, string) {
				var conflict bytes.Buffer
				exit := writeError(&conflict, true, domain.NewError(domain.CodeConflict, "resource conflicts", true))
				return exit, conflict.String()
			},
			wantExit:      ExitConflict,
			wantCode:      domain.CodeConflict,
			wantRetryable: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exit, output := tc.invoke()
			if exit != tc.wantExit {
				t.Fatalf("exit = %d; output=%q", exit, output)
			}
			envelope := decodeEnvelope(t, output, false)
			var errorData ErrorMetadata
			if err := json.Unmarshal(envelope["error"], &errorData); err != nil {
				t.Fatal(err)
			}
			if errorData.Code != string(tc.wantCode) || errorData.Retryable != tc.wantRetryable || errorData.ExitCode != tc.wantExit || errorData.Message == "" {
				t.Fatalf("error metadata = %+v", errorData)
			}
		})
	}
}

func TestRunVersionJSONUsesStableEnvelope(t *testing.T) {
	exit, output := run(t, "version", "--json")
	if exit != ExitSuccess {
		t.Fatalf("exit = %d", exit)
	}
	var data struct {
		Version string `json:"version"`
	}
	decodeData(t, output, &data)
	if data.Version != "test-version" {
		t.Fatalf("version = %q", data.Version)
	}
}
func TestInitPreservesExistingConfigAndNeverMigrates(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, ".omg", "project.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	original := "[project]\nname = \"kept\"\n"
	if err := os.WriteFile(configPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		exit, _ := run(t, "init", "--project", root)
		if exit != ExitSuccess {
			t.Fatalf("init exit = %d", exit)
		}
	}
	got, err := os.ReadFile(configPath)
	if err != nil || string(got) != original {
		t.Fatalf("config = %q, %v", got, err)
	}
	exit, output := run(t, "preflight", "--project", root, "--json")
	if exit != ExitSuccess {
		t.Fatalf("preflight exit=%d: %s", exit, output)
	}
	var status struct {
		Pending int `json:"pending_migrations"`
	}
	decodeData(t, output, &status)
	if status.Pending == 0 {
		t.Fatal("init applied migrations")
	}
	gitRoot := t.TempDir()
	if command := exec.Command("git", "init", gitRoot); command.Run() != nil {
		t.Fatal("git init failed")
	}
	if exit, _ := run(t, "init", "--project", gitRoot); exit != ExitSuccess {
		t.Fatalf("git init command exit=%d", exit)
	}
	exit, output = run(t, "preflight", "--project", gitRoot, "--json")
	if exit != ExitSuccess {
		t.Fatalf("git preflight=%d: %s", exit, output)
	}
	decodeData(t, output, &status)
	if status.Pending == 0 {
		t.Fatal("git init applied migrations")
	}
}
func TestFoundationMigrationFlowAndApprovalGuards(t *testing.T) {
	root := migrationOutputProject(t)
	outputDir := migrationOutputDirectory(t, root)
	planPath := filepath.Join(outputDir, "plan.json")
	exit, output := run(t, "migration", "plan", "--project", root, "--output", planPath, "--json")
	if exit != ExitSuccess {
		t.Fatalf("plan: %d %s", exit, output)
	}
	planData, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	plan, planErr := foundation.ReadPlan(planData)
	if planErr.Code != "" {
		t.Fatal(planErr)
	}
	exit, output = run(t, "doctor", "--project", root, "--integrity", "--json")
	if exit != ExitSuccess {
		t.Fatalf("doctor: %d %s", exit, output)
	}
	var doctor struct {
		Pending   int   `json:"pending_migrations"`
		Integrity *bool `json:"integrity"`
	}
	decodeData(t, output, &doctor)
	if doctor.Pending == 0 || doctor.Integrity == nil || !*doctor.Integrity {
		t.Fatalf("doctor=%+v", doctor)
	}
	approvalPath := filepath.Join(outputDir, "approval.json")
	// A missing approval and an unnamed approval both fail before schema mutation.
	exit, _ = run(t, "migration", "apply", "--project", root, "--plan-file", planPath, "--approval-file", approvalPath)
	if exit != ExitUsage {
		t.Fatalf("missing approval exit=%d", exit)
	}
	if err := os.WriteFile(approvalPath, []byte(`{"approved_by":"","timestamp":"2026-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	exit, _ = run(t, "migration", "apply", "--project", root, "--plan-file", planPath, "--approval-file", approvalPath)
	if exit != ExitUsage {
		t.Fatalf("unnamed approval exit=%d", exit)
	}
	mismatch := foundation.ApprovalFile{ApprovalID: "approval-mismatch", ApprovedBy: "Ada", EvidenceReference: "ticket-1", PlanID: plan.ID, Project: plan.Project, FromVersion: plan.FromVersion, ToVersion: plan.ToVersion, Checksums: plan.Checksums, BackupLocation: plan.BackupLocation, BackupChecksum: "wrong", Command: "omg migration apply", Timestamp: time.Now().UTC().Format(time.RFC3339Nano), ExpiresAtRaw: time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339Nano)}
	mismatchData, _ := json.Marshal(mismatch)
	if err := os.WriteFile(approvalPath, mismatchData, 0o600); err != nil {
		t.Fatal(err)
	}
	exit, _ = run(t, "migration", "apply", "--project", root, "--plan-file", planPath, "--approval-file", approvalPath)
	if exit != ExitUsage {
		t.Fatalf("mismatched approval exit=%d", exit)
	}
	exit, output = run(t, "preflight", "--project", root, "--json")
	if exit != ExitSuccess {
		t.Fatalf("preflight: %s", output)
	}
	var before struct {
		Pending int `json:"pending_migrations"`
	}
	decodeData(t, output, &before)
	if before.Pending == 0 {
		t.Fatal("bad approval mutated schema")
	}
	exit, output = run(t, "backup", "create", "--project", root, "--plan-file", planPath, "--json")
	if exit != ExitSuccess {
		t.Fatalf("backup: %d %s", exit, output)
	}
	var backup struct {
		Checksum string `json:"checksum"`
	}
	decodeData(t, output, &backup)
	if backup.Checksum == "" {
		t.Fatal("empty backup checksum")
	}
	approval := foundation.ApprovalFile{ApprovalID: "approval-apply", ApprovedBy: "Ada", EvidenceReference: "ticket-1", PlanID: plan.ID, Project: plan.Project, FromVersion: plan.FromVersion, ToVersion: plan.ToVersion, Checksums: plan.Checksums, BackupLocation: plan.BackupLocation, BackupChecksum: backup.Checksum, Command: "omg migration apply", Timestamp: time.Now().UTC().Format(time.RFC3339Nano), ExpiresAtRaw: time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339Nano)}
	data, err := json.Marshal(approval)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(approvalPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	exit, output = run(t, "migration", "apply", "--project", root, "--plan-file", planPath, "--approval-file", approvalPath, "--json")
	if exit != ExitSuccess {
		t.Fatalf("apply: %d %s", exit, output)
	}
	exit, output = run(t, "preflight", "--project", root, "--json")
	if exit != ExitSuccess {
		t.Fatalf("postapply: %s", output)
	}
	decodeData(t, output, &before)
	if before.Pending != 0 {
		t.Fatalf("pending=%d", before.Pending)
	}
}
func TestApprovalTimestampAndJSONErrorsAreSafe(t *testing.T) {
	root := migrationOutputProject(t)
	outputDir := migrationOutputDirectory(t, root)
	planPath := filepath.Join(outputDir, "plan.json")
	if exit, _ := run(t, "migration", "plan", "--project", root, "--output", planPath); exit != ExitSuccess {
		t.Fatal(exit)
	}
	planData, _ := os.ReadFile(planPath)
	plan, _ := foundation.ReadPlan(planData)
	approval := foundation.ApprovalFile{ApprovedBy: "Ada", EvidenceReference: "e", PlanID: plan.ID, Project: plan.Project, FromVersion: plan.FromVersion, ToVersion: plan.ToVersion, Checksums: plan.Checksums, BackupLocation: plan.BackupLocation, BackupChecksum: "x", Command: "omg migration apply", Timestamp: "2026-01-01T00:00:00+01:00"}
	data, _ := json.Marshal(approval)
	approvalPath := filepath.Join(root, "approval.json")
	if err := os.WriteFile(approvalPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	exit, output := run(t, "migration", "apply", "--project", root, "--plan-file", planPath, "--approval-file", approvalPath, "--json")
	if exit != ExitUsage {
		t.Fatalf("exit=%d %s", exit, output)
	}
	if strings.Contains(output, root) || strings.Contains(output, "approval.json") {
		t.Fatalf("private path leaked: %s", output)
	}
}
func TestReleaseStatusAndBoardPreconditions(t *testing.T) {
	exit, output := run(t, "release", "status", "--json")
	if exit != ExitSuccess {
		t.Fatal(exit)
	}
	var data struct {
		Status string `json:"status"`
	}
	decodeData(t, output, &data)
	if data.Status != "NOT PUBLISHED" {
		t.Fatal(data.Status)
	}
	exit, output = run(t, "board", "--json")
	if exit != ExitUsage || !strings.Contains(output, "invalid_argument") {
		t.Fatalf("missing mode: %d %s", exit, output)
	}
	exit, output = run(t, "board", "all", "--project", t.TempDir(), "--json")
	if exit != ExitUnavailable || !strings.Contains(output, "uninitialized") {
		t.Fatalf("uninitialized board: %d %s", exit, output)
	}
}

func TestPreflightReportsUninitializedProjection(t *testing.T) {
	exit, output := run(t, "preflight", "--project", t.TempDir(), "--json")
	if exit != ExitSuccess {
		t.Fatalf("preflight exit=%d output=%s", exit, output)
	}
	var preflight app.PreflightView
	decodeData(t, output, &preflight)
	if preflight.Initialized || preflight.PendingMigrations != 0 || preflight.Sessions == nil || preflight.Tasks == nil || preflight.Inbox == nil || preflight.Dependencies == nil || preflight.Reservations == nil {
		t.Fatalf("uninitialized preflight=%+v", preflight)
	}
	exit, output = run(t, "preflight", "--project", t.TempDir())
	if exit != ExitSuccess {
		t.Fatalf("TTY preflight exit=%d output=%s", exit, output)
	}
	for _, want := range []string{"OMG  OPERATOR LEDGER / PREFLIGHT", "STATE", "Initialized: false", "schema_state=uninitialized", "IDENTITY", "SESSIONS + TASKS", "INBOX", "DEPENDENCIES", "RESERVATIONS", "GIT"} {
		if !strings.Contains(output, want) {
			t.Errorf("TTY preflight output missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "{Initialized:") {
		t.Fatalf("TTY preflight output must not dump a Go struct: %s", output)
	}
}

func TestBoardAndStaticExportUseCurrentCanonicalStore(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	root := t.TempDir()
	dataRoot := t.TempDir()
	resolver := platform.NewResolver(platform.Dependencies{
		Git: func(context.Context, string, ...string) (string, error) {
			return "", errors.New("not a Git repository")
		},
		UserConfigDir: func() (string, error) { return dataRoot, nil },
	})
	service := foundation.New(foundation.Dependencies{
		Resolver:          resolver,
		ConfigInitializer: platform.NewProjectConfigInitializer(),
		Open: func(ctx context.Context, path string, options ports.OpenOptions) (ports.FoundationStore, ports.OpenStatus, error) {
			store, status, err := sqlite.Open(ctx, path, options)
			if err != nil {
				return nil, ports.OpenStatus{}, err
			}
			return store, status, nil
		},
		InspectBackup: func(ctx context.Context, path, checksum string) (ports.BackupInspection, error) {
			inspection, err := sqlite.InspectBackup(ctx, path, checksum)
			if err != nil {
				return ports.BackupInspection{}, err
			}
			return ports.BackupInspection{Checksum: inspection.Checksum, SchemaVersion: inspection.SchemaVersion, Integrity: inspection.Integrity, Compatible: inspection.Compatible}, nil
		},
	})
	selection := foundation.Selection{Project: root}
	if _, err := service.Init(ctx, selection); err.Code != "" {
		t.Fatal(err)
	}
	plan, err := service.Plan(ctx, selection)
	if err.Code != "" {
		t.Fatal(err)
	}
	backup, err := service.Backup(ctx, selection, &plan)
	if err.Code != "" {
		t.Fatal(err)
	}
	approval := foundation.ApprovalFile{
		ApprovalID:        "cli-board-approval",
		ApprovedBy:        "test",
		EvidenceReference: "cli-board-test",
		PlanID:            plan.ID,
		Project:           plan.Project,
		FromVersion:       plan.FromVersion,
		ToVersion:         plan.ToVersion,
		Checksums:         plan.Checksums,
		BackupLocation:    plan.BackupLocation,
		BackupChecksum:    backup.Checksum,
		Command:           "omg migration apply",
		Timestamp:         now.Format(time.RFC3339Nano),
		ExpiresAtRaw:      now.Add(5 * time.Minute).Format(time.RFC3339Nano),
	}
	if applyErr := service.Apply(ctx, selection, plan, approval); applyErr.Code != "" {
		t.Fatal(applyErr)
	}
	openErr := service.WithCurrentStore(ctx, selection, func(resolved ports.ResolvedStore, store ports.Store) error {
		_, _, writeErr := store.Write(ctx, "seed-board-cli", "test.write", func(repositories ports.Repositories) (domain.Result, error) {
			coordination := repositories.Coordination()
			human := lineage.Human{ID: "human-cli", DisplayName: "Operator", Confidence: lineage.ConfidenceVerified, CreatedAt: now}
			if err := coordination.CreateHuman(ctx, human); err != nil {
				return domain.Result{}, err
			}
			session := lineage.AgentSession{ID: "session-cli", ProjectID: lineage.ID(resolved.Project), HumanID: human.ID, Kind: lineage.HumanDirect, Runtime: "test", Role: "owner", Source: lineage.SourceHuman, SourceRef: "cli", RootID: "session-cli", StartedAt: now, NativeAccessState: lineage.NativeAccessUnsupported}
			if err := coordination.CreateSession(ctx, session); err != nil {
				return domain.Result{}, err
			}
			task, err := coordination.CreateTask(ctx, lineage.Task{ID: "task-cli", ProjectID: lineage.ID(resolved.Project), DisplayNumber: 1, Title: "Render canonical board", State: lineage.TaskReady, CreatedBySessionID: session.ID, CreatedAt: now, UpdatedAt: now})
			if err != nil {
				return domain.Result{}, err
			}
			if err := coordination.CreateRun(ctx, lineage.TaskRun{ID: "run-cli", TaskID: task.ID, SessionID: session.ID, State: lineage.RunWorkComplete, StartedAt: now}); err != nil {
				return domain.Result{}, err
			}
			return domain.Result{ID: "seed-board-cli", Outcome: domain.OutcomeOK}, nil
		})
		return writeErr
	})
	if openErr.Code != "" {
		t.Fatal(openErr)
	}

	var output bytes.Buffer
	exit := RunWithService([]string{"board", "all", "--project", root, "--json"}, "test-version", &output, service)
	if exit != ExitSuccess {
		t.Fatalf("board exit=%d: %s", exit, output.String())
	}
	var model struct {
		Kind string              `json:"kind"`
		Data query.BoardSnapshot `json:"data"`
	}
	decodeData(t, output.String(), &model)
	if model.Kind != "board" || len(model.Data.Sessions) != 1 || model.Data.Sessions[0].ID != "session-cli" || len(model.Data.Tasks) != 1 || model.Data.Tasks[0].Title != "Render canonical board" {
		t.Fatalf("board model = %+v", model)
	}
	output.Reset()
	exit = RunWithService([]string{"preflight", "--project", root, "--json"}, "test-version", &output, service)
	if exit != ExitSuccess {
		t.Fatalf("preflight without session exit=%d: %s", exit, output.String())
	}
	var unselected app.PreflightView
	decodeData(t, output.String(), &unselected)
	if unselected.Identity != nil || len(unselected.Sessions) != 1 || unselected.Sessions[0].ID != "session-cli" {
		t.Fatalf("unselected preflight identities = %#v", unselected)
	}
	output.Reset()
	exit = RunWithService([]string{"preflight", "--project", root, "--session", "session-cli", "--json"}, "test-version", &output, service)
	if exit != ExitSuccess {
		t.Fatalf("preflight with session exit=%d: %s", exit, output.String())
	}
	var preflight app.PreflightView
	decodeData(t, output.String(), &preflight)
	if preflight.Identity == nil || preflight.Identity.ID != "session-cli" {
		t.Fatalf("preflight identity = %#v", preflight.Identity)
	}

	taskPayload := `{"title":"MCP parity task","created_by_session_id":"session-cli"}`
	output.Reset()
	exit = RunWithService([]string{"task", "create", "--project", root, "--idempotency-key", "task-create-cli", "--payload", taskPayload, "--json"}, "test-version", &output, service)
	if exit != ExitSuccess {
		t.Fatalf("task create exit=%d: %s", exit, output.String())
	}
	var taskResult struct {
		ID string `json:"id"`
	}
	decodeData(t, output.String(), &taskResult)
	firstTaskOutput := output.String()
	output.Reset()
	exit = RunWithService([]string{"task", "create", "--project", root, "--idempotency-key", "task-create-cli", "--payload", taskPayload, "--json"}, "test-version", &output, service)
	if exit != ExitSuccess || output.String() != firstTaskOutput || taskResult.ID == "" {
		t.Fatalf("task replay exit=%d: %s", exit, output.String())
	}

	messagePayload := `{"id":"message-cli","type":"NOTICE","thread_id":"thread-cli","sender_session_id":"session-cli","recipients":[{"session_id":"session-cli"}],"subject":"Parity","body":"inert body","related_task_id":"` + taskResult.ID + `"}`
	output.Reset()
	exit = RunWithService([]string{"message", "send", "--project", root, "--idempotency-key", "message-send-cli", "--payload", messagePayload, "--json"}, "test-version", &output, service)
	if exit != ExitSuccess || !strings.Contains(output.String(), `"id":"message-cli"`) {
		t.Fatalf("message send exit=%d: %s", exit, output.String())
	}

	handoffPayload := `{"id":"handoff-cli","task_id":"task-cli","run_id":"run-cli","source_session_id":"session-cli","summary":"Completed safely","final_output_policy":"none","changed_files":["/private/cli-release.go"],"commits":["password=not-for-output"],"verification_evidence":[{"summary":"/private/verification","hash":"sha256:evidence"}],"remaining_risks":["password=not-for-output"],"suggested_actions":["/private/review"]}`
	output.Reset()
	exit = RunWithService([]string{"handoff", "create", "--project", root, "--idempotency-key", "handoff-create-cli", "--payload", handoffPayload, "--json"}, "test-version", &output, service)
	if exit != ExitSuccess || !strings.Contains(output.String(), `"status":"submitted"`) {
		t.Fatalf("handoff create exit=%d: %s", exit, output.String())
	}
	output.Reset()
	exit = RunWithService([]string{"handoff", "show", "--project", root, "--payload", `{"handoff_id":"handoff-cli"}`, "--json"}, "test-version", &output, service)
	if exit != ExitSuccess || !strings.Contains(output.String(), `"run_state":"WORK_COMPLETE"`) || !strings.Contains(output.String(), `"changed_files"`) || strings.Contains(output.String(), "/private/") || strings.Contains(output.String(), "password=not-for-output") {
		t.Fatalf("handoff show did not safely expose evidence: exit=%d output=%s", exit, output.String())
	}
	output.Reset()
	exit = RunWithService([]string{"handoff", "history", "--project", root, "--payload", `{"task_id":"task-cli"}`, "--json"}, "test-version", &output, service)
	if exit != ExitSuccess || !strings.Contains(output.String(), `"handoff-cli"`) || !strings.Contains(output.String(), `"verification_evidence"`) || strings.Contains(output.String(), "/private/") || strings.Contains(output.String(), "password=not-for-output") {
		t.Fatalf("handoff history did not safely preserve evidence: exit=%d output=%s", exit, output.String())
	}

	mcpCall := func(id, command, key, payload string) string {
		var typedPayload any
		if err := json.Unmarshal([]byte(payload), &typedPayload); err != nil {
			t.Fatal(err)
		}
		frame, marshalErr := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"method":  "tools/call",
			"params": map[string]any{
				"name": "omg",
				"arguments": map[string]any{"request": map[string]any{
					"version": 1, "command": command, "project": root, "idempotency_key": key, "payload": typedPayload,
				}},
			},
		})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		return string(frame) + "\n"
	}
	mcpInput := mcpCall("task", "task.create", "task-create-cli", taskPayload) +
		mcpCall("message", "message.send", "message-send-cli", messagePayload) +
		mcpCall("handoff", "handoff.create", "handoff-create-cli", handoffPayload) +
		mcpCall("handoff-show", "handoff.show", "", `{"handoff_id":"handoff-cli"}`) +
		mcpCall("handoff-history", "handoff.history", "", `{"task_id":"task-cli"}`)
	var mcpOutput bytes.Buffer
	exit = runWithContext(context.Background(), []string{"mcp", "serve", "--stdio"}, "test-version", &mcpOutput, bootstrap.CLIService(service), strings.NewReader(mcpInput), io.Discard)
	mcpLines := strings.Split(strings.TrimSpace(mcpOutput.String()), "\n")
	if exit != ExitSuccess || len(mcpLines) != 5 || strings.Contains(mcpOutput.String(), `"isError":true`) || !strings.Contains(mcpOutput.String(), `"run_state":"WORK_COMPLETE"`) || !strings.Contains(mcpOutput.String(), `"verification_evidence"`) || strings.Contains(mcpOutput.String(), "/private/") || strings.Contains(mcpOutput.String(), "password=not-for-output") {
		t.Fatalf("MCP handoff parity exit=%d responses=%q", exit, mcpLines)
	}
	output.Reset()
	exit = RunWithService([]string{"board", "all", "--project", root, "--json"}, "test-version", &output, service)
	if exit != ExitSuccess {
		t.Fatalf("post-MCP board exit=%d: %s", exit, output.String())
	}
	decodeData(t, output.String(), &model)
	if len(model.Data.Tasks) != 2 || len(model.Data.Inbox) != 1 || len(model.Data.Handoffs) != 1 || model.Data.Handoffs[0].RunState != string(lineage.RunWorkComplete) {
		t.Fatalf("MCP replay changed canonical state: tasks=%d inbox=%d handoffs=%d handoff=%#v", len(model.Data.Tasks), len(model.Data.Inbox), len(model.Data.Handoffs), model.Data.Handoffs)
	}

	exportDirectory, pathErr := filepath.EvalSymlinks(t.TempDir())
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	htmlPath := filepath.Join(exportDirectory, "board.html")
	output.Reset()
	exit = RunWithService([]string{"export", "html", "--project", root, "--output", htmlPath}, "test-version", &output, service)
	if exit != ExitSuccess {
		t.Fatalf("export exit=%d: %s", exit, output.String())
	}
	html, readErr := os.ReadFile(htmlPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Contains(html, []byte("session-cli")) || !bytes.Contains(html, []byte("Content-Security-Policy")) {
		t.Fatal("invalid static export")
	}
	assertPrivateExportFile(t, htmlPath)
	before := append([]byte(nil), html...)
	output.Reset()
	if exit = RunWithService([]string{"export", "html", "--project", root, "--output", htmlPath}, "test-version", &output, service); exit != ExitUnavailable {
		t.Fatalf("existing export exit=%d: %s", exit, output.String())
	}
	after, _ := os.ReadFile(htmlPath)
	if !bytes.Equal(before, after) {
		t.Fatal("failed export altered existing output")
	}
	output.Reset()
	exit = runWithContext(context.Background(), []string{"watch", "status", "--project", root, "--json"}, "test-version", &output, bootstrap.CLIService(service), strings.NewReader(""), io.Discard)
	if exit != ExitSuccess || !strings.Contains(output.String(), `"code":"stopped"`) {
		t.Fatalf("watch status exit=%d: %s", exit, output.String())
	}
	watchContext, cancelWatch := context.WithCancel(context.Background())
	// The watch assertion covers cooperative cancellation, not foundation startup
	// latency. Race instrumentation can legitimately push store opening beyond
	// 100 ms, which made this test report an unrelated unavailable error.
	timer := time.AfterFunc(2*time.Second, cancelWatch)
	output.Reset()
	exit = runWithContext(watchContext, []string{"watch", "--project", root, "--json"}, "test-version", &output, bootstrap.CLIService(service), strings.NewReader(""), io.Discard)
	timer.Stop()
	cancelWatch()
	if exit != ExitSuccess || !strings.Contains(output.String(), `"code":"stopped"`) {
		t.Fatalf("watch run exit=%d: %s", exit, output.String())
	}
}

func TestCLIAndMCPFreshRequestsHaveEquivalentCanonicalState(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	dataRoot := t.TempDir()
	resolver := platform.NewResolver(platform.Dependencies{
		Git: func(context.Context, string, ...string) (string, error) {
			return "", errors.New("not a Git repository")
		},
		UserConfigDir: func() (string, error) { return dataRoot, nil },
	})
	service := foundation.New(foundation.Dependencies{
		Resolver:          resolver,
		ConfigInitializer: platform.NewProjectConfigInitializer(),
		Open: func(ctx context.Context, path string, options ports.OpenOptions) (ports.FoundationStore, ports.OpenStatus, error) {
			store, status, err := sqlite.Open(ctx, path, options)
			return store, status, err
		},
		InspectBackup: func(ctx context.Context, path, checksum string) (ports.BackupInspection, error) {
			inspection, err := sqlite.InspectBackup(ctx, path, checksum)
			return ports.BackupInspection{Checksum: inspection.Checksum, SchemaVersion: inspection.SchemaVersion, Integrity: inspection.Integrity, Compatible: inspection.Compatible}, err
		},
	})
	seed := func(root string) {
		t.Helper()
		selection := foundation.Selection{Project: root}
		if _, err := service.Init(ctx, selection); err.Code != "" {
			t.Fatal(err)
		}
		plan, err := service.Plan(ctx, selection)
		if err.Code != "" {
			t.Fatal(err)
		}
		backup, err := service.Backup(ctx, selection, &plan)
		if err.Code != "" {
			t.Fatal(err)
		}
		approval := foundation.ApprovalFile{ApprovalID: "fresh-parity-" + filepath.Base(root), ApprovedBy: "test", EvidenceReference: "fresh-parity", PlanID: plan.ID, Project: plan.Project, FromVersion: plan.FromVersion, ToVersion: plan.ToVersion, Checksums: plan.Checksums, BackupLocation: plan.BackupLocation, BackupChecksum: backup.Checksum, Command: "omg migration apply", Timestamp: now.Format(time.RFC3339Nano), ExpiresAtRaw: now.Add(5 * time.Minute).Format(time.RFC3339Nano)}
		if err := service.Apply(ctx, selection, plan, approval); err.Code != "" {
			t.Fatal(err)
		}
		if err := service.WithCurrentStore(ctx, selection, func(resolved ports.ResolvedStore, store ports.Store) error {
			_, _, err := store.Write(ctx, "fresh-parity-seed", "test.write", func(repositories ports.Repositories) (domain.Result, error) {
				coordination := repositories.Coordination()
				human := lineage.Human{ID: "fresh-human", DisplayName: "Operator", Confidence: lineage.ConfidenceVerified, CreatedAt: now}
				if err := coordination.CreateHuman(ctx, human); err != nil {
					return domain.Result{}, err
				}
				session := lineage.AgentSession{ID: "fresh-session", ProjectID: lineage.ID(resolved.Project), HumanID: human.ID, Kind: lineage.HumanDirect, Runtime: "test", Role: "owner", Source: lineage.SourceHuman, SourceRef: "fresh-parity", RootID: "fresh-session", StartedAt: now, NativeAccessState: lineage.NativeAccessUnsupported}
				if err := coordination.CreateSession(ctx, session); err != nil {
					return domain.Result{}, err
				}
				task, err := coordination.CreateTask(ctx, lineage.Task{ID: "fresh-base-task", ProjectID: lineage.ID(resolved.Project), DisplayNumber: 1, Title: "Base task", State: lineage.TaskReady, CreatedBySessionID: session.ID, CreatedAt: now, UpdatedAt: now})
				if err != nil {
					return domain.Result{}, err
				}
				if err := coordination.CreateRun(ctx, lineage.TaskRun{ID: "fresh-base-run", TaskID: task.ID, SessionID: session.ID, State: lineage.RunWorkComplete, StartedAt: now}); err != nil {
					return domain.Result{}, err
				}
				return domain.Result{ID: "fresh-parity-seed", Outcome: domain.OutcomeOK}, nil
			})
			return err
		}); err.Code != "" {
			t.Fatal(err)
		}
	}
	withoutIDs := func(raw []byte) map[string]any {
		t.Helper()
		var value map[string]any
		if err := json.Unmarshal(raw, &value); err != nil {
			t.Fatal(err)
		}
		delete(value, "id")
		delete(value, "task_id")
		return value
	}
	type canonicalView struct {
		SchemaVersion int
		ViewVersion   int
		Mode          query.BoardMode
		Tasks         []struct {
			DisplayNumber int64
			Title, State  string
		}
		Inbox    []struct{ Type, Subject, Acknowledgement string }
		Handoffs []struct{ Summary, FinalOutputPolicy, Status string }
	}
	project := func(snapshot query.BoardSnapshot) canonicalView {
		view := canonicalView{SchemaVersion: snapshot.SchemaVersion, ViewVersion: snapshot.ViewVersion, Mode: snapshot.Mode}
		for _, task := range snapshot.Tasks {
			view.Tasks = append(view.Tasks, struct {
				DisplayNumber int64
				Title, State  string
			}{task.DisplayNumber, task.Title, task.State})
		}
		for _, item := range snapshot.Inbox {
			view.Inbox = append(view.Inbox, struct{ Type, Subject, Acknowledgement string }{item.Type, item.Subject, item.Acknowledgement})
		}
		for _, handoff := range snapshot.Handoffs {
			view.Handoffs = append(view.Handoffs, struct{ Summary, FinalOutputPolicy, Status string }{handoff.Summary, handoff.FinalOutputPolicy, handoff.Status})
		}
		return view
	}
	cliRoot, mcpRoot := t.TempDir(), t.TempDir()
	seed(cliRoot)
	seed(mcpRoot)

	runCLI := func(root string) ([]map[string]any, query.BoardSnapshot) {
		t.Helper()
		var output bytes.Buffer
		run := func(args ...string) []byte {
			output.Reset()
			if exit := RunWithService(args, "test-version", &output, service); exit != ExitSuccess {
				t.Fatalf("CLI %v: %s", args, output.String())
			}
			var envelope struct {
				Data json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			return envelope.Data
		}
		task := run("task", "create", "--project", root, "--idempotency-key", "fresh-cli-task", "--payload", `{"title":"Fresh parity task","created_by_session_id":"fresh-session"}`, "--json")
		var taskResult struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(task, &taskResult); err != nil || taskResult.ID == "" {
			t.Fatalf("CLI task=%s err=%v", task, err)
		}
		message := run("message", "send", "--project", root, "--idempotency-key", "fresh-cli-message", "--payload", `{"id":"fresh-message","type":"NOTICE","thread_id":"fresh-thread","sender_session_id":"fresh-session","recipients":[{"session_id":"fresh-session"}],"subject":"Fresh parity","body":"inert","related_task_id":"`+taskResult.ID+`"}`, "--json")
		handoff := run("handoff", "create", "--project", root, "--idempotency-key", "fresh-cli-handoff", "--payload", `{"id":"fresh-handoff","task_id":"fresh-base-task","run_id":"fresh-base-run","source_session_id":"fresh-session","summary":"Fresh parity handoff","final_output_policy":"none"}`, "--json")
		board := run("board", "all", "--project", root, "--json")
		var model struct {
			Data query.BoardSnapshot `json:"data"`
		}
		if err := json.Unmarshal(board, &model); err != nil || model.Data.SchemaVersion != query.BoardSchemaVersion || model.Data.ViewVersion != query.ViewVersion {
			t.Fatalf("CLI board=%s err=%v", board, err)
		}
		return []map[string]any{withoutIDs(task), withoutIDs(message), withoutIDs(handoff)}, model.Data
	}
	runMCP := func(root string) ([]map[string]any, query.BoardSnapshot) {
		t.Helper()
		call := func(id, command, key, payload string) []byte {
			var typed any
			if err := json.Unmarshal([]byte(payload), &typed); err != nil {
				t.Fatal(err)
			}
			frame, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": map[string]any{"name": "omg", "arguments": map[string]any{"request": map[string]any{"version": 1, "command": command, "project": root, "idempotency_key": key, "payload": typed}}}})
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			if exit := runWithContext(ctx, []string{"mcp", "serve", "--stdio"}, "test-version", &output, bootstrap.CLIService(service), strings.NewReader(string(frame)+"\n"), io.Discard); exit != ExitSuccess {
				t.Fatalf("MCP %s: %s", command, output.String())
			}
			var response struct {
				Result struct {
					StructuredContent struct {
						OK   bool            `json:"ok"`
						Data json.RawMessage `json:"data"`
					} `json:"structuredContent"`
				} `json:"result"`
			}
			if err := json.Unmarshal(output.Bytes(), &response); err != nil || !response.Result.StructuredContent.OK {
				t.Fatalf("MCP %s response=%s err=%v", command, output.String(), err)
			}
			return response.Result.StructuredContent.Data
		}
		task := call("mcp-task", "task.create", "fresh-mcp-task", `{"title":"Fresh parity task","created_by_session_id":"fresh-session"}`)
		var taskResult struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(task, &taskResult); err != nil || taskResult.ID == "" {
			t.Fatalf("MCP task=%s err=%v", task, err)
		}
		message := call("mcp-message", "message.send", "fresh-mcp-message", `{"id":"fresh-message","type":"NOTICE","thread_id":"fresh-thread","sender_session_id":"fresh-session","recipients":[{"session_id":"fresh-session"}],"subject":"Fresh parity","body":"inert","related_task_id":"`+taskResult.ID+`"}`)
		handoff := call("mcp-handoff", "handoff.create", "fresh-mcp-handoff", `{"id":"fresh-handoff","task_id":"fresh-base-task","run_id":"fresh-base-run","source_session_id":"fresh-session","summary":"Fresh parity handoff","final_output_policy":"none"}`)
		board := call("mcp-board", "board.query", "", `{"mode":"all"}`)
		var model struct {
			ViewVersion    int                 `json:"view_version"`
			Kind           string              `json:"kind"`
			SnapshotCursor string              `json:"snapshot_cursor"`
			Data           query.BoardSnapshot `json:"data"`
		}
		if err := json.Unmarshal(board, &model); err != nil || model.Kind != "board" || model.ViewVersion != query.ViewVersion || model.SnapshotCursor == "" || model.Data.SchemaVersion != query.BoardSchemaVersion || model.Data.ViewVersion != query.ViewVersion {
			t.Fatalf("MCP board=%s err=%v", board, err)
		}
		return []map[string]any{withoutIDs(task), withoutIDs(message), withoutIDs(handoff)}, model.Data
	}
	cliOutcomes, cliView := runCLI(cliRoot)
	mcpOutcomes, mcpView := runMCP(mcpRoot)
	if !reflect.DeepEqual(cliOutcomes, mcpOutcomes) || !reflect.DeepEqual(project(cliView), project(mcpView)) {
		t.Fatalf("CLI outcomes=%#v view=%#v MCP outcomes=%#v view=%#v", cliOutcomes, project(cliView), mcpOutcomes, project(mcpView))
	}
}

func TestIntegrationPlanApplyIdempotentAndRemove(t *testing.T) {
	root := t.TempDir()
	agentsPath := filepath.Join(root, "AGENTS.md")
	claudePath := filepath.Join(root, "CLAUDE.md")
	original := []byte("# Existing rules\r\n\r\nKeep this.\r\n")
	if err := os.WriteFile(agentsPath, original, 0o640); err != nil {
		t.Fatal(err)
	}
	exit, output := run(t, "integration", "plan", "--project", root, "--json")
	if exit != ExitSuccess || !strings.Contains(output, `"action":"create"`) {
		t.Fatalf("plan exit=%d: %s", exit, output)
	}
	if _, err := os.Stat(claudePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("plan mutated target: %v", err)
	}
	exit, output = run(t, "integration", "apply", "--project", root, "--json")
	if exit != ExitSuccess {
		t.Fatalf("apply exit=%d: %s", exit, output)
	}
	firstAgents, _ := os.ReadFile(agentsPath)
	firstClaude, _ := os.ReadFile(claudePath)
	exit, output = run(t, "integration", "apply", "--project", root, "--json")
	if exit != ExitSuccess || !strings.Contains(output, `"action":"none"`) {
		t.Fatalf("idempotent apply exit=%d: %s", exit, output)
	}
	secondAgents, _ := os.ReadFile(agentsPath)
	secondClaude, _ := os.ReadFile(claudePath)
	if !bytes.Equal(firstAgents, secondAgents) || !bytes.Equal(firstClaude, secondClaude) {
		t.Fatal("second apply changed instruction targets")
	}
	exit, output = run(t, "integration", "remove", "--status", "--project", root, "--json")
	if exit != ExitSuccess || !strings.Contains(output, `"status"`) {
		t.Fatalf("remove exit=%d: %s", exit, output)
	}
	restored, readErr := os.ReadFile(agentsPath)
	if readErr != nil || !bytes.Equal(restored, original) {
		t.Fatalf("existing target not restored exactly: err=%v data=%q", readErr, restored)
	}
	if _, err := os.Stat(claudePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created target remains after remove: %v", err)
	}
}

func TestRuntimeWrapperPreservesChildHelpArgv(t *testing.T) {
	service := bootstrap.CLIService(bootstrap.Foundation())
	var got app.CLIRuntimeRequest
	service.Runtime = func(_ context.Context, request app.CLIRuntimeRequest) (app.CLIRuntimeResult, error) {
		got = request
		return app.CLIRuntimeResult{Runtime: request.Runtime, Status: "succeeded"}, nil
	}

	var output bytes.Buffer
	exit := RunWithApplication(
		context.Background(),
		[]string{"run", "--runtime", "codex", "--", "codex", "--help"},
		"test-version",
		strings.NewReader(""),
		&output,
		io.Discard,
		service,
	)
	if exit != ExitSuccess {
		t.Fatalf("runtime wrapper exit=%d output=%q", exit, output.String())
	}
	if !reflect.DeepEqual(got.Argv, []string{"codex", "--help"}) {
		t.Fatalf("child argv=%q, want exact child argv", got.Argv)
	}

	output.Reset()
	exit = RunWithApplication(
		context.Background(),
		[]string{"run", "--help"},
		"test-version",
		strings.NewReader(""),
		&output,
		io.Discard,
		service,
	)
	if exit != ExitSuccess || !strings.Contains(output.String(), "Usage:") {
		t.Fatalf("subcommand help exit=%d output=%q", exit, output.String())
	}
}

func TestIntegrationRejectsUnsupportedSelectorsBeforeDispatch(t *testing.T) {
	projectA := t.TempDir()
	cwdMismatch := t.TempDir()
	service := bootstrap.CLIService(bootstrap.Foundation())
	calls := 0
	service.Integration = func(_ context.Context, request app.CLIIntegrationRequest) (any, domain.DomainError) {
		calls++
		return request, domain.DomainError{}
	}

	for _, selector := range [][]string{
		{"--workspace", cwdMismatch},
		{"--store", filepath.Join(cwdMismatch, "state.db")},
	} {
		for _, subcommand := range []string{"plan", "apply", "remove", "status"} {
			t.Run(subcommand+"/"+selector[0], func(t *testing.T) {
				var output bytes.Buffer
				args := []string{"integration", subcommand, "--project", projectA}
				args = append(args, selector...)
				args = append(args, "--json")
				exit := RunWithApplication(context.Background(), args, "test-version", strings.NewReader(""), &output, io.Discard, service)
				if exit != ExitUsage {
					t.Fatalf("exit=%d output=%s", exit, output.String())
				}
				envelope := decodeEnvelope(t, output.String(), false)
				var failure ErrorMetadata
				if err := json.Unmarshal(envelope["error"], &failure); err != nil {
					t.Fatal(err)
				}
				if failure.Code != string(domain.CodeInvalidArgument) || failure.Message != "integration request does not support --workspace or --store" {
					t.Fatalf("error=%+v", failure)
				}
			})
		}
	}
	if calls != 0 {
		t.Fatalf("integration dispatched %d times with unsupported selectors", calls)
	}
}

func TestIntegrationRejectsUnsupportedRequestShapesBeforeServiceCall(t *testing.T) {
	project := t.TempDir()
	for _, test := range []struct {
		name string
		args []string
	}{
		{"missing subcommand", nil},
		{"unknown subcommand", []string{"unknown"}},
		{"plan status", []string{"plan", "--status"}},
		{"apply status", []string{"apply", "--status"}},
		{"status status", []string{"status", "--status"}},
		{"idempotency key", []string{"plan", "--idempotency-key", "key"}},
		{"empty idempotency key", []string{"plan", "--idempotency-key", ""}},
		{"runtime", []string{"plan", "--runtime", "codex"}},
		{"empty runtime", []string{"plan", "--runtime", ""}},
		{"plan file", []string{"plan", "--plan-file", "plan.json"}},
		{"empty plan file", []string{"plan", "--plan-file", ""}},
		{"approval file", []string{"plan", "--approval-file", "approval.json"}},
		{"empty approval file", []string{"plan", "--approval-file", ""}},
		{"integrity", []string{"plan", "--integrity"}},
		{"stdio", []string{"plan", "--stdio"}},
		{"output", []string{"plan", "--output", "output.json"}},
		{"empty output", []string{"plan", "--output", ""}},
		{"format", []string{"plan", "--format", "json"}},
		{"empty format", []string{"plan", "--format", ""}},
		{"session", []string{"plan", "--session", "session"}},
		{"empty session", []string{"plan", "--session", ""}},
		{"task", []string{"plan", "--task", "task"}},
		{"empty task", []string{"plan", "--task", ""}},
		{"workspace", []string{"plan", "--workspace", t.TempDir()}},
		{"store", []string{"plan", "--store", filepath.Join(t.TempDir(), "state.db")}},
		{"payload", []string{"plan", "--payload", "{}"}},
		{"empty payload", []string{"plan", "--payload", ""}},
		{"payload file", []string{"plan", "--payload-file", "payload.json"}},
		{"empty payload file", []string{"plan", "--payload-file", ""}},
		{"payload stdin", []string{"plan", "--payload-stdin"}},
		{"child argv", []string{"plan", "--", "child", "--flag"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := bootstrap.CLIService(bootstrap.Foundation())
			calls := 0
			service.Integration = func(_ context.Context, request app.CLIIntegrationRequest) (any, domain.DomainError) {
				calls++
				return request, domain.DomainError{}
			}

			args := append([]string{"integration"}, test.args...)
			args = append(args, "--project", project, "--json")
			if test.name == "child argv" {
				args = []string{"integration", "plan", "--project", project, "--json", "--", "child", "--flag"}
			}
			var output bytes.Buffer
			exit := RunWithApplication(context.Background(), args, "test-version", strings.NewReader(""), &output, io.Discard, service)
			if exit != ExitUsage {
				t.Fatalf("exit=%d output=%s", exit, output.String())
			}
			envelope := decodeEnvelope(t, output.String(), false)
			var failure ErrorMetadata
			if err := json.Unmarshal(envelope["error"], &failure); err != nil {
				t.Fatal(err)
			}
			if failure.Code != string(domain.CodeInvalidArgument) {
				t.Fatalf("error=%+v", failure)
			}
			if calls != 0 {
				t.Fatalf("integration dispatched %d times", calls)
			}
		})
	}
}

func TestIntegrationAllowsDocumentedRequestShapes(t *testing.T) {
	project := t.TempDir()
	for _, test := range []struct {
		name       string
		subcommand string
		status     bool
	}{
		{"status", "status", false},
		{"plan", "plan", false},
		{"apply", "apply", false},
		{"remove", "remove", false},
		{"remove with status", "remove", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := bootstrap.CLIService(bootstrap.Foundation())
			calls := 0
			service.Integration = func(_ context.Context, request app.CLIIntegrationRequest) (any, domain.DomainError) {
				calls++
				if request.Project != project || request.Subcommand != test.subcommand || request.Status != test.status {
					t.Fatalf("request=%+v", request)
				}
				return request, domain.DomainError{}
			}
			args := []string{"integration", test.subcommand, "--project", project, "--json"}
			if test.status {
				args = append(args, "--status")
			}
			var output bytes.Buffer
			exit := RunWithApplication(context.Background(), args, "test-version", strings.NewReader(""), &output, io.Discard, service)
			if exit != ExitSuccess {
				t.Fatalf("exit=%d output=%s", exit, output.String())
			}
			if calls != 1 {
				t.Fatalf("integration dispatched %d times", calls)
			}
		})
	}
}

func TestIntegrationRequiresExplicitNonemptyProjectBeforeServiceCall(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	for _, test := range []struct {
		name string
		args []string
	}{
		{"absent", []string{"integration", "plan", "--json"}},
		{"empty", []string{"integration", "plan", "--project", "", "--json"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := bootstrap.CLIService(bootstrap.Foundation())
			calls := 0
			service.Integration = func(_ context.Context, request app.CLIIntegrationRequest) (any, domain.DomainError) {
				calls++
				return request, domain.DomainError{}
			}
			var output bytes.Buffer
			exit := RunWithApplication(context.Background(), test.args, "test-version", strings.NewReader(""), &output, io.Discard, service)
			if exit != ExitUsage {
				t.Fatalf("exit=%d output=%s", exit, output.String())
			}
			if calls != 0 {
				t.Fatalf("integration dispatched %d times", calls)
			}
		})
	}
}

func TestBoardRejectsInvalidShapesBeforeQueryDispatch(t *testing.T) {
	project := t.TempDir()
	for _, test := range []struct {
		name string
		args []string
	}{
		{"missing mode", []string{"board", "--project", project, "--json"}},
		{"invalid mode", []string{"board", "invalid", "--project", project, "--json"}},
		{"me missing session", []string{"board", "me", "--project", project, "--json"}},
		{"me empty session", []string{"board", "me", "--session", "", "--project", project, "--json"}},
		{"me task selector", []string{"board", "me", "--task", "task", "--project", project, "--json"}},
		{"me empty task selector", []string{"board", "me", "--session", "session", "--task", "", "--project", project, "--json"}},
		{"task missing selector", []string{"board", "task", "--project", project, "--json"}},
		{"task empty selector", []string{"board", "task", "--task", "", "--project", project, "--json"}},
		{"task session selector", []string{"board", "task", "--session", "session", "--task", "task", "--project", project, "--json"}},
		{"task empty session selector", []string{"board", "task", "--session", "", "--task", "task", "--project", project, "--json"}},
		{"all session selector", []string{"board", "all", "--session", "session", "--project", project, "--json"}},
		{"all empty session selector", []string{"board", "all", "--session", "", "--project", project, "--json"}},
		{"tree task selector", []string{"board", "tree", "--task", "task", "--project", project, "--json"}},
		{"tree empty task selector", []string{"board", "tree", "--task", "", "--project", project, "--json"}},
		{"git session selector", []string{"board", "git", "--session", "session", "--project", project, "--json"}},
		{"git empty session selector", []string{"board", "git", "--session", "", "--project", project, "--json"}},
		{"empty format", []string{"board", "all", "--format", "", "--project", project, "--json"}},
		{"unknown format", []string{"board", "all", "--format", "xml", "--project", project, "--json"}},
		{"json with format", []string{"board", "all", "--format", "json", "--project", project, "--json"}},
		{"output", []string{"board", "all", "--output", "board.txt", "--project", project, "--json"}},
		{"empty output", []string{"board", "all", "--output", "", "--project", project, "--json"}},
		{"integrity", []string{"board", "all", "--integrity", "--project", project, "--json"}},
		{"status", []string{"board", "all", "--status", "--project", project, "--json"}},
		{"stdio", []string{"board", "all", "--stdio", "--project", project, "--json"}},
		{"runtime", []string{"board", "all", "--runtime", "codex", "--project", project, "--json"}},
		{"empty runtime", []string{"board", "all", "--runtime", "", "--project", project, "--json"}},
		{"plan file", []string{"board", "all", "--plan-file", "plan.json", "--project", project, "--json"}},
		{"empty plan file", []string{"board", "all", "--plan-file", "", "--project", project, "--json"}},
		{"approval file", []string{"board", "all", "--approval-file", "approval.json", "--project", project, "--json"}},
		{"empty approval file", []string{"board", "all", "--approval-file", "", "--project", project, "--json"}},
		{"idempotency key", []string{"board", "all", "--idempotency-key", "key", "--project", project, "--json"}},
		{"empty idempotency key", []string{"board", "all", "--idempotency-key", "", "--project", project, "--json"}},
		{"payload", []string{"board", "all", "--payload", "{}", "--project", project, "--json"}},
		{"empty payload", []string{"board", "all", "--payload", "", "--project", project, "--json"}},
		{"payload file", []string{"board", "all", "--payload-file", "payload.json", "--project", project, "--json"}},
		{"empty payload file", []string{"board", "all", "--payload-file", "", "--project", project, "--json"}},
		{"payload stdin", []string{"board", "all", "--payload-stdin", "--project", project, "--json"}},
		{"trailing argv", []string{"board", "all", "--project", project, "--json", "--", "child"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := bootstrap.CLIService(bootstrap.Foundation())
			dispatcher := &recordingDispatcher{}
			service.Dispatcher = dispatcher
			var output bytes.Buffer
			exit := RunWithApplication(context.Background(), test.args, "test-version", strings.NewReader(""), &output, io.Discard, service)
			if exit != ExitUsage {
				t.Fatalf("exit=%d output=%s", exit, output.String())
			}
			if len(dispatcher.requests) != 0 {
				t.Fatalf("board dispatched %d queries", len(dispatcher.requests))
			}
		})
	}
}

func TestBoardAllowsDocumentedRequestShapes(t *testing.T) {
	project := t.TempDir()
	model, err := query.NewViewModel("board", "cursor", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		args []string
	}{
		{"me json", []string{"board", "me", "--session", "session", "--project", project, "--json"}},
		{"tree tty", []string{"board", "tree", "--project", project}},
		{"task markdown", []string{"board", "task", "--task", "task", "--project", project, "--format", "markdown"}},
		{"all json format", []string{"board", "all", "--project", project, "--format", "json"}},
		{"git html", []string{"board", "git", "--project", project, "--format", "html"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := bootstrap.CLIService(bootstrap.Foundation())
			dispatcher := &recordingDispatcher{outcome: app.Outcome{Data: model}}
			service.Dispatcher = dispatcher
			service.RenderBoard = func(string, any, io.Writer) error { return nil }
			var output bytes.Buffer
			exit := RunWithApplication(context.Background(), test.args, "test-version", strings.NewReader(""), &output, io.Discard, service)
			if exit != ExitSuccess {
				t.Fatalf("exit=%d output=%s", exit, output.String())
			}
			if len(dispatcher.requests) != 1 || dispatcher.requests[0].Command != "board.query" {
				t.Fatalf("requests=%+v", dispatcher.requests)
			}
		})
	}
}

func TestGenericRuntimeWrapperExecutesOnlyExplicitArgv(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	exit := RunContext(
		context.Background(),
		[]string{"run", "--runtime", "test", "--", executable, "-test.run=^$"},
		"test-version",
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if exit != ExitSuccess || !strings.Contains(stdout.String(), "succeeded") {
		t.Fatalf("runtime wrapper exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), executable) {
		t.Fatal("runtime wrapper exposed resolved executable path")
	}
	exit = RunContext(
		context.Background(),
		[]string{"run", "--runtime", "test", "--json", "--", executable},
		"test-version",
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if exit != ExitUsage {
		t.Fatalf("JSON runtime wrapper exit=%d", exit)
	}
}

func TestShellInitAndCompletionCommandsEmitStaticScripts(t *testing.T) {
	exit, output := run(t, "shell-init", "bash")
	if exit != ExitSuccess || !strings.Contains(output, "omg_preflight") || !strings.Contains(output, "omg_board") || strings.Contains(output, "eval ") {
		t.Fatalf("bash init exit=%d: %s", exit, output)
	}
	exit, output = run(t, "completion", "powershell", "--json")
	if exit != ExitSuccess {
		t.Fatalf("PowerShell completion exit=%d: %s", exit, output)
	}
	var result struct {
		Shell   string `json:"shell"`
		Content string `json:"content"`
	}
	decodeData(t, output, &result)
	if result.Shell != "powershell" || !strings.Contains(result.Content, "Register-ArgumentCompleter") {
		t.Fatalf("PowerShell completion = %+v", result)
	}
	exit, output = run(t, "completion", "tcsh", "--json")
	if exit != ExitUsage || strings.Contains(output, "tcsh") {
		t.Fatalf("unsupported shell exit=%d: %s", exit, output)
	}
}

func TestPreflightRejectsUnsupportedOptionPresenceBeforeDispatch(t *testing.T) {
	project := t.TempDir()
	for _, test := range []struct {
		name string
		args []string
	}{
		{"subcommand", []string{"preflight", "status", "--project", project, "--json"}},
		{"empty session", []string{"preflight", "--session", "", "--project", project, "--json"}},
		{"integrity", []string{"preflight", "--integrity", "--project", project, "--json"}},
		{"status", []string{"preflight", "--status", "--project", project, "--json"}},
		{"stdio", []string{"preflight", "--stdio", "--project", project, "--json"}},
		{"runtime", []string{"preflight", "--runtime", "codex", "--project", project, "--json"}},
		{"empty runtime", []string{"preflight", "--runtime", "", "--project", project, "--json"}},
		{"output", []string{"preflight", "--output", "report.json", "--project", project, "--json"}},
		{"empty output", []string{"preflight", "--output", "", "--project", project, "--json"}},
		{"plan file", []string{"preflight", "--plan-file", "plan.json", "--project", project, "--json"}},
		{"empty plan file", []string{"preflight", "--plan-file", "", "--project", project, "--json"}},
		{"approval file", []string{"preflight", "--approval-file", "approval.json", "--project", project, "--json"}},
		{"empty approval file", []string{"preflight", "--approval-file", "", "--project", project, "--json"}},
		{"idempotency key", []string{"preflight", "--idempotency-key", "key", "--project", project, "--json"}},
		{"empty idempotency key", []string{"preflight", "--idempotency-key", "", "--project", project, "--json"}},
		{"format", []string{"preflight", "--format", "html", "--project", project, "--json"}},
		{"empty format", []string{"preflight", "--format", "", "--project", project, "--json"}},
		{"task", []string{"preflight", "--task", "task-1", "--project", project, "--json"}},
		{"empty task", []string{"preflight", "--task", "", "--project", project, "--json"}},
		{"payload", []string{"preflight", "--payload", "{}", "--project", project, "--json"}},
		{"empty payload", []string{"preflight", "--payload", "", "--project", project, "--json"}},
		{"payload file", []string{"preflight", "--payload-file", "payload.json", "--project", project, "--json"}},
		{"empty payload file", []string{"preflight", "--payload-file", "", "--project", project, "--json"}},
		{"payload stdin", []string{"preflight", "--payload-stdin", "--project", project, "--json"}},
		{"trailing argv", []string{"preflight", "--project", project, "--json", "--", "child"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := bootstrap.CLIService(bootstrap.Foundation())
			dispatcher := &recordingDispatcher{}
			service.Dispatcher = dispatcher
			var output bytes.Buffer
			exit := RunWithApplication(context.Background(), test.args, "test-version", strings.NewReader(""), &output, io.Discard, service)
			if exit != ExitUsage {
				t.Fatalf("exit=%d output=%s", exit, output.String())
			}
			if len(dispatcher.requests) != 0 {
				t.Fatalf("preflight dispatched %d queries", len(dispatcher.requests))
			}
		})
	}
}

func TestExportRejectsUnsupportedOptionsBeforeQueryDispatch(t *testing.T) {
	project := t.TempDir()
	for _, test := range []struct {
		name string
		args []string
	}{
		{"missing JSON mode", []string{"export", "--project", project}},
		{"missing output", []string{"export", "html", "--project", project}},
		{"empty output", []string{"export", "html", "--output", "", "--project", project}},
		{"JSON with format subcommand", []string{"export", "html", "--output", "board.html", "--project", project, "--json"}},
		{"invalid subcommand", []string{"export", "xml", "--output", "board.xml", "--project", project}},
		{"integrity", []string{"export", "--integrity", "--project", project, "--json"}},
		{"status", []string{"export", "--status", "--project", project, "--json"}},
		{"stdio", []string{"export", "--stdio", "--project", project, "--json"}},
		{"runtime", []string{"export", "--runtime", "codex", "--project", project, "--json"}},
		{"empty runtime", []string{"export", "--runtime", "", "--project", project, "--json"}},
		{"format", []string{"export", "--format", "html", "--project", project, "--json"}},
		{"empty format", []string{"export", "--format", "", "--project", project, "--json"}},
		{"session", []string{"export", "--session", "session-1", "--project", project, "--json"}},
		{"empty session", []string{"export", "--session", "", "--project", project, "--json"}},
		{"task", []string{"export", "--task", "task-1", "--project", project, "--json"}},
		{"empty task", []string{"export", "--task", "", "--project", project, "--json"}},
		{"plan file", []string{"export", "--plan-file", "plan.json", "--project", project, "--json"}},
		{"empty plan file", []string{"export", "--plan-file", "", "--project", project, "--json"}},
		{"approval file", []string{"export", "--approval-file", "approval.json", "--project", project, "--json"}},
		{"empty approval file", []string{"export", "--approval-file", "", "--project", project, "--json"}},
		{"idempotency key", []string{"export", "--idempotency-key", "key", "--project", project, "--json"}},
		{"empty idempotency key", []string{"export", "--idempotency-key", "", "--project", project, "--json"}},
		{"payload", []string{"export", "--payload", "{}", "--project", project, "--json"}},
		{"empty payload", []string{"export", "--payload", "", "--project", project, "--json"}},
		{"payload file", []string{"export", "--payload-file", "payload.json", "--project", project, "--json"}},
		{"empty payload file", []string{"export", "--payload-file", "", "--project", project, "--json"}},
		{"payload stdin", []string{"export", "--payload-stdin", "--project", project, "--json"}},
		{"trailing argv", []string{"export", "--project", project, "--json", "--", "child"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := bootstrap.CLIService(bootstrap.Foundation())
			dispatcher := &recordingDispatcher{}
			service.Dispatcher = dispatcher
			var output bytes.Buffer
			exit := RunWithApplication(context.Background(), test.args, "test-version", strings.NewReader(""), &output, io.Discard, service)
			if exit != ExitUsage {
				t.Fatalf("exit=%d output=%s", exit, output.String())
			}
			if len(dispatcher.requests) != 0 {
				t.Fatalf("export dispatched %d queries", len(dispatcher.requests))
			}
		})
	}
}
