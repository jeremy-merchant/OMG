package runtime

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunPreservesExactArgvWithoutShellExpansion(t *testing.T) {
	t.Setenv("GO_WANT_RUNTIME_HELPER", "1")
	var stdout, stderr bytes.Buffer
	argv := []string{"-test.run=TestRuntimeHelperProcess", "mode=echo", "two words", "$HOME", "; touch should-not-run", "*", "$(echo nope)"}

	result, err := Run(context.Background(), RunRequest{
		Runtime:   "generic-runtime",
		Argv:      append([]string{os.Args[0]}, argv...),
		Directory: t.TempDir(),
	}, Dependencies{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := stdout.String(), strings.Join(argv, "\x1f"); got != want {
		t.Fatalf("stdout = %q, want exact argv %q", got, want)
	}
	if result.Executable != filepath.Base(os.Args[0]) || result.Resolution != ResolutionExplicit {
		t.Fatalf("unsafe or incorrect resolution result: %#v", result)
	}
}

func TestRunRuntimeIsProvenanceOnly(t *testing.T) {
	t.Setenv("GO_WANT_RUNTIME_HELPER", "1")
	request := func(runtime string) RunRequest {
		return RunRequest{Runtime: runtime, Argv: []string{os.Args[0], "-test.run=TestRuntimeHelperProcess", "mode=echo", "identity"}}
	}
	for _, runtime := range []string{"alpha", "beta"} {
		var stdout bytes.Buffer
		result, err := Run(context.Background(), request(runtime), Dependencies{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &bytes.Buffer{}})
		if err != nil {
			t.Fatalf("Run(%q) error = %v", runtime, err)
		}
		if got, want := stdout.String(), "-test.run=TestRuntimeHelperProcess\x1fmode=echo\x1fidentity"; got != want {
			t.Fatalf("runtime %q changed executable argv: got %q, want %q", runtime, got, want)
		}
		if result.Runtime != runtime {
			t.Fatalf("result runtime = %q, want %q", result.Runtime, runtime)
		}
	}
}

func TestRunStreamsStandardIOAndPropagatesExitStatus(t *testing.T) {
	t.Setenv("GO_WANT_RUNTIME_HELPER", "1")
	var stdout, stderr bytes.Buffer
	result, err := Run(context.Background(), RunRequest{Runtime: "runner", Argv: []string{os.Args[0], "-test.run=TestRuntimeHelperProcess", "mode=stdio", "7"}}, Dependencies{
		Stdin: strings.NewReader("input payload"), Stdout: &stdout, Stderr: &stderr,
	})
	if !errors.Is(err, ErrExited) {
		t.Fatalf("Run() error = %v, want exit error", err)
	}
	if result.Status != StatusExited || result.ExitCode != 7 {
		t.Fatalf("result = %#v, want exited status 7", result)
	}
	if got, want := stdout.String(), "out:input payload"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if got, want := stderr.String(), "err:input payload"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

func TestRunReportsCancellationAndKillsProcess(t *testing.T) {
	t.Setenv("GO_WANT_RUNTIME_HELPER", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result, err := Run(ctx, RunRequest{Runtime: "runner", Argv: []string{os.Args[0], "-test.run=TestRuntimeHelperProcess", "mode=wait"}}, Dependencies{
		Stdin: strings.NewReader(""), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{},
	})
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("Run() error = %v, want cancellation", err)
	}
	if result.Status != StatusCancelled {
		t.Fatalf("result = %#v, want cancelled", result)
	}
}

func TestRunResolvesOnlySuppliedExecutableThroughPath(t *testing.T) {
	t.Setenv("GO_WANT_RUNTIME_HELPER", "1")
	directory := t.TempDir()
	name := "runtime-helper-on-path"
	source, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	target, err := os.OpenFile(filepath.Join(directory, name), os.O_CREATE|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(target, source); err != nil {
		t.Fatal(err)
	}
	if err := target.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)

	var stdout bytes.Buffer
	result, err := Run(context.Background(), RunRequest{
		Runtime: "provenance", Argv: []string{name, "-test.run=TestRuntimeHelperProcess", "mode=echo", "path"},
	}, Dependencies{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Executable != name || result.Resolution != ResolutionPath {
		t.Fatalf("result = %#v, want redacted PATH resolution", result)
	}
	if got, want := stdout.String(), "-test.run=TestRuntimeHelperProcess\x1fmode=echo\x1fpath"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestRunReportsMissingBinaryAndRedactsPaths(t *testing.T) {
	missing := "omg-runtime-missing-binary"
	result, err := Run(context.Background(), RunRequest{Runtime: "runner", Argv: []string{missing}}, Dependencies{
		Stdin: strings.NewReader(""), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{},
	})
	if !errors.Is(err, ErrExecutableNotFound) {
		t.Fatalf("Run() error = %v, want missing executable", err)
	}
	if result.Status != StatusNotFound || result.Executable != missing || result.Resolution != ResolutionPath {
		t.Fatalf("result = %#v, want redacted path lookup result", result)
	}
	if strings.Contains(result.Executable, string(filepath.Separator)) || strings.Contains(err.Error(), string(filepath.Separator)) {
		t.Fatalf("result or error exposed a path: result=%#v error=%v", result, err)
	}
}

func TestRunRejectsUnsafeOrOversizedInvocation(t *testing.T) {
	dependencies := Dependencies{Stdin: strings.NewReader(""), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	tooMany := make([]string, maxArgvCount+1)
	for index := range tooMany {
		tooMany[index] = "x"
	}
	tooMany[0] = "echo"
	aggregate := make([]string, 33)
	aggregate[0] = "echo"
	for index := 1; index < len(aggregate); index++ {
		aggregate[index] = strings.Repeat("x", maxArgumentLength)
	}
	for _, request := range []RunRequest{
		{Runtime: "runner", Argv: []string{"echo", "nul\x00argument"}},
		{Runtime: "runner", Argv: []string{"echo"}, Directory: "nul\x00directory"},
		{Runtime: "runner", Argv: []string{strings.Repeat("x", maxArgumentLength+1)}},
		{Runtime: "runner", Argv: tooMany},
		{Runtime: "runner", Argv: aggregate},
	} {
		if _, err := Run(context.Background(), request, dependencies); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("Run(%#v) error = %v, want invalid request", request, err)
		}
	}
}

func TestRunRedactsSecretLikeExecutableName(t *testing.T) {
	result, err := Run(context.Background(), RunRequest{
		Runtime: "runner", Argv: []string{"/private/top-secret-token"},
	}, Dependencies{Stdin: strings.NewReader(""), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if !errors.Is(err, ErrExecutableNotFound) {
		t.Fatalf("Run() error = %v, want missing executable", err)
	}
	if result.Executable != "redacted" || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unsafe result or error: result=%#v error=%v", result, err)
	}
}

func TestRunValidatesRequestAndDependencies(t *testing.T) {
	validDependencies := Dependencies{Stdin: strings.NewReader(""), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	for _, request := range []RunRequest{
		{Runtime: "", Argv: []string{"echo"}},
		{Runtime: "invalid runtime", Argv: []string{"echo"}},
		{Runtime: strings.Repeat("a", maxRuntimeLabelLength+1), Argv: []string{"echo"}},
		{Runtime: "runner", Argv: nil},
		{Runtime: "runner", Argv: []string{""}},
	} {
		if _, err := Run(context.Background(), request, validDependencies); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("Run(%#v) error = %v, want invalid request", request, err)
		}
	}
	if _, err := Run(context.Background(), RunRequest{Runtime: "runner", Argv: []string{"echo"}}, Dependencies{}); !errors.Is(err, ErrInvalidDependencies) {
		t.Fatalf("Run() error = %v, want invalid dependencies", err)
	}
}

func TestRuntimeHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_RUNTIME_HELPER") != "1" {
		return
	}
	args := os.Args[1:]
	mode := ""
	for _, arg := range args {
		if strings.HasPrefix(arg, "mode=") {
			mode = strings.TrimPrefix(arg, "mode=")
			break
		}
	}
	switch mode {
	case "stdio":
		input, _ := io.ReadAll(os.Stdin)
		_, _ = os.Stdout.WriteString("out:" + string(input))
		_, _ = os.Stderr.WriteString("err:" + string(input))
		os.Exit(7)
	case "wait":
		time.Sleep(5 * time.Second)
		os.Exit(0)
	case "echo":
		_, _ = os.Stdout.WriteString(strings.Join(args, "\x1f"))
		os.Exit(0)
	default:
		os.Exit(2)
	}
}
