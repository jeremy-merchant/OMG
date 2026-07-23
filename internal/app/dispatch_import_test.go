package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"example.invalid/coordledger/internal/app/foundation"
	"example.invalid/coordledger/internal/domain"
	"example.invalid/coordledger/internal/platform"
	"example.invalid/coordledger/internal/ports"
)

func importRecordDispatcher(t *testing.T) (*ServiceDispatcher, foundation.Selection) {
	t.Helper()
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
	service := foundation.New(dispatcherTestDependencies(resolver))
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
		ApprovalID: "import-dispatch-approval", ApprovedBy: "test", EvidenceReference: "import-dispatch-test", PlanID: plan.ID, Project: plan.Project,
		FromVersion: plan.FromVersion, ToVersion: plan.ToVersion, Checksums: plan.Checksums,
		BackupLocation: plan.BackupLocation, BackupChecksum: backup.Checksum, Command: "omg migration apply",
		Timestamp: now.Format(time.RFC3339Nano), ExpiresAtRaw: now.Add(5 * time.Minute).Format(time.RFC3339Nano),
	}
	if err := service.Apply(ctx, selection, plan, approval); err.Code != "" {
		t.Fatal(err)
	}
	return NewDispatcher(service), selection
}

func TestDispatchImportRecordProducesSafeResultAndReplaysIdempotently(t *testing.T) {
	disp, sel := importRecordDispatcher(t)
	req := Request{
		Version:        RequestVersion,
		Command:        "import.record",
		IdempotencyKey: "disp-import-1",
		Payload:        json.RawMessage(`{"source_record_id":"opaque-source-42","source_state":"ambiguous","title":"Review import","runtime":"generic","role":"reviewer"}`),
	}
	out, ok := disp.dispatchImport(context.Background(), req, sel)
	if !ok {
		t.Fatal("dispatchImport returned false for valid request")
	}
	if out.Error.Code != "" {
		t.Fatalf("error = %v", out.Error)
	}
	result, ok := out.Data.(ImportRecordResult)
	if !ok {
		t.Fatalf("Data type = %T; want ImportRecordResult", out.Data)
	}
	if result.SessionID == "" || result.TaskID == "" {
		t.Fatalf("empty session/task: %+v", result)
	}
	if strings.Contains(result.SessionID, "opaque-source-42") {
		t.Fatalf("secret leaked in session_id: %s", result.SessionID)
	}
	if result.Classification != "imported_unverified" {
		t.Fatalf("classification = %q; want imported_unverified", result.Classification)
	}

	// Replay with same idempotency key must produce identical result.
	out2, _ := disp.dispatchImport(context.Background(), req, sel)
	if out2.Error.Code != "" {
		t.Fatalf("replay error = %v", out2.Error)
	}
	result2 := out2.Data.(ImportRecordResult)
	if result.SessionID != result2.SessionID || result.TaskID != result2.TaskID {
		t.Fatalf("replay differs: first=%+v second=%+v", result, result2)
	}
}

func TestDispatchReceiptQueriesAreScopedAndRedacted(t *testing.T) {
	disp, sel := importRecordDispatcher(t)
	ctx := context.Background()
	request := Request{Version: RequestVersion, Command: "import.record", Project: sel.Project, IdempotencyKey: "receipt-query-key", Payload: json.RawMessage(`{"source_record_id":"opaque-source-42","source_state":"ambiguous","title":"Confidential import title","runtime":"generic","role":"reviewer"}`)}
	if result := disp.Dispatch(ctx, request); result.Error.Code != "" {
		t.Fatalf("import result = %+v", result)
	}
	var receiptID domain.ReceiptID
	if err := disp.service.WithReadOnlyCurrentStore(ctx, sel, func(_ ports.ResolvedStore, store ports.Store) error {
		return store.Read(ctx, func(repositories ports.Repositories) error {
			receipt, found, err := repositories.Receipts().FindReceipt(ctx, domain.IdempotencyKey(request.IdempotencyKey))
			if err == nil && found {
				receiptID = receipt.ID
			}
			return err
		})
	}); err.Code != "" {
		t.Fatal(err)
	}
	got := disp.Dispatch(ctx, Request{Version: RequestVersion, Command: "receipt.get", Project: sel.Project, Payload: json.RawMessage(`{"id":"` + string(receiptID) + `"}`)})
	if got.Error.Code != "" {
		t.Fatalf("receipt.get result = %+v", got)
	}
	view, ok := got.Data.(ReceiptView)
	if !ok || view.Operation != "import.record" || view.IdempotencyKey != "receipt-query-key" {
		t.Fatalf("receipt view = %#v", got.Data)
	}
	encoded, _ := json.Marshal(got.Data)
	if strings.Contains(string(encoded), "opaque-source-42") || strings.Contains(string(encoded), "Confidential import title") {
		t.Fatalf("receipt query leaked request content: %s", encoded)
	}
	list := disp.Dispatch(ctx, Request{Version: RequestVersion, Command: "receipt.list", Project: sel.Project, Payload: json.RawMessage(`{}`)})
	if list.Error.Code != "" {
		t.Fatalf("receipt.list result = %+v", list)
	}
	values, ok := list.Data.([]ReceiptView)
	if !ok || len(values) == 0 {
		t.Fatalf("receipt list = %#v", list.Data)
	}
}

func TestDispatchImportRecordWithParentTask(t *testing.T) {
	disp, sel := importRecordDispatcher(t)
	parentOutcome, _ := disp.dispatchImport(context.Background(), Request{
		Version:        RequestVersion,
		Command:        "import.record",
		IdempotencyKey: "disp-import-parent-root",
		Payload:        json.RawMessage(`{"source_record_id":"src-parent","source_state":"planned","title":"Parent import","runtime":"ci","role":"worker"}`),
	}, sel)
	if parentOutcome.Error.Code != "" {
		t.Fatalf("parent error = %v", parentOutcome.Error)
	}
	parent := parentOutcome.Data.(ImportRecordResult)
	payload, err := json.Marshal(ImportRecordPayload{
		SourceRecordID: "src-100",
		SourceState:    "planned",
		Title:          "Sub import",
		Runtime:        "ci",
		Role:           "worker",
		ParentTaskID:   parent.TaskID,
	})
	if err != nil {
		t.Fatal(err)
	}
	out, _ := disp.dispatchImport(context.Background(), Request{
		Version:        RequestVersion,
		Command:        "import.record",
		IdempotencyKey: "disp-import-child",
		Payload:        payload,
	}, sel)
	if out.Error.Code != "" {
		t.Fatalf("child error = %v", out.Error)
	}
	result := out.Data.(ImportRecordResult)
	if result.State != "READY" || result.SessionID == "" || result.TaskID == "" {
		t.Fatalf("result = %+v", result)
	}
}

func TestDispatchImportRejectsMissingIdempotencyKey(t *testing.T) {
	disp, _ := importRecordDispatcher(t)
	req := Request{
		Version: RequestVersion,
		Command: "import.record",
		Payload: json.RawMessage(`{"source_record_id":"x","source_state":"active","title":"X","runtime":"g","role":"w"}`),
	}
	out, ok := disp.dispatchImport(context.Background(), req, foundation.Selection{})
	if !ok {
		t.Fatal("dispatchImport returned false — should have handled and rejected")
	}
	if out.Error.Code == "" {
		t.Fatal("expected rejection for missing idempotency key")
	}
}

func TestDispatchImportRejectsBlankPayloadFields(t *testing.T) {
	disp, _ := importRecordDispatcher(t)
	for _, tc := range []struct {
		name    string
		payload string
	}{
		{"missing source_record_id", `{"source_state":"active","title":"X","runtime":"g","role":"w"}`},
		{"missing source_state", `{"source_record_id":"s","title":"X","runtime":"g","role":"w"}`},
		{"missing title", `{"source_record_id":"s","source_state":"active","runtime":"g","role":"w"}`},
		{"missing runtime", `{"source_record_id":"s","source_state":"active","title":"X","role":"w"}`},
		{"missing role", `{"source_record_id":"s","source_state":"active","title":"X","runtime":"g"}`},
		{"empty source_record_id", `{"source_record_id":"","source_state":"active","title":"X","runtime":"g","role":"w"}`},
		{"empty source_state", `{"source_record_id":"s","source_state":"","title":"X","runtime":"g","role":"w"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := Request{
				Version:        RequestVersion,
				Command:        "import.record",
				IdempotencyKey: "blank-field",
				Payload:        json.RawMessage(tc.payload),
			}
			out, ok := disp.dispatchImport(context.Background(), req, foundation.Selection{})
			if !ok {
				t.Fatal("dispatchImport returned false")
			}
			if out.Error.Code == "" {
				t.Fatalf("expected rejection")
			}
		})
	}
}

func TestDispatchImportRejectsUnknownPayloadFields(t *testing.T) {
	disp, _ := importRecordDispatcher(t)
	req := Request{
		Version:        RequestVersion,
		Command:        "import.record",
		IdempotencyKey: "unknown-field",
		Payload:        json.RawMessage(`{"source_record_id":"s","source_state":"active","title":"X","runtime":"g","role":"w","execution_command":"rm -rf /"}`),
	}
	out, ok := disp.dispatchImport(context.Background(), req, foundation.Selection{})
	if !ok {
		t.Fatal("dispatchImport returned false")
	}
	if out.Error.Code == "" {
		t.Fatal("expected rejection for unknown field")
	}
}

func TestDispatchImportValidatesSourceState(t *testing.T) {
	disp, sel := importRecordDispatcher(t)
	req := Request{
		Version:        RequestVersion,
		Command:        "import.record",
		IdempotencyKey: "invalid-state",
		Payload:        json.RawMessage(`{"source_record_id":"s","source_state":"bogus","title":"X","runtime":"g","role":"w"}`),
	}
	out, ok := disp.dispatchImport(context.Background(), req, sel)
	if !ok {
		t.Fatal("dispatchImport returned false")
	}
	if out.Error.Code == "" {
		t.Fatal("expected rejection for invalid state")
	}
	if !errors.Is(out.Error, domain.NewError(domain.CodeInvalidArgument, "invalid import record", false)) {
		t.Fatalf("error = %v; want invalid import record", out.Error)
	}
}

func TestDispatchImportReturnsPrivacySafeDTO(t *testing.T) {
	disp, sel := importRecordDispatcher(t)
	req := Request{
		Version:        RequestVersion,
		Command:        "import.record",
		IdempotencyKey: "privacy-test",
		Payload:        json.RawMessage(`{"source_record_id":"opaque-record-id","source_state":"active","title":"Hidden title","runtime":"custom","role":"owner"}`),
	}
	out, _ := disp.dispatchImport(context.Background(), req, sel)
	if out.Error.Code != "" {
		t.Fatalf("unexpected error = %v", out.Error)
	}
	dt, ok := out.Data.(ImportRecordResult)
	if !ok {
		t.Fatalf("type = %T; want ImportRecordResult", out.Data)
	}
	marshaled, err := json.Marshal(dt)
	if err != nil {
		t.Fatal(err)
	}
	s := string(marshaled)
	if strings.Contains(s, "opaque-record-id") || strings.Contains(s, "Hidden title") || strings.Contains(s, "custom") || strings.Contains(s, "owner") {
		t.Fatalf("PII leaked in DTO: %s", s)
	}
}
