package cli

import (
	"context"
	"io"
	"os"

	"example.invalid/coordledger/internal/app/foundation"
	"example.invalid/coordledger/internal/bootstrap"
)

func Run(args []string, version string, output io.Writer) int {
	return RunWithApplication(context.Background(), args, version, os.Stdin, output, os.Stderr, bootstrap.CLIService(bootstrap.Foundation()))
}

func RunWithService(args []string, version string, output io.Writer, service *foundation.Service) int {
	return RunWithApplication(context.Background(), args, version, os.Stdin, output, os.Stderr, bootstrap.CLIService(service))
}

func RunContext(ctx context.Context, args []string, version string, stdin io.Reader, stdout, stderr io.Writer) int {
	return RunWithApplication(ctx, args, version, stdin, stdout, stderr, bootstrap.CLIService(bootstrap.Foundation()))
}
