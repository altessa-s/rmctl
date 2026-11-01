// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package commands defines the Cobra command tree of the rmctl binary.
//
// The tree is assembled in [New] and [Run] and looks like:
//
//	rmctl
//	├── register, logout, account           account
//	├── ls, tree, stat, find                 reading
//	├── get, export, mget                    reading (downloads)
//	├── mkdir, put, mv, trash, restore, rm,  writing (asks first, or --yes)
//	│   mput
//	├── watch                                live notifications
//	├── debug root|blob|files|hashes|        protocol diagnostics
//	│         checkfiles|sweep
//	├── version
//	└── completion bash|zsh|fish|powershell
//
// The commands drive the input ports of internal/port and nothing else: the
// services arrive through a [Factory] once the flags are parsed, so the
// profile and the verbosity reach the composition root.
package commands
