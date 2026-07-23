package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	queryapp "example.invalid/coordledger/internal/app/query"
	"example.invalid/coordledger/internal/domain"
	coord "example.invalid/coordledger/internal/domain/coordination"
	"example.invalid/coordledger/internal/domain/lineage"
	"example.invalid/coordledger/internal/ports"
)

func TestV4ToV8ScopedReceiptsAuditAndHumansUpgradePreservesReferencedRows(t *testing.T) {
	ctx := context.Background()
	stateDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	store, _, err := Open(ctx, filepath.Join(stateDir, "state.db"), OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	v4 := []Migration{
		{Version: 1, SQL: foundationSQL},
		{Version: 2, SQL: coordinationSQL},
		{Version: 3, SQL: reservationSQL},
		{Version: 4, SQL: gitInventorySQL},
	}
	for _, migration := range v4 {
		if _, err := store.db.ExecContext(ctx, migration.SQL); err != nil {
			t.Fatalf("apply v%d fixture migration: %v", migration.Version, err)
		}
		if _, err := store.db.ExecContext(ctx, `INSERT INTO schema_migrations(version,checksum,applied_at) VALUES(?,?,?)`, migration.Version, checksumSQL(migration.SQL), "2026-01-01T00:00:00Z"); err != nil {
			t.Fatalf("record v%d fixture migration: %v", migration.Version, err)
		}
	}

	result := []byte(`{"checksum":"receipt","status":"ok"}`)
	payload := []byte(`{"checksum":"audit","receipt_id":"receipt-v4"}`)
	if _, err := store.db.ExecContext(ctx, `INSERT INTO command_receipts(id,idempotency_key,outcome,result_json,created_at) VALUES(?,?,?,?,?)`, "receipt-v4", "shared-key", "ok", result, "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO audit_events(id,receipt_id,event_type,payload_json,occurred_at) VALUES(?,?,?,?,?)`, "audit-v4", "receipt-v4", "command_completed", payload, "2026-01-01T00:00:01Z"); err != nil {
		t.Fatal(err)
	}
	for _, project := range []string{"project-a", "project-b", "project-c"} {
		if _, err := store.db.ExecContext(ctx, `INSERT INTO projects(id,created_at) VALUES(?,?)`, project, "2026-01-01T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	for _, human := range []string{"legacy-single", "legacy-multi", "legacy-unreferenced"} {
		if _, err := store.db.ExecContext(ctx, `INSERT INTO humans(id,display_name,provenance_confidence,created_at) VALUES(?,?,?,?)`, human, "Legacy Human", "explicit", "2026-01-01T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	for _, session := range []struct{ id, project, human string }{
		{"legacy-single-a", "project-a", "legacy-single"},
		{"legacy-multi-a", "project-a", "legacy-multi"},
		{"legacy-multi-b", "project-b", "legacy-multi"},
	} {
		if _, err := store.db.ExecContext(ctx, `INSERT INTO agent_sessions(id,project_id,human_id,lineage_kind,runtime,role,instruction_source,source_ref,native_access_state,started_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, session.id, session.project, session.human, "human_direct", "test", "owner", "human", "fixture", "unsupported", "2026-01-01T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}

	plan, _, approval := migrationApproval(t, store, "upgrade-project")
	if plan.FromVersion != 4 || plan.ToVersion != 8 {
		t.Fatalf("upgrade plan = v%d to v%d; want v4 to v8", plan.FromVersion, plan.ToVersion)
	}
	if err := store.ApplyMigrations(ctx, plan, approval); err != nil {
		t.Fatalf("upgrade to v8: %v", err)
	}

	var receiptProject string
	var migratedResult []byte
	if err := store.db.QueryRowContext(ctx, `SELECT project_id,result_json FROM command_receipts WHERE id='receipt-v4'`).Scan(&receiptProject, &migratedResult); err != nil {
		t.Fatal(err)
	}
	if receiptProject != "legacy" || !bytes.Equal(migratedResult, result) {
		t.Fatalf("migrated receipt = project %q, result %q", receiptProject, migratedResult)
	}
	for _, check := range []struct {
		project string
		human   lineage.ID
		want    bool
	}{
		{"project-a", "legacy-single", true},
		{"project-a", "legacy-multi", true},
		{"project-b", "legacy-single", false},
		{"project-b", "legacy-multi", true},
		{"project-c", "legacy-single", false},
		{"project-c", "legacy-multi", false},
		{"project-a", "legacy-unreferenced", false},
	} {
		if err := store.Scope(domain.ProjectID(check.project)).Read(ctx, func(r ports.Repositories) error {
			_, ok, err := r.Coordination().GetHuman(ctx, check.human)
			if err != nil || ok != check.want {
				t.Fatalf("%s visibility of %s = ok=%t err=%v; want %t", check.project, check.human, ok, err, check.want)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	view, err := queryapp.New(store).Query(ctx, domain.NewActorContext("scope", "project-a", "workspace", domain.InvocationCLI, []domain.Capability{domain.CapabilityRead}), queryapp.BoardRequest{Mode: queryapp.BoardMe, SessionID: "legacy-multi-a"})
	if err != nil {
		t.Fatalf("board did not resolve legacy human identity: %v", err)
	}
	var board queryapp.BoardSnapshot
	if err := json.Unmarshal(view.Data(), &board); err != nil {
		t.Fatal(err)
	}
	if board.Identity == nil || board.Identity.HumanID != "legacy-multi" || board.Identity.ProvenanceConfidence != "explicit" {
		t.Fatalf("board legacy identity = %#v", board.Identity)
	}
	var singleProject sql.NullString
	if err := store.db.QueryRowContext(ctx, `SELECT project_id FROM humans WHERE id='legacy-single'`).Scan(&singleProject); err != nil {
		t.Fatal(err)
	}
	if !singleProject.Valid || singleProject.String != "project-a" {
		t.Fatalf("single legacy human project = %#v; want project-a", singleProject)
	}
	var multiProject sql.NullString
	if err := store.db.QueryRowContext(ctx, `SELECT project_id FROM humans WHERE id='legacy-multi'`).Scan(&multiProject); err != nil {
		t.Fatal(err)
	}
	if multiProject.Valid {
		t.Fatalf("multi-project legacy human was narrowed to %q", multiProject.String)
	}

	createLegacySession := func(project string) error {
		_, _, err := store.Scope(domain.ProjectID(project)).Write(ctx, domain.IdempotencyKey("legacy-session-"+project), "test.write", func(r ports.Repositories) (domain.Result, error) {
			err := r.Coordination().CreateSession(ctx, lineage.AgentSession{ID: lineage.ID("new-" + project), ProjectID: lineage.ID(project), HumanID: "legacy-multi", Kind: lineage.HumanDirect, Runtime: "test", Role: "owner", Source: lineage.SourceHuman, SourceRef: "fixture", RootID: lineage.ID("new-" + project), StartedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
			return domain.Result{ID: domain.ResultID("new-" + project), Outcome: domain.OutcomeOK}, err
		})
		return err
	}
	if err := createLegacySession("project-a"); err != nil {
		t.Fatalf("existing legacy association did not permit session: %v", err)
	}
	if err := createLegacySession("project-c"); err == nil {
		t.Fatal("unrelated project newly bound a legacy human")
	}
	if _, _, err := store.Scope("project-c").Write(ctx, "legacy-recipient-sender", "test.write", func(r ports.Repositories) (domain.Result, error) {
		session := lineage.AgentSession{ID: "legacy-recipient-sender", ProjectID: "project-c", Kind: lineage.HumanDirect, Runtime: "test", Role: "owner", Source: lineage.SourceHuman, SourceRef: "fixture", RootID: "legacy-recipient-sender", StartedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
		return domain.Result{ID: "legacy-recipient-sender", Outcome: domain.OutcomeOK}, r.Coordination().CreateSession(ctx, session)
	}); err != nil {
		t.Fatalf("create unrelated message sender: %v", err)
	}
	createLegacyRecipientMessage := func(project, sender string) error {
		_, _, err := store.Scope(domain.ProjectID(project)).Write(ctx, domain.IdempotencyKey("legacy-recipient-"+project), "test.write", func(r ports.Repositories) (domain.Result, error) {
			message := coord.MailMessage{ID: "legacy-recipient-" + project, ThreadID: "legacy-recipient-thread-" + project, SenderSessionID: sender, Type: coord.MessageNotice, Subject: "legacy recipient", Body: "body", Recipients: []coord.RecipientTarget{{HumanID: "legacy-multi"}}, CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
			return domain.Result{ID: domain.ResultID(message.ID), Outcome: domain.OutcomeOK}, r.Coordination().CreateMessage(ctx, project, message)
		})
		return err
	}
	if err := createLegacyRecipientMessage("project-a", "legacy-multi-a"); err != nil {
		t.Fatalf("legacy multi-project recipient rejected in associated project-a: %v", err)
	}
	if err := createLegacyRecipientMessage("project-b", "legacy-multi-b"); err != nil {
		t.Fatalf("legacy multi-project recipient rejected in associated project-b: %v", err)
	}
	if err := createLegacyRecipientMessage("project-c", "legacy-recipient-sender"); err == nil {
		t.Fatal("unrelated project accepted legacy multi-project recipient")
	}
	if err := store.Scope("project-c").Read(ctx, func(r ports.Repositories) error {
		if _, ok, err := r.Coordination().GetMessage(ctx, "legacy-recipient-project-c"); err != nil || ok {
			return fmt.Errorf("unrelated legacy recipient message persisted: ok=%t err=%w", ok, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if _, _, err := store.Scope("project-c").Write(ctx, "new-scoped-human", "test.write", func(r ports.Repositories) (domain.Result, error) {
		human := lineage.Human{ID: "new-scoped", DisplayName: "New Scoped", Confidence: lineage.ConfidenceExplicit, CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
		return domain.Result{ID: "new-scoped", Outcome: domain.OutcomeOK}, r.Coordination().CreateHuman(ctx, human)
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Scope("project-a").Read(ctx, func(r ports.Repositories) error {
		if _, ok, err := r.Coordination().GetHuman(ctx, "new-scoped"); err != nil || ok {
			t.Fatalf("new scoped human leaked to unrelated project: ok=%t err=%v", ok, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	var auditProject, receiptID string
	var migratedPayload []byte
	if err := store.db.QueryRowContext(ctx, `SELECT project_id,receipt_id,payload_json FROM audit_events WHERE id='audit-v4'`).Scan(&auditProject, &receiptID, &migratedPayload); err != nil {
		t.Fatal(err)
	}
	if auditProject != "legacy" || receiptID != "receipt-v4" || !bytes.Equal(migratedPayload, payload) {
		t.Fatalf("migrated audit = project %q, receipt %q, payload %q", auditProject, receiptID, migratedPayload)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO audit_events(id,project_id,receipt_id,event_type,payload_json,occurred_at) VALUES(?,?,?,?,?,?)`, "audit-orphan", "legacy", "missing-receipt", "command_completed", []byte(`{}`), "2026-01-01T00:00:02Z"); err == nil {
		t.Fatal("post-upgrade orphan audit event succeeded")
	}

	assertForeignKeyCheckClean(t, ctx, store.db)
	var integrity string
	if err := store.db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		t.Fatal(err)
	}
	if integrity != "ok" {
		t.Fatalf("integrity_check = %q", integrity)
	}

	for _, project := range []string{"project-a", "project-b"} {
		if _, err := store.db.ExecContext(ctx, `INSERT INTO command_receipts(id,project_id,idempotency_key,outcome,result_json,created_at,operation) VALUES(?,?,?,?,?,?,?)`, "receipt-"+project, project, "shared-key", "ok", []byte(`{}`), "2026-01-01T00:00:02Z", "test.write"); err != nil {
			t.Fatalf("insert %s scoped receipt: %v", project, err)
		}
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO command_receipts(id,project_id,idempotency_key,outcome,result_json,created_at,operation) VALUES(?,?,?,?,?,?,?)`, "receipt-project-a-duplicate", "project-a", "shared-key", "ok", []byte(`{}`), "2026-01-01T00:00:03Z", "test.write"); err == nil {
		t.Fatal("duplicate project-scoped idempotency key succeeded")
	}
}

func assertForeignKeyCheckClean(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	rows, err := db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		var table string
		var rowID sql.NullInt64
		var parent string
		var foreignKeyIndex int
		if err := rows.Scan(&table, &rowID, &parent, &foreignKeyIndex); err != nil {
			t.Fatal(err)
		}
		t.Fatalf("foreign_key_check violation: table=%s rowid=%v parent=%s fk=%d", table, rowID, parent, foreignKeyIndex)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
