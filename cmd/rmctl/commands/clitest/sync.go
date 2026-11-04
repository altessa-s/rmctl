// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package clitest

import (
	"context"
	"strconv"

	"github.com/altessa-s/rmctl/internal/port"
)

var _ port.Syncer = (*Fake)(nil)

// Sync implements port.Syncer: it reports SyncSteps and answers Synced.
func (f *Fake) Sync(_ context.Context, query string, o port.SyncOptions) (port.SyncReport, error) {
	if o.Progress != nil {
		for _, p := range f.SyncSteps {
			o.Progress(p)
		}
	}
	args := []string{query}
	for _, a := range []string{opt(o.Files, "files"), opt(o.All, "all"), opt(o.Force, "force"), "jobs=" + strconv.Itoa(o.Jobs)} {
		if a != "" {
			args = append(args, a)
		}
	}
	return f.Synced, f.record("Sync", args...)
}
