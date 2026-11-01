// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// rmctl works with a reMarkable cloud library from the shell: list it, find
// in it, download and upload documents, create folders, move, trash and
// restore, and follow its changes live.
//
// main makes Ctrl-C cancel rather than kill, then delegates to
// [commands.Run], which builds the Cobra tree, runs the matched command and
// releases the services. The exit code says how the command ended: 0 ok,
// 1 error, 2 usage, 3 not registered, 4 the library changed during a write,
// 130 interrupted.
//
// Usage:
//
//	rmctl register <code>
//	rmctl tree
//	rmctl get /Books/Novel -o novel.pdf
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"

	"golang.org/x/term"

	rmcloud "github.com/altessa-s/rmcloud-go"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Exit))
}

// run is main with its streams and exit injected, so a test can drive it.
// extra are transport options: a test points the tool at an emulated cloud.
func run(args []string, in *os.File, out, errOut io.Writer, exit func(int), extra ...rmcloud.Option) int {
	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, os.Interrupt)
	defer signal.Stop(sigs)
	ctx, stop := cliutil.Interrupt(context.Background(), sigs, errOut, exit)
	defer stop()

	streams := commands.Streams{
		In:         in,
		Out:        out,
		Err:        errOut,
		IsTerminal: isTerminal(in) && isTerminal(errOut),
	}
	factory := func(ctx context.Context, profile string, verbose bool) (commands.Services, func() error, error) {
		return build(ctx, profile, verbose, errOut, extra...)
	}
	err := commands.Run(ctx, args, streams, factory)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "rmctl: %v\n", err) //nolint:errcheck // the exit code still says it
		if commands.ExitCode(err) == cliutil.CodeUsage {
			_, _ = fmt.Fprintln(errOut, "Run 'rmctl --help' for usage.") //nolint:errcheck // see above
		}
	}
	return commands.ExitCode(err)
}

// isTerminal reports whether v is a terminal.
func isTerminal(v any) bool {
	f, ok := v.(*os.File)
	return ok && term.IsTerminal(int(f.Fd())) //nolint:gosec // a file descriptor fits an int
}
