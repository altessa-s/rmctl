// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package mirror

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/output"
	"github.com/altessa-s/rmctl/internal/port"
)

// GroupID is the help group of mirror: it changes the library.
const GroupID = "write"

// The keys of the answers that name a mirror and its folder, and the flag
// of an ad-hoc mirror's folder.
const (
	keyName   = "name"
	keyFolder = "folder"
)

// New returns the mirror command and its subcommands.
func New(env *cliutil.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "mirror",
		GroupID: GroupID,
		Short:   "Keep local folders and cloud folders in step, both ways",
		Long: heredoc.Doc(`
			A mirror keeps a local directory and a cloud folder in step, both ways.
			New PDFs and EPUBs in the directory are uploaded into the folder, a PDF
			changed locally replaces the document's PDF and keeps its annotations,
			renames and moves follow on either side, and every document annotated on
			the tablet is drawn into _annotated/ in the directory, at the same path,
			as a PDF (as an .rmdoc in rmctl-lite). A file deleted locally puts its
			document in the trash; a document deleted on the tablet moves its file
			into .rmctl-removed/. Nothing is deleted outright, and a guard refuses a
			mass of deletions on one side unless --force-delete.

			mirror add defines a mirror in config.toml, mirror run makes a pass (or,
			with --watch, keeps it in step until Ctrl-C), and mirror service install
			starts every mirror with the session, on macOS, Linux and Windows.
		`),
		Example: heredoc.Doc(`
			rmctl mirror add papers ~/Papers /Papers
			rmctl mirror run papers --dry-run
			rmctl mirror run --all --watch
			rmctl mirror service install
		`),
	}
	cmd.AddCommand(
		cliutil.WithAccess(newAdd(env), cliutil.AccessOffline),
		cliutil.WithAccess(newLs(env), cliutil.AccessOffline),
		cliutil.WithAccess(newRm(env), cliutil.AccessOffline),
		newRun(env),
		newService(env),
	)
	return cmd
}

// mirrors is the mirrors use case.
func mirrors(cmd *cobra.Command, env *cliutil.Env) (port.Mirrors, error) {
	return cliutil.Need(cmd, env, func(s cliutil.Services) port.Mirrors { return s.Mirrors })
}

// specView is a mirror's definition as --json prints it.
type specView struct {
	Name     string `json:"name"`
	Dir      string `json:"dir"`
	Folder   string `json:"folder"`
	Profile  string `json:"profile"`
	PullNew  bool   `json:"pull_new"`
	NoDelete bool   `json:"no_delete"`
}

func viewOf(s port.MirrorSpec) specView {
	return specView{Name: s.Name, Dir: s.Dir, Folder: s.Folder, Profile: s.Profile, PullNew: s.PullNew, NoDelete: s.NoDelete}
}

func specAnswer(s port.MirrorSpec) output.Result {
	return output.Fields(viewOf(s),
		output.Field{Key: keyName, Value: output.Text(s.Name)},
		output.Field{Key: "dir", Value: output.Path(s.Dir)},
		output.Field{Key: keyFolder, Value: output.Path(s.Folder)},
		output.Field{Key: "profile", Value: output.Text(profileOf(s))},
		output.Field{Key: "pull_new", Value: output.Bool(s.PullNew)},
		output.Field{Key: "no_delete", Value: output.Bool(s.NoDelete)})
}

// profileOf names the profile of a mirror for a person: its own, or the
// run's when it names none.
func profileOf(s port.MirrorSpec) string {
	if s.Profile == "" {
		return "(the run's)"
	}
	return s.Profile
}

func newAdd(env *cliutil.Env) *cobra.Command {
	var spec port.MirrorSpec
	cmd := &cobra.Command{
		Use:   "add <name> <dir> <cloud-folder>",
		Short: "Define a mirror in config.toml",
		Long: heredoc.Doc(`
			Appends a [[mirror]] table to config.toml: the local directory, which
			must exist, kept in step with the cloud folder, created at the first pass
			when missing. --profile (or RMCTL_PROFILE) given here is the profile the
			mirror always uses; without it, the mirror uses the profile of the run.
			A name is 1-64 characters of a-z, 0-9, _ and -. A mirror whose directory
			contains this one or lies inside it, or of the same profile whose folder
			does, is refused. Nothing is mirrored yet: mirror run does it.

			--pull-new draws the documents created on the tablet in the folder into
			_annotated/ too. --no-delete carries no deletion from one side to the
			other: the document or the file deleted on one side stays on the other,
			and the mirror stops following it.
		`),
		Example: heredoc.Doc(`
			rmctl mirror add papers ~/Papers /Papers
			rmctl -P work mirror add contracts ~/Work/Contracts /Clients/Contracts --pull-new
		`),
		Args:         cliutil.Args(cobra.ExactArgs(3)), //nolint:mnd // name, dir and folder
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := mirrors(cmd, env)
			if err != nil {
				return err
			}
			dir, err := filepath.Abs(args[1])
			if err != nil {
				return err
			}
			spec.Name, spec.Dir, spec.Folder, spec.Profile = args[0], dir, args[2], cliutil.Profile(cmd)
			added, err := m.Add(cmd.Context(), spec)
			if err != nil {
				return addErr(err)
			}
			cliutil.Notef(cmd.ErrOrStderr(), "rmctl mirror run %s makes a pass (--dry-run first shows it); "+
				"rmctl mirror service install keeps every mirror in step", added.Name)
			return env.Emit(cmd, specAnswer(added))
		},
	}
	cmd.Flags().BoolVar(&spec.PullNew, "pull-new", false, "draw the documents created on the tablet into _annotated/ too")
	cmd.Flags().BoolVar(&spec.NoDelete, "no-delete", false, "carry no deletion from one side to the other")
	return cmd
}

// addErr makes a definition refused a usage error.
func addErr(err error) error {
	if errors.Is(err, port.ErrMirrorSpec) || errors.Is(err, port.ErrMirrorExists) {
		return cliutil.Usage(fmt.Errorf("mirror add: %w", err))
	}
	return fmt.Errorf("mirror add: %w", err)
}

// stateView is a mirror and its state as --json prints it.
type stateView struct {
	specView
	Tracked      int    `json:"tracked"`
	LastPass     string `json:"last_pass"`
	LastError    string `json:"last_error"`
	Pending      int    `json:"pending"`
	LocalChanges int    `json:"local_changes"`
	Error        string `json:"error"`
}

func newLs(env *cliutil.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List the mirrors and how each stands",
		Long: heredoc.Doc(`
			Lists the mirrors of config.toml with what each directory's journal says:
			the documents in step, when the last pass ended, what it left pending
			(files still being written, deletions refused, steps that failed) and its
			error, and the files added, changed or deleted locally since, by their
			size and time. It reads only this machine.
		`),
		Args:         cliutil.Args(cobra.NoArgs),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			m, err := mirrors(cmd, env)
			if err != nil {
				return err
			}
			states, err := m.List(cmd.Context())
			if err != nil {
				return fmt.Errorf("mirror ls: %w", err)
			}
			return env.Emit(cmd, lsAnswer(states))
		},
	}
}

func lsAnswer(states []port.MirrorState) output.Result {
	t := output.Table{Columns: []output.Column{
		{Title: "name", Role: output.RoleName},
		{Title: "dir", Role: output.RolePath},
		{Title: "folder", Role: output.RolePath},
		{Title: "profile"},
		{Title: "tracked", Role: output.RoleCount},
		{Title: "last pass", Role: output.RoleTime},
		{Title: "pending", Role: output.RoleCount},
		{Title: "local changes", Role: output.RoleCount},
		{Title: "state"},
	}}
	views := make([]stateView, 0, len(states))
	for _, st := range states {
		v := stateView{
			specView: viewOf(st.Spec), Tracked: st.Tracked, LastPass: cliutil.Stamp(st.LastPass),
			LastError: st.LastError, Pending: st.Pending, LocalChanges: st.LocalChanges,
		}
		state := "ok"
		switch {
		case st.Err != nil:
			v.Error, state = st.Err.Error(), st.Err.Error()
		case st.LastPass.IsZero():
			state = "never run"
		case st.LastError != "":
			state = st.LastError
		}
		views = append(views, v)
		t.Rows = append(t.Rows, []output.Cell{
			output.Text(st.Spec.Name), output.Path(st.Spec.Dir), output.Path(st.Spec.Folder), output.Text(profileOf(st.Spec)),
			output.Count(st.Tracked), output.Time(st.LastPass), output.Count(st.Pending), output.Count(st.LocalChanges), output.Text(state),
		})
	}
	return output.Result{JSON: views, Doc: output.Doc{t}}
}

func newRm(env *cliutil.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "rm <name>",
		Short: "Forget a mirror's definition, touching no file",
		Long: heredoc.Doc(`
			Takes the mirror's [[mirror]] table out of config.toml, and leaves every
			other line. No file is deleted or moved, in the directory or in the cloud:
			the directory keeps its journal, so a mirror defined again on it picks up
			where this one left off.
		`),
		Args:         cliutil.Args(cobra.ExactArgs(1)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := mirrors(cmd, env)
			if err != nil {
				return err
			}
			spec, err := m.Remove(cmd.Context(), args[0])
			if errors.Is(err, port.ErrNoMirror) {
				return cliutil.Usage(fmt.Errorf("mirror rm: %w", err))
			}
			if err != nil {
				return fmt.Errorf("mirror rm: %w", err)
			}
			cliutil.Notef(cmd.ErrOrStderr(), "nothing was deleted: %s and the cloud folder %s stay as they are", spec.Dir, spec.Folder)
			return env.Emit(cmd, specAnswer(spec))
		},
	}
}
