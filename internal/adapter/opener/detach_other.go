// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build !unix

package opener

import "syscall"

// detached is nothing to set where the system has no sessions.
func detached() *syscall.SysProcAttr { return nil }
