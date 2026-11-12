// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package mirror

import (
	"context"
	"io"
	"maps"
	"time"

	"github.com/altessa-s/rmctl/internal/port"
)

// ServiceArgs are the arguments the service runs rmctl with: every mirror,
// watched, logging to the service's log.
var ServiceArgs = []string{"mirror", "run", "--service"} //nolint:gochecknoglobals // a fixed command line

// FollowPoll is how often a follow of the log looks for new lines.
const FollowPoll = time.Second

// Autostart implements port.MirrorService over the system's service
// manager.
type Autostart struct {
	auto    port.Autostart
	program string
	env     map[string]string
	timer   port.Timer
}

var _ port.MirrorService = (*Autostart)(nil)

// NewAutostart returns the service use case: program is rmctl, an absolute
// path, and env the variables the service keeps from this run (where the
// configuration and the cache are, which hosts). A nil dependency panics.
func NewAutostart(auto port.Autostart, program string, env map[string]string, timer port.Timer) *Autostart {
	need(dependency{"Autostart", auto == nil}, dependency{depTimer, timer == nil})
	return &Autostart{auto: auto, program: program, env: maps.Clone(env), timer: timer}
}

// Install implements port.MirrorService: installing again replaces the
// definition, so a new rmctl or new settings take effect.
func (a *Autostart) Install(ctx context.Context) (port.ServiceInfo, error) {
	return a.auto.Install(ctx, port.ServiceSpec{Program: a.program, Args: ServiceArgs, Env: maps.Clone(a.env)})
}

// Uninstall implements port.MirrorService.
func (a *Autostart) Uninstall(ctx context.Context) (port.ServiceInfo, error) {
	return a.auto.Uninstall(ctx)
}

// Status implements port.MirrorService.
func (a *Autostart) Status(ctx context.Context) (port.ServiceInfo, error) {
	return a.auto.Status(ctx)
}

// Log implements port.MirrorService.
func (a *Autostart) Log(ctx context.Context, lines int, follow bool, on func(string)) error {
	tail, end, err := a.auto.ReadLog(lines)
	if err != nil {
		return err
	}
	for _, l := range tail {
		on(l)
	}
	for follow && a.timer.Sleep(ctx, FollowPoll) == nil {
		more, next, rerr := a.auto.ReadLogFrom(end)
		if rerr != nil {
			return rerr
		}
		end = next
		for _, l := range more {
			on(l)
		}
	}
	return nil
}

// OpenLog implements port.MirrorService.
func (a *Autostart) OpenLog() (io.WriteCloser, error) { return a.auto.OpenLog() }
