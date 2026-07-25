// Package runtime executes an explicitly supplied process invocation.
package runtime

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

const (
	maxRuntimeLabelLength = 64
	maxArgvCount          = 256
	maxArgumentLength     = 32 << 10
	maxArgvLength         = 1 << 20
	maxExecutableLength   = 128
)

var (
	ErrInvalidRequest      = errors.New("runtime run invalid request")
	ErrInvalidDependencies = errors.New("runtime run invalid dependencies")
	ErrExecutableNotFound  = errors.New("runtime run executable not found")
	ErrCancelled           = errors.New("runtime run cancelled")
	ErrStartFailed         = errors.New("runtime run start failed")
	ErrWaitFailed          = errors.New("runtime run wait failed")
	ErrExited              = errors.New("runtime run exited")
)

// RunRequest identifies an explicit process invocation. Runtime is provenance metadata;
// it never affects executable selection. Argv[0] is the only requested executable.
type RunRequest struct {
	Runtime   string
	Argv      []string
	Directory string
}

// Dependencies supplies the process streams explicitly. Nil streams are rejected so the
// runner does not inherit ambient terminal streams.
type Dependencies struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Resolution reports how the explicitly supplied executable was resolved without
// exposing the resolved filesystem location.
type Resolution string

const (
	ResolutionPath     Resolution = "path"
	ResolutionExplicit Resolution = "explicit"
)

// Status is the safe outcome classification for a process attempt.
type Status string

const (
	StatusSucceeded   Status = "succeeded"
	StatusExited      Status = "exited"
	StatusNotFound    Status = "not_found"
	StatusCancelled   Status = "cancelled"
	StatusStartFailed Status = "start_failed"
	StatusWaitFailed  Status = "wait_failed"
)

// RunResult contains no command arguments, output, stderr, or resolved path.
type RunResult struct {
	Runtime    string     `json:"runtime"`
	Executable string     `json:"executable"`
	Resolution Resolution `json:"resolution"`
	Status     Status     `json:"status"`
	ExitCode   int        `json:"exit_code"`
}

// RunError identifies an execution outcome without exposing platform error details.
type RunError struct {
	Kind     error
	ExitCode int
}

func (e *RunError) Error() string {
	if errors.Is(e.Kind, ErrExited) {
		return "runtime run exited with status " + strconv.Itoa(e.ExitCode)
	}
	return e.Kind.Error()
}

func (e *RunError) Unwrap() error { return e.Kind }

// Run executes only request.Argv[0], passing the remaining arguments directly to the
// operating system. It never invokes a shell or derives an executable from Runtime.
func Run(ctx context.Context, request RunRequest, dependencies Dependencies) (RunResult, error) {
	if ctx == nil || validateRequest(request) != nil {
		return RunResult{}, ErrInvalidRequest
	}
	if dependencies.Stdin == nil || dependencies.Stdout == nil || dependencies.Stderr == nil {
		return RunResult{}, ErrInvalidDependencies
	}

	argv := append([]string(nil), request.Argv...)
	result := RunResult{
		Runtime: request.Runtime, Executable: safeExecutableName(argv[0]),
		Resolution: resolutionFor(argv[0]), ExitCode: -1,
	}
	resolved, err := exec.LookPath(argv[0])
	if err != nil {
		if ctx.Err() != nil {
			result.Status = StatusCancelled
			return result, &RunError{Kind: ErrCancelled, ExitCode: -1}
		}
		result.Status = StatusNotFound
		return result, &RunError{Kind: ErrExecutableNotFound, ExitCode: -1}
	}

	command := exec.CommandContext(ctx, resolved, argv[1:]...)
	command.Args = argv
	command.Dir = request.Directory
	command.Stdin = dependencies.Stdin
	command.Stdout = dependencies.Stdout
	command.Stderr = dependencies.Stderr
	command.Cancel = configureProcessCancellation(command)

	if err := command.Start(); err != nil {
		if ctx.Err() != nil {
			result.Status = StatusCancelled
			return result, &RunError{Kind: ErrCancelled, ExitCode: -1}
		}
		result.Status = StatusStartFailed
		return result, &RunError{Kind: ErrStartFailed, ExitCode: -1}
	}
	if err := command.Wait(); err != nil {
		if ctx.Err() != nil {
			result.Status = StatusCancelled
			return result, &RunError{Kind: ErrCancelled, ExitCode: -1}
		}
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			result.Status = StatusExited
			result.ExitCode = exitError.ExitCode()
			return result, &RunError{Kind: ErrExited, ExitCode: result.ExitCode}
		}
		result.Status = StatusWaitFailed
		return result, &RunError{Kind: ErrWaitFailed, ExitCode: -1}
	}
	result.Status = StatusSucceeded
	result.ExitCode = 0
	return result, nil
}

func validateRequest(request RunRequest) error {
	if !validRuntimeLabel(request.Runtime) || len(request.Argv) == 0 || len(request.Argv) > maxArgvCount ||
		strings.IndexByte(request.Directory, 0) >= 0 {
		return ErrInvalidRequest
	}
	totalLength := 0
	for index, argument := range request.Argv {
		if (index == 0 && argument == "") || len(argument) > maxArgumentLength || strings.IndexByte(argument, 0) >= 0 {
			return ErrInvalidRequest
		}
		totalLength += len(argument)
		if totalLength > maxArgvLength {
			return ErrInvalidRequest
		}
	}
	return nil
}

func validRuntimeLabel(label string) bool {
	if label == "" || len(label) > maxRuntimeLabelLength {
		return false
	}
	for index := range len(label) {
		value := label[index]
		if (value >= 'a' && value <= 'z') || (value >= '0' && value <= '9') {
			continue
		}
		if index > 0 && (value == '-' || value == '_' || value == '.') {
			continue
		}
		return false
	}
	return true
}

func resolutionFor(executable string) Resolution {
	if strings.ContainsAny(executable, "/\\") {
		return ResolutionExplicit
	}
	return ResolutionPath
}

func executableName(executable string) string {
	if index := strings.LastIndexAny(executable, "/\\"); index >= 0 {
		return executable[index+1:]
	}
	return executable
}
func safeExecutableName(executable string) string {
	name := executableName(executable)
	if len(name) == 0 || len(name) > maxExecutableLength || containsControl(name) || secretLike(name) {
		return "redacted"
	}
	return name
}

func containsControl(value string) bool {
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return true
		}
	}
	return false
}

func secretLike(value string) bool {
	value = strings.ToLower(value)
	for _, marker := range []string{"token", "secret", "password", "credential", "api_key", "apikey", "private"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}
