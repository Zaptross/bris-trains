// Command trains shows the next Brisbane trains between two stations, and
// serves the same data to the web UI.
//
//	trains <origin> <destination> [query flags]
//	trains serve [serve flags]
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"trains/cmd/trains/query"
	"trains/cmd/trains/serve"
	"trains/internal/version"
)

func main() { os.Exit(run(os.Args[1:])) }

// run dispatches on the first argument: an exact "serve" selects serve mode,
// anything else is a query (SPEC.md §4.1).
func run(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(args) == 0 {
		fmt.Fprint(os.Stderr, query.Usage)
		return 2
	}
	if args[0] == "--version" || args[0] == "-version" {
		fmt.Println("trains", version.String())
		return 0
	}
	if args[0] == "serve" {
		return serve.Run(ctx, args[1:], os.Stdout, os.Stderr, query.FlagNames())
	}
	return query.Run(ctx, args, os.Stdout, os.Stderr, serve.FlagNames())
}
