package foundation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
)

type restorePathInspector func(string) bool

func (inspect restorePathInspector) FreshDestination(path string) bool {
	return inspect(path)
}

func (restorePathInspector) SameDirectory(string, string) bool {
	return false
}

func TestPlanRestoreValidatesBackupAndLeavesFreshDestinationUntouched(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(root, "verified-backup.db")
	destination := filepath.Join(root, "fresh-restored.db")
	inspections := 0
	service := New(Dependencies{
		Resolver: resolverStub{resolved: ports.ResolvedStore{Project: domain.ProjectID("project")}},
		PathInspector: restorePathInspector(func(path string) bool {
			return path == destination
		}),
		InspectBackup: func(_ context.Context, path, checksum string) (ports.BackupInspection, error) {
			inspections++
			if path != backup || checksum != "sha256:verified" {
				t.Fatalf("inspection path=%q checksum=%q", path, checksum)
			}
			return ports.BackupInspection{Checksum: checksum, SchemaVersion: 5, Integrity: true, Compatible: true}, nil
		},
	})
	plan, planErr := service.PlanRestore(context.Background(), Selection{Project: root}, RestorePlanRequest{
		BackupPath: backup, BackupChecksum: "sha256:verified", DestinationPath: destination,
	})
	if planErr.Code != "" {
		t.Fatal(planErr)
	}
	if inspections != 1 || plan.Project != "project" || plan.BackupChecksum != "sha256:verified" || plan.SchemaVersion != 5 ||
		!plan.Integrity || !plan.Compatible || plan.ApplyAvailable || plan.PlanID == "" || plan.DestinationFingerprint == "" || plan.RequiredAction == "" {
		t.Fatalf("restore plan = %+v inspections=%d", plan, inspections)
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("restore planning mutated destination: %v", err)
	}
}

func TestPlanRestoreRejectsUnsafeOrUnverifiedInputsWithoutMutation(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(root, "backup.db")
	fresh := filepath.Join(root, "fresh.db")
	existing := filepath.Join(root, "existing.db")
	if err := os.WriteFile(existing, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name       string
		request    RestorePlanRequest
		inspection ports.BackupInspection
		inspectErr error
		wantCode   domain.ErrorCode
	}{
		{"relative backup", RestorePlanRequest{BackupPath: "backup.db", BackupChecksum: "sum", DestinationPath: fresh}, ports.BackupInspection{}, nil, domain.CodeInvalidArgument},
		{"relative destination", RestorePlanRequest{BackupPath: backup, BackupChecksum: "sum", DestinationPath: "fresh.db"}, ports.BackupInspection{}, nil, domain.CodeInvalidArgument},
		{"same source and destination", RestorePlanRequest{BackupPath: backup, BackupChecksum: "sum", DestinationPath: backup}, ports.BackupInspection{}, nil, domain.CodeInvalidArgument},
		{"existing destination", RestorePlanRequest{BackupPath: backup, BackupChecksum: "sum", DestinationPath: existing}, ports.BackupInspection{}, nil, domain.CodeInvalidArgument},
		{"checksum inspection failure", RestorePlanRequest{BackupPath: backup, BackupChecksum: "bad", DestinationPath: fresh}, ports.BackupInspection{}, errors.New("checksum mismatch"), domain.CodeConflict},
		{"integrity false", RestorePlanRequest{BackupPath: backup, BackupChecksum: "sum", DestinationPath: fresh}, ports.BackupInspection{Checksum: "sum", Compatible: true}, nil, domain.CodeConflict},
		{"compatibility false", RestorePlanRequest{BackupPath: backup, BackupChecksum: "sum", DestinationPath: fresh}, ports.BackupInspection{Checksum: "sum", Integrity: true}, nil, domain.CodeConflict},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			service := New(Dependencies{
				Resolver: resolverStub{resolved: ports.ResolvedStore{Project: domain.ProjectID("project")}},
				PathInspector: restorePathInspector(func(path string) bool {
					return path != existing
				}),
				InspectBackup: func(context.Context, string, string) (ports.BackupInspection, error) {
					return test.inspection, test.inspectErr
				},
			})
			if _, got := service.PlanRestore(context.Background(), Selection{Project: root}, test.request); got.Code != test.wantCode {
				t.Fatalf("error=%+v; want %s", got, test.wantCode)
			}
			if data, err := os.ReadFile(existing); err != nil || string(data) != "preserve" {
				t.Fatalf("existing destination changed: %q %v", data, err)
			}
			if test.request.DestinationPath == fresh {
				if _, err := os.Lstat(fresh); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("fresh destination mutated: %v", err)
				}
			}
		})
	}
}
