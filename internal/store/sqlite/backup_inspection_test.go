package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jeremy-merchant/OMG/internal/ports"
)

func TestInspectBackupValidatesChecksumIntegrityAndSchema(t *testing.T) {
	ctx := context.Background()
	store := migratedStore(t, OpenOptions{})
	defer store.Close()
	backupPath := filepath.Join(t.TempDir(), "backup.db")
	backup, err := store.Backup(ctx, ports.BackupDestination(backupPath))
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectBackup(ctx, backupPath, backup.Checksum)
	if err != nil {
		t.Fatal(err)
	}
	if !inspection.Integrity || !inspection.Compatible || inspection.SchemaVersion != 11 || inspection.Checksum != backup.Checksum {
		t.Fatalf("inspection = %#v", inspection)
	}
	if _, err := InspectBackup(ctx, backupPath, "sha256:wrong"); err == nil {
		t.Fatal("checksum mismatch accepted")
	}
}
