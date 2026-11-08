// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build !darwin && !windows

package opener

import (
	"time"

	"github.com/altessa-s/rmctl/internal/port"
)

// grace is how long xdg-open(1) is waited for. Most desktops return at
// once; some run the viewer in its place, which is left running after it.
const grace = time.Second

// New is the system's opener: xdg-open(1).
func New() port.Opener { return Command{Name: "xdg-open", Grace: grace} }
