package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jeremy-merchant/oh-my-group/internal/app/foundation"
)

func TestProductionFoundationInitializesFreshOwnerOnlySelection(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	stateDir := filepath.Join(root, "state")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(stateDir, "state.db")

	service := Foundation()
	status, err := service.Init(context.Background(), foundation.Selection{Project: project, Store: store})
	if err.Code != "" {
		t.Fatalf("production foundation init failed: %+v", err)
	}
	if !status.Initialized || status.Pending == 0 {
		t.Fatalf("fresh init status = %+v; want initialized with explicit pending migrations", status)
	}
	for _, path := range []string{filepath.Join(project, ".omg", "project.toml"), store} {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatalf("expected initialized artifact %s: %v", path, statErr)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("initialized artifact is not owner-only: %s mode=%#o", path, info.Mode().Perm())
		}
	}
}
