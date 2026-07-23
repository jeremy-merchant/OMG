package app

import (
	"context"
	"io"

	"example.invalid/coordledger/internal/app/foundation"
	"example.invalid/coordledger/internal/domain"
)

// Foundation is the narrow application capability required by CLI commands.
// Its store resolution is intentionally not exposed to transport.
type Foundation interface {
	Init(context.Context, foundation.Selection) (foundation.Status, domain.DomainError)
	Status(context.Context, foundation.Selection, bool) (foundation.Status, domain.DomainError)
	Plan(context.Context, foundation.Selection) (foundation.Plan, domain.DomainError)
	Backup(context.Context, foundation.Selection, *foundation.Plan) (foundation.Backup, domain.DomainError)
	Apply(context.Context, foundation.Selection, foundation.Plan, foundation.ApprovalFile) domain.DomainError
	PlanRestore(context.Context, foundation.Selection, foundation.RestorePlanRequest) (foundation.RestorePlan, domain.DomainError)
}

type CLIService struct {
	Foundation  Foundation
	Dispatcher  Dispatcher
	MCPServe    func(context.Context, io.Reader, io.Writer, string, Dispatcher) error
	RenderBoard func(string, any, io.Writer) error
	Watch       func(context.Context, CLIWatchRequest) (CLIWatchResult, domain.DomainError)
	Shell       func(string, string) (CLIShellResult, error)
	Runtime     func(context.Context, CLIRuntimeRequest) (CLIRuntimeResult, error)
	Integration func(context.Context, CLIIntegrationRequest) (any, domain.DomainError)
}

type CLIWatchRequest struct {
	Project   string
	Workspace string
	Store     string
	Status    bool
	Refresh   func(context.Context) domain.DomainError
}

type CLIWatchResult struct {
	Status   any
	Result   any
	Stopped  bool
	Conflict bool
}

type CLIShellResult struct {
	Content any
	Text    string
}

type CLIRuntimeRequest struct {
	Runtime string
	Argv    []string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
}

type CLIRuntimeResult struct {
	Value      any
	Runtime    string
	Executable string
	Resolution string
	Status     string
	ExitCode   int
	Exited     bool
	Invalid    bool
	NotFound   bool
	Cancelled  bool
}

type CLIIntegrationRequest struct {
	Project    string
	Subcommand string
	Status     bool
}
