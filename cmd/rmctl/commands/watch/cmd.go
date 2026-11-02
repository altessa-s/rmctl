// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package watch

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/go-atlas-core/time/clock"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/output"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// New returns the watch command.
func New(env *cliutil.Env) *cobra.Command { return newWatch(env, execRunner{}) }

// execFlags are the flags of --exec.
type execFlags struct {
	program  string
	shell    bool
	debounce time.Duration
	timeout  time.Duration
	only     string
	under    string
}

func newWatch(env *cliutil.Env, run runner) *cobra.Command {
	var raw bool
	var x execFlags
	cmd := &cobra.Command{
		Use:     "watch [--exec PROGRAM [-- ARG...]]",
		GroupID: cliutil.GroupRead,
		Short:   "Follow the library's notifications live",
		Long: heredoc.Doc(`
			Connects to the notification socket and prints each event as it arrives,
			reconnecting when the socket drops, until Ctrl-C. Connections and drops
			are noted on standard error; events go to standard output.

			Document names are masked as <name:N>. --raw prints every frame whole
			instead: the wrapper, the messageid, and any field nobody has a name
			for yet, with names unmasked, so its output is not for a report. A
			frame that does not parse is printed whole either way.

			--exec runs a program on the library's events. Events that arrive within
			--debounce of the first one of a batch make one run. The program is run
			as it is given, with the arguments after --, and no shell, unless --shell
			hands --exec to sh -c (then the arguments after -- are $0, $1, ...). The
			events reach it only through its environment — RMCTL_EVENT, RMCTL_ID,
			RMCTL_NAME, RMCTL_PATH, RMCTL_KIND, RMCTL_THIS_DEVICE and RMCTL_TIME of
			the last event, RMCTL_COUNT, RMCTL_DROPPED, RMCTL_PROFILE — and as JSON
			on its standard input ({"profile": ..., "dropped": N, "events": [...]});
			no name is ever put into its command line. Each variable replaces one of
			the same name in rmctl's environment. A batch holds at most 1000 events:
			past that the oldest are dropped, counted in RMCTL_DROPPED and "dropped",
			and noted on standard error, so a program that sees a non-zero count
			knows it missed events and can rescan the library. Runs happen one at a
			time; each may take --exec-timeout,
			and its output goes to standard error. A run that fails is noted with
			its exit status, and watch goes on. --only and --under keep the events
			of documents or folders, or below a folder. At Ctrl-C a run under way
			has a few seconds to finish.
		`),
		Example: heredoc.Doc(`
			rmctl watch
			rmctl watch --raw --json
			rmctl watch --exec ./sync-notes.sh --only document --under /Notes
			rmctl watch --exec notify-send -- "reMarkable" "the library changed"
			rmctl watch --shell --exec 'jq -r ".events[].path" >> ~/changes.log'
		`),
		Args:         execArgs(&x),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, exec, err := x.options(cmd, args)
			if err != nil {
				return err
			}
			w, err := cliutil.Need(cmd, env, func(s cliutil.Services) port.Watcher { return s.Watcher })
			if err != nil {
				return err
			}
			// The notes, the runs' output and the runs' outcomes reach the
			// error stream from several goroutines.
			errOut := &lockedWriter{w: cmd.ErrOrStderr()}
			p := printer{em: env.Stream(cmd, columns(raw)), errOut: errOut, raw: raw, now: clock.Real().Now}
			on := p.print
			var d *dispatcher
			if exec {
				d = newDispatcher(opts, run, libraryTree(cmd, env), errOut, clock.Real().Now)
				go d.work(cmd.Context())
				on = func(e port.Event) {
					p.print(e)
					d.add(e)
				}
			}
			err = w.Watch(cmd.Context(), on)
			if d != nil {
				d.close()
				<-d.done
			}
			if endErr := p.em.End(nil); err == nil {
				err = endErr
			}
			// Ctrl-C ends watch as an interruption, whether or not the
			// socket's reader said so.
			if cmd.Context().Err() != nil {
				return context.Cause(cmd.Context())
			}
			if err != nil {
				return fmt.Errorf("watch: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&raw, "raw", false, "print every frame whole, names unmasked")
	cmd.Flags().StringVar(&x.program, "exec", "", "run this program on the events (its arguments after --)")
	cmd.Flags().BoolVar(&x.shell, "shell", false, "run --exec with sh -c")
	cmd.Flags().DurationVar(&x.debounce, "debounce", DefaultDebounce, "gather the events of this long into one run")
	cmd.Flags().DurationVar(&x.timeout, "exec-timeout", DefaultExecTimeout, "stop a run that takes longer")
	cmd.Flags().StringVar(&x.only, "only", "", "run only for a document or a folder")
	cmd.Flags().StringVar(&x.under, "under", "", "run only for items below this folder")
	return cmd
}

// execArgs allows arguments only after --, and only for --exec.
func execArgs(x *execFlags) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		dash := cmd.ArgsLenAtDash()
		switch {
		case len(args) == 0:
			return nil
		case dash != 0:
			return cliutil.Usagef("%s: the program's arguments follow --", cmd.CommandPath())
		case x.program == "":
			return cliutil.Usagef("%s: arguments after -- are for --exec", cmd.CommandPath())
		}
		return nil
	}
}

// options checks the --exec flags; ok false is no --exec.
func (x execFlags) options(cmd *cobra.Command, args []string) (opts execOptions, ok bool, err error) {
	path := cmd.CommandPath()
	if x.program == "" {
		for _, name := range []string{"shell", "debounce", "exec-timeout", "only", "under"} {
			if cmd.Flags().Changed(name) {
				return execOptions{}, false, cliutil.Usagef("%s: --%s goes with --exec", path, name)
			}
		}
		return execOptions{}, false, nil
	}
	switch {
	case x.debounce < 0:
		return execOptions{}, false, cliutil.Usagef("%s: --debounce is a duration of zero or more", path)
	case x.timeout <= 0:
		return execOptions{}, false, cliutil.Usagef("%s: --exec-timeout is a positive duration", path)
	case x.only != "" && x.only != onlyDocument && x.only != onlyFolder:
		return execOptions{}, false, cliutil.Usagef("%s: --only is document or folder", path)
	case x.under != "" && !strings.HasPrefix(x.under, "/"):
		return execOptions{}, false, cliutil.Usagef("%s: --under is a folder path from the top, such as /Notes", path)
	}
	argv := append([]string{x.program}, args...)
	if x.shell {
		argv = append([]string{"sh", "-c", x.program, "rmctl-watch"}, args...)
	}
	profile := cliutil.ProfileName(cmd)
	return execOptions{
		argv: argv, debounce: x.debounce, timeout: x.timeout, grace: execGrace,
		only: x.only, under: x.under, profile: profile,
	}, true, nil
}

// libraryTree reads the library for the paths and kinds of a run's events:
// one request with the cache, none within its TTL.
func libraryTree(cmd *cobra.Command, env *cliutil.Env) func(context.Context) (*domain.Tree, error) {
	return func(ctx context.Context) (*domain.Tree, error) {
		b, err := cliutil.Need(cmd, env, func(s cliutil.Services) port.Browser { return s.Browser })
		if err != nil {
			return nil, err
		}
		return b.Tree(ctx)
	}
}

// eventView is an event as --json prints it.
type eventView struct {
	Time           string `json:"time"`
	Name           string `json:"name"`
	SyncComplete   bool   `json:"sync_complete"`
	DocumentID     string `json:"document_id,omitempty"`
	VisibleName    string `json:"visible_name,omitempty"`
	FromThisDevice bool   `json:"from_this_device"`
}

// columns are watch's columns: an event's time, name, document and origin,
// and the frame when it is printed whole; --raw prints frames only.
func columns(raw bool) []output.Column {
	timeCol := output.Column{Title: "time", Role: output.RoleTime}
	event := output.Column{Title: "event", Role: output.RoleKind}
	frame := output.Column{Title: "frame"}
	if raw {
		return []output.Column{timeCol, event, frame}
	}
	return []output.Column{
		timeCol, event,
		{Title: "id", Role: output.RoleID},
		{Title: "name", Role: output.RoleName},
		{Title: "this_device", Role: output.RoleKind},
		frame,
	}
}

type printer struct {
	em     output.Emitter
	errOut io.Writer
	raw    bool
	now    func() time.Time
}

func (p printer) print(e port.Event) {
	switch e.Kind {
	case port.EventConnected:
		cliutil.Notef(p.errOut, "connected")
		return
	case port.EventDropped:
		if e.Err != nil {
			cliutil.Notef(p.errOut, "dropped: %v — reconnecting", e.Err)
		} else {
			cliutil.Notef(p.errOut, "dropped — reconnecting")
		}
		return
	case port.EventUnreadable:
		// There is no summary to fall back on, and an unknown shape is the
		// most interesting thing this channel delivers: print it whole.
		p.frame("unreadable frame", e.Frame)
		return
	case port.EventLibrary:
		if p.raw {
			p.frame(e.Name, e.Frame)
			return
		}
	default:
		return
	}
	now := p.now().UTC()
	v := eventView{
		Time: now.Format(time.RFC3339), Name: e.Name, SyncComplete: e.SyncComplete,
		DocumentID: string(e.DocumentID), VisibleName: cliutil.MaskName(e.VisibleName), FromThisDevice: e.FromThisDevice,
	}
	_ = p.em.Row(output.Row{JSON: v, Cells: []output.Cell{ //nolint:errcheck // a closed pipe ends the next write too
		output.Time(now), output.Text(v.Name), output.ID(v.DocumentID), output.Text(v.VisibleName), output.Bool(v.FromThisDevice), output.Text(""),
	}})
}

// frameView is a frame printed whole, for --raw and for a frame that did not
// parse.
type frameView struct {
	Time  string `json:"time"`
	Event string `json:"event"`
	Frame string `json:"frame"`
}

// frame prints a socket frame as it arrived. Names inside it are not masked.
func (p printer) frame(label string, data []byte) {
	now := p.now().UTC()
	v := frameView{Time: now.Format(time.RFC3339), Event: label, Frame: string(data)}
	cells := []output.Cell{output.Time(now), output.Text(label), output.Text(v.Frame)}
	if !p.raw {
		cells = []output.Cell{output.Time(now), output.Text(label), output.ID(""), output.Text(""), output.Text(""), output.Text(v.Frame)}
	}
	_ = p.em.Row(output.Row{JSON: v, Cells: cells}) //nolint:errcheck // a closed pipe ends the next write too
}
