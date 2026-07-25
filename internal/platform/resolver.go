// Package platform provides local operating-system and repository resolution.
package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jeremy-merchant/OMG/internal/config"
	"github.com/jeremy-merchant/OMG/internal/domain"
	"github.com/jeremy-merchant/OMG/internal/ports"
)

// GitRunner invokes Git with a working directory and argv arguments only.
type GitRunner func(context.Context, string, ...string) (string, error)

// Dependencies keeps environmental resolution injectable for focused tests.
type Dependencies struct {
	Git               GitRunner
	LoadProjectConfig func(string) (config.Project, error)
	UserConfigDir     func() (string, error)
	UserStateDir      func() (string, error)
	Root              func(string) (string, error)
	Environment       func(string) string
	WorkingDir        func() (string, error)
}

// Resolver resolves a canonical store location; it never opens or migrates it.
type Resolver struct{ dependencies Dependencies }

func NewResolver(dependencies Dependencies) *Resolver {
	if dependencies.Git == nil {
		dependencies.Git = runGit
	}
	userConfigDirProvided := dependencies.UserConfigDir != nil
	if dependencies.UserConfigDir == nil {
		dependencies.UserConfigDir = os.UserConfigDir
	}
	if dependencies.UserStateDir == nil {
		if userConfigDirProvided {
			dependencies.UserStateDir = dependencies.UserConfigDir
		} else {
			dependencies.UserStateDir = defaultUserStateDir
		}
	}
	if dependencies.Root == nil {
		dependencies.Root = canonicalRoot
	}
	if dependencies.LoadProjectConfig == nil {
		dependencies.LoadProjectConfig = config.Load
	}
	if dependencies.Environment == nil {
		dependencies.Environment = os.Getenv
	}
	if dependencies.WorkingDir == nil {
		dependencies.WorkingDir = os.Getwd
	}
	return &Resolver{dependencies: dependencies}
}

// Resolve implements CLI explicit > safe environment > local override > tracked
// config > defaults. All returned locations are absolute and cleaned.
func (r *Resolver) Resolve(ctx context.Context, request ports.ResolveRequest) (ports.ResolvedStore, error) {
	projectInput := request.ProjectPath
	if projectInput == "" {
		var err error
		projectInput, err = r.dependencies.WorkingDir()
		if err != nil {
			return ports.ResolvedStore{}, fmt.Errorf("resolve working directory: %w", err)
		}
	}
	projectRoot, err := r.dependencies.Root(projectInput)
	if err != nil {
		return ports.ResolvedStore{}, fmt.Errorf("resolve project root: %w", err)
	}
	if err := validateResolvedProjectRoot(projectRoot); err != nil {
		return ports.ResolvedStore{}, fmt.Errorf("validate project root locality: %w", err)
	}
	projectConfig, err := r.dependencies.LoadProjectConfig(projectRoot)
	if err != nil {
		return ports.ResolvedStore{}, err
	}
	environment := make(map[string]string, 2)
	if value := r.dependencies.Environment("OMG_WORKSPACE"); value != "" {
		environment["OMG_WORKSPACE"] = value
	}
	if value := r.dependencies.Environment("OMG_STORE_PATH"); value != "" {
		environment["OMG_STORE_PATH"] = value
	}
	settings, err := projectConfig.Resolve(config.Inputs{
		Workspace:   request.WorkspacePath,
		StorePath:   request.StorePath,
		Environment: environment,
	})
	if err != nil {
		return ports.ResolvedStore{}, err
	}
	if settings.StorePath != "" {
		return r.resolveOverride(projectRoot, settings.StorePath)
	}
	if settings.Workspace != "" {
		return r.resolveWorkspace(projectRoot, settings.Workspace)
	}
	return r.resolveProject(ctx, projectRoot)
}

func (r *Resolver) resolveOverride(projectRoot, path string) (ports.ResolvedStore, error) {
	if !filepath.IsAbs(path) {
		return ports.ResolvedStore{}, errors.New("OMG_STORE_PATH must be an absolute path")
	}
	path = filepath.Clean(path)
	return ports.ResolvedStore{Path: path, Mode: ports.StoreModeOverride, Project: projectID(projectRoot), ProjectRoot: projectRoot}, nil
}

func (r *Resolver) resolveWorkspace(projectRoot, workspace string) (ports.ResolvedStore, error) {
	if !filepath.IsAbs(workspace) {
		return ports.ResolvedStore{}, errors.New("workspace must be an absolute directory")
	}
	workspaceRoot, err := r.dependencies.Root(workspace)
	if err != nil {
		return ports.ResolvedStore{}, fmt.Errorf("resolve workspace root: %w", err)
	}
	stateDir, err := r.dependencies.UserStateDir()
	if err != nil {
		return ports.ResolvedStore{}, fmt.Errorf("resolve user state directory: %w", err)
	}
	workspaceID := workspaceID(workspaceRoot)
	return ports.ResolvedStore{
		Path: filepath.Join(stateDir, "omg", "workspaces", string(workspaceID), "state.db"), Mode: ports.StoreModeWorkspace,
		Project: projectID(projectRoot), Workspace: workspaceID, ProjectRoot: projectRoot, WorkspaceRoot: workspaceRoot,
	}, nil
}

func (r *Resolver) resolveProject(ctx context.Context, projectRoot string) (ports.ResolvedStore, error) {
	bare, err := r.dependencies.Git(ctx, projectRoot, "rev-parse", "--is-bare-repository")
	if err == nil && strings.TrimSpace(bare) == "true" {
		return ports.ResolvedStore{}, errors.New("bare Git repositories require explicit workspace mode")
	}
	common, err := r.dependencies.Git(ctx, projectRoot, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err == nil {
		common = strings.TrimSpace(common)
		if !filepath.IsAbs(common) {
			return ports.ResolvedStore{}, errors.New("Git returned a non-absolute common directory")
		}
		stateDir, stateErr := r.dependencies.UserStateDir()
		if stateErr != nil {
			return ports.ResolvedStore{}, fmt.Errorf("resolve user state directory: %w", stateErr)
		}
		id := projectID(common)
		return ports.ResolvedStore{Path: filepath.Join(stateDir, "omg", "git", string(id), "state.db"), Mode: ports.StoreModeGit, Project: id, ProjectRoot: projectRoot, GitCommonDir: common}, nil
	}
	stateDir, err := r.dependencies.UserStateDir()
	if err != nil {
		return ports.ResolvedStore{}, fmt.Errorf("resolve user state directory: %w", err)
	}
	id := projectID(projectRoot)
	return ports.ResolvedStore{Path: filepath.Join(stateDir, "omg", "projects", string(id), "state.db"), Mode: ports.StoreModeProject, Project: id, ProjectRoot: projectRoot}, nil
}

func canonicalRoot(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	absolute = filepath.Clean(absolute)
	if evaluated, err := filepath.EvalSymlinks(absolute); err == nil {
		return filepath.Clean(evaluated), nil
	}
	return absolute, nil
}

func projectID(root string) domain.ProjectID     { return domain.ProjectID(stableID(root)) }
func workspaceID(root string) domain.WorkspaceID { return domain.WorkspaceID(stableID(root)) }

func stableID(root string) string {
	return stableIDForFilesystem(root, caseInsensitiveProjectRoot)
}

func stableIDForFilesystem(root string, caseInsensitive func(string) bool) string {
	if caseInsensitive(root) {
		root = strings.ToLower(root)
	}
	sum := sha256.Sum256([]byte(root))
	return hex.EncodeToString(sum[:16])
}

func runGit(ctx context.Context, cwd string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = cwd
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}
