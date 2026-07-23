package platform_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"example.invalid/coordledger/internal/platform"
)

func TestProjectConfigInitializerCreatesPrivateProjectConfig(t *testing.T) {
	root := t.TempDir()
	initializer := platform.NewProjectConfigInitializer()
	if err := initializer.InitializeProjectConfig(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if err := initializer.InitializeProjectConfig(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, ".omg", "project.toml")
	contents, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "# OMG project configuration\n" {
		t.Fatalf("project config = %q", contents)
	}
	if info, err := os.Stat(filepath.Join(root, ".omg", "private")); err != nil || !info.IsDir() {
		t.Fatalf("private operator directory = %v, %v", info, err)
	}
}
