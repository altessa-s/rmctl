// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package opener

import (
	"context"
	"fmt"

	"github.com/altessa-s/rmctl/internal/port"

	"golang.org/x/sys/windows"
)

// New is the system's opener: the shell's "open" verb, through
// ShellExecute, which returns once the viewer is started.
func New() port.Opener { return shell{} }

// shell opens a file through ShellExecute. Unlike "cmd /c start", it
// takes the path as it is: no command line, so no quoting of &, ^ or %.
type shell struct{}

// Open implements port.Opener.
func (shell) Open(_ context.Context, path string) error {
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	if err = windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL); err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	return nil
}
