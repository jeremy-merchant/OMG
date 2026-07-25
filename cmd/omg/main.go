// Command omg is the private local coordination-ledger CLI.
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/jeremy-merchant/OMG/internal/bootstrap"
	"github.com/jeremy-merchant/OMG/internal/transport/cli"
)

// version is set with -ldflags for build artifacts. The default deliberately
// identifies an unversioned local build rather than claiming a release.
var version = "devel"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(cli.RunWithApplication(ctx, os.Args[1:], version, os.Stdin, os.Stdout, os.Stderr, bootstrap.CLIService(bootstrap.Foundation())))
}
