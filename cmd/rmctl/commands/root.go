// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package commands

import (
	"context"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/go-atlas-core/runtime/appinfo"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/account"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/browse"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/debug"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/fetch"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/rmdoc"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/version"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/watch"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/write"
)

type (
	// Services are the use cases the commands drive.
	Services = cliutil.Services
	// Streams are the standard streams the commands use.
	Streams = cliutil.Streams
	// Factory builds the services once the flags are parsed.
	Factory = cliutil.Factory
	// ExitError carries the exit code of an error.
	ExitError = cliutil.ExitError
)

// ExitCode is the process exit code for err: 0 ok, 1 error, 2 usage,
// 3 not registered, 4 root conflict, 130 interrupted.
func ExitCode(err error) int { return cliutil.ExitCode(err) }

// helpGroups are the help groups, in the order help prints them.
func helpGroups() []*cobra.Group {
	return []*cobra.Group{
		{ID: account.GroupID, Title: "Account:"},
		{ID: browse.GroupID, Title: "Reading — these change nothing:"},
		{ID: write.GroupID, Title: "Writing — these change the live library and ask first:"},
		{ID: debug.GroupID, Title: "Diagnostics:"},
	}
}

// New builds the command tree over fixed services.
func New(svc Services, streams Streams) *cobra.Command {
	cmd, _ := newRoot(cliutil.NewEnv(streams, cliutil.Fixed(svc)))
	return cmd
}

// newRoot builds the command tree over env.
func newRoot(env *cliutil.Env) (*cobra.Command, *cliutil.Env) {
	root := &cobra.Command{
		Use:   "rmctl",
		Short: "Work with a reMarkable cloud library from the shell",
		Long: heredoc.Doc(`
			rmctl reads and changes a reMarkable cloud library: list it, find in it,
			download and upload documents, create folders, move, trash and restore,
			and follow its changes live.

			Items are named by path ("/Books/Novel") or by identifier, whole or a
			unique prefix. Answers go to standard output and commentary to standard
			error, so a pipe gets the answer. On a terminal an answer is a table; in a
			pipe it is plain lines of TAB-separated fields; --format table, plain or
			json (or RMCTL_FORMAT) chooses, and --json is --format json. Writing
			commands ask before they change anything; --yes answers for them.
		`),
		Example: heredoc.Doc(`
			# Register this machine with a one-time code from
			# https://my.remarkable.com/device/desktop/connect
			rmctl register abcdefgh

			# The library as a tree, and one folder in detail
			rmctl tree
			rmctl ls -l /Books

			# Download a document and upload one
			rmctl get /Books/Novel -o novel.pdf
			rmctl put paper.pdf /Papers --yes

			# Everything as JSON for a script
			rmctl ls --json /Papers
		`),
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       appinfo.Version,
	}
	root.SetIn(env.Streams.In)
	root.SetOut(env.Streams.Out)
	root.SetErr(env.Streams.Err)
	root.SuggestionsMinimumDistance = 1
	root.SetFlagErrorFunc(cliutil.FlagError)
	root.SetVersionTemplate(heredoc.Doc(`
		{{with .Name}}{{printf "%s " .}}{{end}}{{printf "version %s" .Version}}
	`))

	cliutil.AddPersistentFlags(root, env)

	root.AddGroup(helpGroups()...)
	root.AddCommand(account.Commands(env)...)
	root.AddCommand(browse.Commands(env)...)
	root.AddCommand(fetch.Commands(env)...)
	root.AddCommand(write.Commands(env)...)
	root.AddCommand(watch.New(env))
	root.AddCommand(rmdoc.New(env))
	root.AddCommand(debug.New(env))
	root.AddCommand(version.New(env))
	root.SetHelpCommandGroupID(account.GroupID)
	root.SetCompletionCommandGroupID(account.GroupID)
	return root, env
}

// Run builds the command tree with services from factory, runs args, and
// releases the services. An error caused by Ctrl-C comes back as
// [cliutil.ErrInterrupted] when ctx was canceled for that reason.
func Run(ctx context.Context, args []string, streams Streams, factory Factory) (err error) {
	// The context reaches every command through ExecuteContext below.
	root, env := newRoot(cliutil.NewEnv(streams, factory)) //nolint:contextcheck // see above
	defer func() {
		if closeErr := env.Close(); err == nil {
			err = closeErr
		}
	}()
	root.SetArgs(args)
	return cliutil.Interrupted(ctx, root.ExecuteContext(ctx))
}
