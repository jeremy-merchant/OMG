package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jeremy-merchant/OMG/internal/app"
	"github.com/jeremy-merchant/OMG/internal/app/foundation"
	"github.com/jeremy-merchant/OMG/internal/bootstrap"
	"github.com/jeremy-merchant/OMG/internal/domain"
	"github.com/jeremy-merchant/OMG/internal/platform"
	"github.com/jeremy-merchant/OMG/internal/ports"
	"github.com/jeremy-merchant/OMG/internal/store/sqlite"
)

func TestDelegationRegistrationRequiresNonArgvPayloadTransport(t *testing.T) {
	root, service := currentPayloadService(t)
	invoke := func(input string, args ...string) (int, string) {
		t.Helper()
		var output bytes.Buffer
		exit := runWithContext(context.Background(), args, "test-version", &output, bootstrap.CLIService(service), strings.NewReader(input), io.Discard)
		return exit, output.String()
	}

	for _, command := range [][]string{
		{"human", "create", "--project", root, "--idempotency-key", "payload-human", "--payload", `{"id":"payload-human","display_name":"Payload Human","confidence":"verified"}`, "--json"},
		{"session", "create", "--project", root, "--idempotency-key", "payload-root", "--payload", `{"id":"payload-root","human_id":"payload-human","runtime":"test","role":"owner","source_ref":"payload-test","native_access_state":"unsupported"}`, "--json"},
	} {
		if exit, output := invoke("", command...); exit != ExitSuccess {
			t.Fatalf("fixture command exit=%d output=%s", exit, output)
		}
	}

	issue := func(key string) string {
		t.Helper()
		exit, output := invoke("", "delegate", "issue", "--project", root, "--idempotency-key", key,
			"--payload", `{"parent_session_id":"payload-root","ttl_seconds":300}`, "--json")
		if exit != ExitSuccess {
			t.Fatalf("delegate issue exit=%d output=%s", exit, output)
		}
		var result struct {
			RawToken string `json:"raw_token"`
		}
		decodeData(t, output, &result)
		if result.RawToken == "" {
			t.Fatal("delegate issue omitted raw token")
		}
		return result.RawToken
	}

	rawToken := issue("payload-stdin-issue")
	registerPayload := func(id, token string) string {
		payload, err := json.Marshal(map[string]any{
			"raw_token": token, "parent_session_id": "payload-root",
			"session": map[string]any{"id": id, "runtime": "test", "role": "worker", "source_ref": "payload-test", "native_access_state": "unsupported"},
		})
		if err != nil {
			t.Fatal(err)
		}
		return string(payload)
	}

	inline := registerPayload("payload-child-stdin", rawToken)
	exit, output := invoke("", "delegate", "register", "--project", root, "--idempotency-key", "payload-register-inline", "--payload", inline, "--json")
	if exit != ExitUsage {
		t.Fatalf("inline token payload exit=%d output=%s", exit, output)
	}
	if strings.Contains(output, rawToken) {
		t.Fatal("inline token appeared in rejection output")
	}

	exit, output = invoke(inline, "delegate", "register", "--project", root, "--idempotency-key", "payload-register-stdin", "--payload-stdin", "--json")
	if exit != ExitSuccess {
		t.Fatalf("stdin token payload exit=%d output=%s", exit, output)
	}
	if strings.Contains(output, rawToken) {
		t.Fatal("stdin token appeared in success output")
	}

	secondToken := issue("payload-conflict-issue")
	secondPayload := registerPayload("payload-child-conflict", secondToken)
	exit, output = invoke(secondPayload, "delegate", "register", "--project", root, "--idempotency-key", "payload-register-conflict",
		"--payload-stdin", "--payload", secondPayload, "--json")
	if exit != ExitUsage {
		t.Fatalf("multiple payload transports exit=%d output=%s", exit, output)
	}
	if strings.Contains(output, secondToken) {
		t.Fatal("conflicting token payload appeared in rejection output")
	}
}

func TestBackupRestorePayloadReachesRestoreValidation(t *testing.T) {
	var output bytes.Buffer
	exit := RunWithApplication(
		context.Background(),
		[]string{"backup", "restore", "--payload", "{", "--json"},
		"test-version",
		strings.NewReader(""),
		&output,
		io.Discard,
		bootstrap.CLIService(bootstrap.Foundation()),
	)
	if exit != ExitUsage || !strings.Contains(output.String(), "backup restore plan payload is invalid") {
		t.Fatalf("restore payload exit=%d output=%s", exit, output.String())
	}
}

func TestBackupRestoreAcceptsEachPayloadTransportAndRejectsMixedSources(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	payloadDir := filepath.Join(root, "private")
	if err := os.Mkdir(payloadDir, 0o700); err != nil {
		t.Fatal(err)
	}
	payloadFile := filepath.Join(payloadDir, "restore.json")
	payloads := map[string]foundation.RestorePlanRequest{
		"inline": {
			BackupPath: "/backups/inline.db", BackupChecksum: "sha256:inline", DestinationPath: "/restores/inline.db",
		},
		"file": {
			BackupPath: "/backups/file.db", BackupChecksum: "sha256:file", DestinationPath: "/restores/file.db",
		},
		"stdin": {
			BackupPath: "/backups/stdin.db", BackupChecksum: "sha256:stdin", DestinationPath: "/restores/stdin.db",
		},
	}
	encoded := make(map[string]string, len(payloads))
	for source, payload := range payloads {
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		encoded[source] = string(data)
	}
	if err := os.WriteFile(payloadFile, []byte(encoded["file"]), 0o600); err != nil {
		t.Fatal(err)
	}

	run := func(t *testing.T, args []string, input string) (*restorePlanSpy, int, string) {
		t.Helper()
		application := bootstrap.CLIService(bootstrap.Foundation())
		spy := &restorePlanSpy{Foundation: application.Foundation}
		application.Foundation = spy
		var output bytes.Buffer
		exit := RunWithApplication(context.Background(), append([]string{"backup", "restore"}, args...), "test-version",
			strings.NewReader(input), &output, io.Discard, application)
		return spy, exit, output.String()
	}

	for _, test := range []struct {
		name    string
		args    []string
		input   string
		payload foundation.RestorePlanRequest
	}{
		{name: "inline", args: []string{"--payload", encoded["inline"], "--json"}, payload: payloads["inline"]},
		{name: "file", args: []string{"--payload-file", payloadFile, "--json"}, payload: payloads["file"]},
		{name: "stdin", args: []string{"--payload-stdin", "--json"}, input: encoded["stdin"], payload: payloads["stdin"]},
	} {
		t.Run(test.name, func(t *testing.T) {
			spy, exit, output := run(t, test.args, test.input)
			if exit != ExitSuccess {
				t.Fatalf("restore payload exit=%d output=%s", exit, output)
			}
			if len(spy.calls) != 1 || spy.calls[0] != test.payload {
				t.Fatalf("PlanRestore calls=%+v, want one call with %+v", spy.calls, test.payload)
			}
		})
	}

	for _, test := range []struct {
		name  string
		args  []string
		input string
	}{
		{name: "inline-file", args: []string{"--payload", encoded["inline"], "--payload-file", payloadFile, "--json"}},
		{name: "inline-stdin", args: []string{"--payload", encoded["inline"], "--payload-stdin", "--json"}, input: encoded["stdin"]},
		{name: "file-stdin", args: []string{"--payload-file", payloadFile, "--payload-stdin", "--json"}, input: encoded["stdin"]},
	} {
		t.Run(test.name, func(t *testing.T) {
			spy, exit, output := run(t, test.args, test.input)
			if exit != ExitUsage || !strings.Contains(output, "backup restore plan payload is invalid") {
				t.Fatalf("mixed restore payload exit=%d output=%s", exit, output)
			}
			if len(spy.calls) != 0 {
				t.Fatalf("PlanRestore was called for mixed payload sources: %+v", spy.calls)
			}
		})
	}
}

type restorePlanSpy struct {
	app.Foundation
	calls []foundation.RestorePlanRequest
}

func (spy *restorePlanSpy) PlanRestore(_ context.Context, _ foundation.Selection, payload foundation.RestorePlanRequest) (foundation.RestorePlan, domain.DomainError) {
	spy.calls = append(spy.calls, payload)
	return foundation.RestorePlan{PlanID: "restore-plan"}, domain.DomainError{}
}

func TestApplicationPayloadTransportRejectsEmptyExplicitTransportWithStdin(t *testing.T) {
	root, service := currentPayloadService(t)
	for _, test := range []struct {
		name      string
		transport []string
	}{
		{name: "empty-inline", transport: []string{"--payload", "", "--payload-stdin"}},
		{name: "empty-file", transport: []string{"--payload-file", "", "--payload-stdin"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload := `{"id":"payload-rejected-` + test.name + `","display_name":"Rejected Payload","confidence":"verified"}`
			args := append([]string{"human", "create", "--project", root, "--idempotency-key", "reject-" + test.name}, test.transport...)
			args = append(args, "--json")
			input := &payloadReadCounter{Reader: strings.NewReader(payload)}
			var output bytes.Buffer
			exit := runWithContext(context.Background(), args, "test-version", &output, bootstrap.CLIService(service), input, io.Discard)
			if exit != ExitUsage {
				t.Fatalf("exit=%d output=%s", exit, output.String())
			}
			if input.reads != 0 {
				t.Fatalf("stdin was read %d times for rejected payload transports", input.reads)
			}

			output.Reset()
			exit = runWithContext(context.Background(),
				[]string{"human", "create", "--project", root, "--idempotency-key", "accept-" + test.name, "--payload", payload, "--json"},
				"test-version", &output, bootstrap.CLIService(service), strings.NewReader(""), io.Discard)
			if exit != ExitSuccess {
				t.Fatalf("rejected request wrote a human: exit=%d output=%s", exit, output.String())
			}
		})
	}
}

type payloadReadCounter struct {
	io.Reader
	reads int
}

func (reader *payloadReadCounter) Read(payload []byte) (int, error) {
	reader.reads++
	return reader.Reader.Read(payload)
}

func TestDecodeRecordsEmptyPayloadTransportPresence(t *testing.T) {
	for _, test := range []struct {
		name                 string
		args                 []string
		payloadProvided      bool
		payloadFileProvided  bool
		payloadStdinProvided bool
	}{
		{name: "inline", args: []string{"task", "create", "--payload", ""}, payloadProvided: true},
		{name: "file", args: []string{"task", "create", "--payload-file", ""}, payloadFileProvided: true},
		{name: "stdin", args: []string{"task", "create", "--payload-stdin"}, payloadStdinProvided: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, err := Decode(test.args)
			if err.Code != "" {
				t.Fatalf("Decode() error=%v", err)
			}
			if request.PayloadProvided != test.payloadProvided ||
				request.PayloadFileProvided != test.payloadFileProvided ||
				request.PayloadStdin != test.payloadStdinProvided {
				t.Fatalf("payload transport presence = (%t, %t, %t), want (%t, %t, %t)",
					request.PayloadProvided, request.PayloadFileProvided, request.PayloadStdin,
					test.payloadProvided, test.payloadFileProvided, test.payloadStdinProvided)
			}
		})
	}
}
func TestGitReadCommandsDefaultMissingPayloadToEmptyObject(t *testing.T) {
	for _, subcommand := range []string{"current", "latest", "history", "diff", "cleanup-plan"} {
		t.Run(subcommand, func(t *testing.T) {
			payload, err := loadApplicationPayload(Request{Name: "git", Subcommand: subcommand}, strings.NewReader(""))
			if err != nil || payload != "{}" {
				t.Fatalf("payload=%q err=%v; want empty object", payload, err)
			}
		})
	}

	if payload, err := loadApplicationPayload(Request{Name: "task", Subcommand: "get"}, strings.NewReader("")); err == nil {
		t.Fatalf("task.get payload=%q; want missing transport rejection", payload)
	}
}

func currentPayloadService(t *testing.T) (string, *foundation.Service) {
	t.Helper()
	ctx := context.Background()
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
	now := time.Now().UTC()
	approval := foundation.ApprovalFile{
		ApprovalID: "payload-transport-approval", ApprovedBy: "test", EvidenceReference: "payload-transport-test",
		PlanID: plan.ID, Project: plan.Project, FromVersion: plan.FromVersion, ToVersion: plan.ToVersion,
		Checksums: plan.Checksums, BackupLocation: plan.BackupLocation, BackupChecksum: backup.Checksum,
		Command: "omg migration apply", Timestamp: now.Format(time.RFC3339Nano), ExpiresAtRaw: now.Add(5 * time.Minute).Format(time.RFC3339Nano),
	}
	if applyErr := service.Apply(ctx, selection, plan, approval); applyErr.Code != "" {
		t.Fatal(applyErr)
	}
	return root, service
}
