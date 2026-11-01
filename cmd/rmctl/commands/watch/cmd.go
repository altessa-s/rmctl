// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package watch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/go-atlas-core/time/clock"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/port"
)

// New returns the watch command.
func New(env *cliutil.Env) *cobra.Command {
	var raw bool
	cmd := &cobra.Command{
		Use:     "watch",
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
		`),
		Example: heredoc.Doc(`
			rmctl watch
			rmctl watch --raw --json
		`),
		Args:         cliutil.Args(cobra.NoArgs),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w, err := cliutil.Need(cmd, env, func(s cliutil.Services) port.Watcher { return s.Watcher })
			if err != nil {
				return err
			}
			p := printer{out: cmd.OutOrStdout(), errOut: cmd.ErrOrStderr(), raw: raw, asJSON: cliutil.JSON(cmd), now: clock.Real().Now}
			err = w.Watch(cmd.Context(), p.print)
			if errors.Is(err, context.Canceled) && cmd.Context().Err() != nil {
				return context.Cause(cmd.Context())
			}
			if err != nil {
				return fmt.Errorf("watch: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&raw, "raw", false, "print every frame whole, names unmasked")
	return cmd
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

type printer struct {
	out, errOut io.Writer
	raw, asJSON bool
	now         func() time.Time
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
	name := cliutil.MaskName(e.VisibleName)
	v := eventView{
		Time: p.now().UTC().Format(time.RFC3339), Name: e.Name, SyncComplete: e.SyncComplete,
		DocumentID: string(e.DocumentID), VisibleName: name, FromThisDevice: e.FromThisDevice,
	}
	if p.asJSON {
		_ = cliutil.WriteJSON(p.out, v) //nolint:errcheck // a closed pipe ends the next write too
		return
	}
	origin := ""
	if v.FromThisDevice {
		origin = " (this device)"
	}
	_, _ = fmt.Fprintf(p.out, "%s %s %s %s%s\n", v.Time, v.Name, v.DocumentID, v.VisibleName, origin) //nolint:errcheck // see above
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
	v := frameView{Time: p.now().UTC().Format(time.RFC3339), Event: label, Frame: string(data)}
	if p.asJSON {
		_ = cliutil.WriteJSON(p.out, v) //nolint:errcheck // a closed pipe ends the next write too
		return
	}
	_, _ = fmt.Fprintf(p.out, "%s %s %s\n", v.Time, v.Event, v.Frame) //nolint:errcheck // see above
}
