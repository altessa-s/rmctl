// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package opener

import (
	"time"

	"github.com/altessa-s/rmctl/internal/port"
)

// grace is how long open(1) is waited for: it returns once Launch Services
// has handed the file to its application, and fails when none opens it.
const grace = 10 * time.Second

// New is the system's opener: open(1).
func New() port.Opener { return Command{Name: "open", Grace: grace} }
