// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build unix

package opener

import "syscall"

// detached starts the opener in a session of its own: no signal sent to
// rmctl's terminal reaches the viewer.
func detached() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }
