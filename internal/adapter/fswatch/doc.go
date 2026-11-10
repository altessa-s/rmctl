// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package fswatch implements port.DirWatcher over fsnotify, the one
// package that imports it: a mirror's directory is watched directory by
// directory, so a change is heard at once. Where the system cannot watch —
// a network share, too many directories, events lost — Watch returns an
// error, and the mirror looks at the directory every few seconds instead.
package fswatch
