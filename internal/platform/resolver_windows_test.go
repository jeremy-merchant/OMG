//go:build windows

package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"example.invalid/coordledger/internal/config"
	"example.invalid/coordledger/internal/ports"
)

func TestStableIDNormalizesWindowsPathCase(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Repo")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	variant := filepath.Join(filepath.Dir(root), strings.ToLower(filepath.Base(root)))
	if !caseInsensitiveProjectRoot(root) {
		t.Skip("filesystem does not resolve case variants to the same directory")
	}
	if upper, lower := stableID(root), stableID(variant); upper != lower {
		t.Fatalf("Windows path case produced different IDs: %q != %q", upper, lower)
	}
}

func TestResolveRejectsNonLocalWindowsProjectRootBeforeConfigLoad(t *testing.T) {
	originalDriveType := windowsDriveType
	defer func() { windowsDriveType = originalDriveType }()

	for _, projectRoot := range []string{`\\server\share\repo`, `Z:\remote\repo`} {
		t.Run(projectRoot, func(t *testing.T) {
			windowsDriveType = func(string) uint32 { return windowsDriveRemote }
			configLoads := 0
			gitCalls := 0
			configDirectoryCalls := 0
			resolver := NewResolver(Dependencies{
				LoadProjectConfig: func(string) (config.Project, error) {
					configLoads++
					return config.Project{}, nil
				},
				Root: func(string) (string, error) { return projectRoot, nil },
				Git: func(context.Context, string, ...string) (string, error) {
					gitCalls++
					return "", errors.New("git must not run")
				},
				UserConfigDir: func() (string, error) {
					configDirectoryCalls++
					return "", errors.New("user config must not resolve")
				},
			})

			_, err := resolver.Resolve(context.Background(), ports.ResolveRequest{ProjectPath: projectRoot})
			if !errors.Is(err, ErrNonLocalWindowsPath) {
				t.Fatalf("Resolve(%q) error = %v; want ErrNonLocalWindowsPath", projectRoot, err)
			}
			if configLoads != 0 || gitCalls != 0 || configDirectoryCalls != 0 {
				t.Fatalf("non-local root performed work: config loads=%d git calls=%d user config calls=%d", configLoads, gitCalls, configDirectoryCalls)
			}
		})
	}
}

func TestResolveAcceptsFixedLocalWindowsProjectRoot(t *testing.T) {
	originalDriveType := windowsDriveType
	defer func() { windowsDriveType = originalDriveType }()
	windowsDriveType = func(string) uint32 { return windowsDriveFixed }

	configLoads := 0
	resolver := NewResolver(Dependencies{
		LoadProjectConfig: func(root string) (config.Project, error) {
			configLoads++
			if root != `C:\repo` {
				t.Fatalf("config root = %q", root)
			}
			return config.Project{}, nil
		},
		Root: func(string) (string, error) { return `C:\repo`, nil },
		Git: func(context.Context, string, ...string) (string, error) {
			return "", errors.New("not a repository")
		},
		UserConfigDir: func() (string, error) { return `C:\users\alice\AppData\Roaming`, nil },
	})

	resolved, err := resolver.Resolve(context.Background(), ports.ResolveRequest{ProjectPath: `C:\repo`})
	if err != nil {
		t.Fatal(err)
	}
	if configLoads != 1 {
		t.Fatalf("config loads = %d; want 1", configLoads)
	}
	if resolved.ProjectRoot != `C:\repo` || resolved.Mode != ports.StoreModeProject {
		t.Fatalf("resolved = %#v", resolved)
	}
}
