// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package opener

import (
	"context"
	"fmt"
	"os/exec"
	"slices"
	"time"

	"github.com/altessa-s/rmctl/internal/port"
)

// Command opens a file by running a program with the file after Args: the
// system's opener, which starts the viewer and, for most, returns at once.
// It waits for the program up to Grace, so one that fails at once (no
// viewer for the file, no display) is an error; a program still running
// then is left running, detached from rmctl, and the open succeeds.
type Command struct {
	Name  string
	Args  []string
	Grace time.Duration
}

var _ port.Opener = Command{}

// Open implements port.Opener. Canceling ctx stops the wait, never the
// program.
func (c Command) Open(ctx context.Context, path string) error {
	args := append(slices.Clone(c.Args), path)
	cmd := exec.CommandContext(context.WithoutCancel(ctx), c.Name, args...) // #nosec G204 -- the system's opener, given a file as one argument, no shell
	cmd.SysProcAttr = detached()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open %s with %s: %w", path, c.Name, err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(c.Grace)
	defer timer.Stop()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("open %s with %s: %w", path, c.Name, err)
		}
		return nil
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
