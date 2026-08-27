// Command vat is the workspace control plane.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/takealook97/vat/internal/cli"
)

func main() {
	// The work runs in its own function so the signal handler is torn down
	// before os.Exit, which does not run deferred calls.
	os.Exit(run())
}

// run is separate from main so it can be called in a test: main's whole body is
// os.Exit, which a test cannot survive.
func run() int {
	// A cancelled context lets in-flight git and check commands stop cleanly
	// rather than leaving a half-fetched repository behind.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return cli.Execute(ctx, os.Args[1:])
}
