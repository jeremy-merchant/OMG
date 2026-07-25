package platform_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeremy-merchant/OMG/internal/platform"
	"github.com/jeremy-merchant/OMG/internal/ports"
)

func TestResolveLinkedWorktreesUseCommonDirectory(t *testing.T) {
	common := filepath.Join(t.TempDir(), "repo.git")
	firstRoot := filepath.Join(t.TempDir(), "worktree one")
	secondRoot := filepath.Join(t.TempDir(), "worktree two")
	for _, root := range []string{firstRoot, secondRoot} {
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	resolver := testResolver(t, func(_ context.Context, _ string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "rev-parse --is-bare-repository":
			return "false\n", nil
		case "rev-parse --path-format=absolute --git-common-dir":
			return common + "\n", nil
		default:
			return "", errors.New("unexpected git command")
		}
	})
	first, err := resolver.Resolve(context.Background(), ports.ResolveRequest{ProjectPath: firstRoot})
	if err != nil {
		t.Fatal(err)
	}
	second, err := resolver.Resolve(context.Background(), ports.ResolveRequest{ProjectPath: secondRoot})
	if err != nil {
		t.Fatal(err)
	}
	if first.Path != second.Path {
		t.Fatalf("paths differ: %q != %q", first.Path, second.Path)
	}
	if first.Mode != ports.StoreModeGit {
		t.Fatalf("mode = %q", first.Mode)
	}
}

func TestResolveGitStoreUsesPrivateUserStateRoot(t *testing.T) {
	common := filepath.Join(t.TempDir(), "repo.git")
	root := filepath.Join(t.TempDir(), "worktree")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	stateRoot := filepath.Join(t.TempDir(), "private-state")
	resolver := platform.NewResolver(platform.Dependencies{
		Git: func(_ context.Context, _ string, args ...string) (string, error) {
			switch strings.Join(args, " ") {
			case "rev-parse --is-bare-repository":
				return "false\n", nil
			case "rev-parse --path-format=absolute --git-common-dir":
				return common + "\n", nil
			default:
				return "", errors.New("unexpected git command")
			}
		},
		UserConfigDir: func() (string, error) { return filepath.Join(t.TempDir(), "config"), nil },
		UserStateDir:  func() (string, error) { return stateRoot, nil },
	})

	resolved, err := resolver.Resolve(context.Background(), ports.ResolveRequest{ProjectPath: root})
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(stateRoot, "omg", "git", string(resolved.Project), "state.db")
	if resolved.Path != expected {
		t.Fatalf("path = %q; want %q", resolved.Path, expected)
	}
	if resolved.GitCommonDir != common || resolved.Mode != ports.StoreModeGit {
		t.Fatalf("resolved = %#v", resolved)
	}
	if strings.HasPrefix(resolved.Path, common+string(filepath.Separator)) {
		t.Fatalf("store remained inside Git metadata: %q", resolved.Path)
	}
}

func TestResolveNonGitIDIsStable(t *testing.T) {
	root := filepath.Join(t.TempDir(), "work space 한글")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	resolver := testResolver(t, func(context.Context, string, ...string) (string, error) {
		return "", errors.New("not a git repository")
	})
	first, err := resolver.Resolve(context.Background(), ports.ResolveRequest{ProjectPath: root})
	if err != nil {
		t.Fatal(err)
	}
	second, err := resolver.Resolve(context.Background(), ports.ResolveRequest{ProjectPath: root})
	if err != nil {
		t.Fatal(err)
	}
	if first.Project != second.Project || first.Path != second.Path {
		t.Fatalf("non-Git resolution is unstable: %#v %#v", first, second)
	}
	if first.Mode != ports.StoreModeProject {
		t.Fatalf("mode = %q", first.Mode)
	}
}

func TestResolveCanonicalizesSymlinkedProjectRoots(t *testing.T) {
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "project-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	resolver := testResolver(t, func(context.Context, string, ...string) (string, error) {
		return "", errors.New("not a git repository")
	})
	throughTarget, err := resolver.Resolve(context.Background(), ports.ResolveRequest{ProjectPath: target})
	if err != nil {
		t.Fatal(err)
	}
	throughLink, err := resolver.Resolve(context.Background(), ports.ResolveRequest{ProjectPath: link})
	if err != nil {
		t.Fatal(err)
	}
	if throughTarget.Project != throughLink.Project || throughTarget.Path != throughLink.Path {
		t.Fatalf("symlinked project did not resolve canonically: %#v %#v", throughTarget, throughLink)
	}
}

func TestResolveIgnoresAbsentEnvironmentOverrides(t *testing.T) {
	resolver := platform.NewResolver(platform.Dependencies{
		Git: func(context.Context, string, ...string) (string, error) {
			return "", errors.New("not a git repository")
		},
		UserConfigDir: func() (string, error) { return t.TempDir(), nil },
		Environment:   func(string) string { return "" },
	})
	if _, err := resolver.Resolve(context.Background(), ports.ResolveRequest{ProjectPath: t.TempDir()}); err != nil {
		t.Fatalf("absent environment overrides failed resolution: %v", err)
	}
}

func TestResolveExplicitWorkspace(t *testing.T) {
	configDir := t.TempDir()
	workspace := filepath.Join(t.TempDir(), "shared workspace")
	resolver := platform.NewResolver(platform.Dependencies{UserConfigDir: func() (string, error) { return configDir, nil }})
	resolved, err := resolver.Resolve(context.Background(), ports.ResolveRequest{ProjectPath: t.TempDir(), WorkspacePath: workspace})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Mode != ports.StoreModeWorkspace || resolved.WorkspaceRoot != workspace {
		t.Fatalf("resolved = %#v", resolved)
	}
}

func TestResolveRejectsRelativeOverrideAndBareRepository(t *testing.T) {
	resolver := platform.NewResolver(platform.Dependencies{Environment: func(string) string { return "relative.db" }})
	if _, err := resolver.Resolve(context.Background(), ports.ResolveRequest{ProjectPath: t.TempDir()}); err == nil {
		t.Fatal("relative override succeeded")
	}

	bare := testResolver(t, func(_ context.Context, _ string, args ...string) (string, error) {
		if strings.Join(args, " ") == "rev-parse --is-bare-repository" {
			return "true\n", nil
		}
		return "", errors.New("unexpected git command")
	})
	if _, err := bare.Resolve(context.Background(), ports.ResolveRequest{ProjectPath: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "workspace") {
		t.Fatalf("bare error = %v", err)
	}
}

func testResolver(t *testing.T, git platform.GitRunner) *platform.Resolver {
	t.Helper()
	configDir := t.TempDir()
	return platform.NewResolver(platform.Dependencies{Git: git, UserConfigDir: func() (string, error) { return configDir, nil }})
}
