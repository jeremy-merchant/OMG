// Package bootstrap owns concrete application composition.
package bootstrap

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"time"

	"example.invalid/coordledger/internal/app"
	"example.invalid/coordledger/internal/app/foundation"
	"example.invalid/coordledger/internal/app/query"
	"example.invalid/coordledger/internal/config"
	"example.invalid/coordledger/internal/domain"
	instructions "example.invalid/coordledger/internal/integration/instructions"
	"example.invalid/coordledger/internal/platform"
	"example.invalid/coordledger/internal/ports"
	runtimeexec "example.invalid/coordledger/internal/runtime"
	shellgen "example.invalid/coordledger/internal/shell"
	"example.invalid/coordledger/internal/store/sqlite"
	mcptransport "example.invalid/coordledger/internal/transport/mcp"
	"example.invalid/coordledger/internal/view"
	watchmode "example.invalid/coordledger/internal/watch"
)

// Foundation composes the local resolver and SQLite adapter outside the application layer.
func Foundation() *foundation.Service {
	return foundation.New(foundation.Dependencies{
		Resolver:          platform.NewResolver(platform.Dependencies{}),
		ConfigInitializer: platform.NewProjectConfigInitializer(),
		PathInspector:     platform.NewPathInspector(),
		Open: func(ctx context.Context, path string, options ports.OpenOptions) (ports.FoundationStore, ports.OpenStatus, error) {
			if options.WALEligible == nil {
				options.WALEligible = platform.WALEligible
			}
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
}

// Dispatcher composes the transport-neutral dispatcher with concrete adapters.
func Dispatcher(service *foundation.Service) *app.ServiceDispatcher {
	return app.NewDispatcherWithGitScanner(service, platform.NewGitScanner(platform.GitScannerDependencies{}), platform.NewPathInspector())
}

// CLIService composes the concrete platform capabilities consumed by the CLI
// through app-owned contracts. Transport code never receives store internals.
func CLIService(service *foundation.Service) app.CLIService {
	dispatcher := Dispatcher(service)
	return app.CLIService{
		Foundation: service,
		Dispatcher: dispatcher,
		MCPServe:   mcptransport.Serve,
		RenderBoard: func(format string, model any, output io.Writer) error {
			viewModel, ok := model.(query.ViewModel)
			if !ok {
				return errors.New("invalid board model")
			}
			return view.Render(view.Format(format), viewModel, output)
		},
		Watch: func(ctx context.Context, request app.CLIWatchRequest) (app.CLIWatchResult, domain.DomainError) {
			selection := foundation.Selection{Project: request.Project, Workspace: request.Workspace, Store: request.Store}
			var stateDir string
			if err := service.WithReadOnlyCurrentStore(ctx, selection, func(resolved ports.ResolvedStore, _ ports.Store) error {
				stateDir = filepath.Dir(resolved.Path)
				return nil
			}); err.Code != "" {
				return app.CLIWatchResult{}, err
			}
			engine, err := watchmode.NewSystem(stateDir, 4*time.Second, []watchmode.Callback{func(callbackContext context.Context) error {
				if callbackErr := request.Refresh(callbackContext); callbackErr.Code != "" {
					return callbackErr
				}
				return nil
			}})
			if err != nil {
				return app.CLIWatchResult{}, domain.NewError(domain.CodeUnavailable, "watch is unavailable", false)
			}
			if request.Status {
				return app.CLIWatchResult{Status: engine.Status(ctx)}, domain.DomainError{}
			}
			result := engine.Run(ctx)
			return app.CLIWatchResult{
				Result:   result,
				Stopped:  result.Code == watchmode.ResultStopped,
				Conflict: result.Code == watchmode.ResultConflict,
			}, domain.DomainError{}
		},
		Shell: func(name, shell string) (app.CLIShellResult, error) {
			var result shellgen.Result
			var err error
			if name == "shell-init" {
				result, err = shellgen.Init(shellgen.Shell(shell))
			} else {
				result, err = shellgen.Completion(shellgen.Shell(shell))
			}
			return app.CLIShellResult{Content: result, Text: result.Content}, err
		},
		Runtime: func(ctx context.Context, request app.CLIRuntimeRequest) (app.CLIRuntimeResult, error) {
			result, err := runtimeexec.Run(ctx, runtimeexec.RunRequest{Runtime: request.Runtime, Argv: request.Argv}, runtimeexec.Dependencies{
				Stdin: request.Stdin, Stdout: request.Stdout, Stderr: request.Stderr,
			})
			return app.CLIRuntimeResult{
				Value: result, Runtime: result.Runtime, Executable: result.Executable, Resolution: string(result.Resolution),
				Status: string(result.Status), ExitCode: result.ExitCode, Exited: errors.Is(err, runtimeexec.ErrExited),
				Invalid:  errors.Is(err, runtimeexec.ErrInvalidRequest) || errors.Is(err, runtimeexec.ErrInvalidDependencies),
				NotFound: errors.Is(err, runtimeexec.ErrExecutableNotFound), Cancelled: errors.Is(err, runtimeexec.ErrCancelled),
			}, err
		},
		Integration: func(ctx context.Context, request app.CLIIntegrationRequest) (any, domain.DomainError) {
			root, resolveErr := service.ResolveProjectRoot(ctx, foundation.Selection{Project: request.Project})
			if resolveErr.Code != "" {
				return nil, resolveErr
			}
			targets, err := configuredInstructionTargets(root)
			if err != nil {
				return nil, integrationError(err)
			}
			integration, err := instructions.New(root, targets, instructions.DefaultContent())
			if err != nil {
				return nil, integrationError(err)
			}
			switch request.Subcommand {
			case "status":
				result, err := integration.Status()
				return result, integrationError(err)
			case "plan":
				result, err := integration.Plan()
				return result, integrationError(err)
			case "apply":
				result, err := integration.Apply()
				return result, integrationError(err)
			case "remove":
				results, err := integration.Remove()
				if err != nil {
					return nil, integrationError(err)
				}
				if request.Status {
					status, err := integration.Status()
					if err != nil {
						return nil, integrationError(err)
					}
					return struct {
						Results []instructions.Result `json:"results"`
						Status  []instructions.Status `json:"status"`
					}{results, status}, domain.DomainError{}
				}
				return results, domain.DomainError{}
			default:
				return nil, domain.NewError(domain.CodeInvalidArgument, "integration subcommand is invalid", false)
			}
		},
	}
}

func integrationError(err error) domain.DomainError {
	if err == nil {
		return domain.DomainError{}
	}
	switch {
	case errors.Is(err, instructions.ErrUnsafeTarget), errors.Is(err, instructions.ErrMalformedBlock), errors.Is(err, instructions.ErrUnsupportedEncoding):
		return domain.NewError(domain.CodeInvalidArgument, "instruction surface is invalid", false)
	case errors.Is(err, instructions.ErrChanged):
		return domain.NewError(domain.CodeConflict, "instruction surface changed concurrently", true)
	default:
		return domain.NewError(domain.CodeUnavailable, "instruction surface is unavailable", false)
	}
}

func configuredInstructionTargets(root string) ([]instructions.Target, error) {
	project, err := config.Load(root)
	if err != nil {
		return nil, err
	}
	settings, err := project.Resolve(config.Inputs{})
	if err != nil {
		return nil, err
	}
	if len(settings.Integrations.InstructionTargets) == 0 {
		return instructions.DefaultTargets(), nil
	}
	targets := make([]instructions.Target, len(settings.Integrations.InstructionTargets))
	for i, path := range settings.Integrations.InstructionTargets {
		targets[i] = instructions.Target{Path: path}
	}
	return targets, nil
}
