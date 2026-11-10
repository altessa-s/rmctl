// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package autostart

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// The log of the service.
const (
	// LogName is the log's file name in the log directory.
	LogName = "mirror.log"
	// MaxLogBytes is how large the log grows before it is rotated:
	// mirror.log becomes mirror.log.1, the older ones move up, and
	// KeptLogs of them are kept.
	MaxLogBytes = 5 << 20
	KeptLogs    = 3
	// logMode and logDirMode keep the log to its owner: it names files and
	// documents.
	logMode    fs.FileMode = 0o600
	logDirMode fs.FileMode = 0o700
)

// RotatingLog is a log file that rotates itself as it grows past max
// bytes, keeping keep older files beside it. It is safe for concurrent use.
type RotatingLog struct {
	mu   sync.Mutex
	path string
	max  int64
	keep int
	f    *os.File
	size int64
}

// OpenLog opens the log at path for appending, creating it and its
// directory when missing.
func OpenLog(path string, maxBytes int64, keep int) (*RotatingLog, error) {
	l := &RotatingLog{path: path, max: maxBytes, keep: keep}
	if err := os.MkdirAll(filepath.Dir(path), logDirMode); err != nil {
		return nil, err
	}
	if err := l.open(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *RotatingLog) open() error {
	f, err := os.OpenFile(l.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, logMode) // #nosec G304 -- the service's own log
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	l.f, l.size = f, info.Size()
	return nil
}

// Write appends p, rotating first when p would take the log past its size.
// A write is never split across two files.
func (l *RotatingLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return 0, fs.ErrClosed
	}
	if l.size > 0 && l.size+int64(len(p)) > l.max {
		if err := l.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := l.f.Write(p)
	l.size += int64(n)
	return n, err
}

// rotate moves each kept log up one place, the oldest out, and starts a
// new one.
func (l *RotatingLog) rotate() error {
	if err := l.f.Close(); err != nil {
		return err
	}
	l.f = nil
	_ = os.Remove(fmt.Sprintf("%s.%d", l.path, l.keep))
	for i := l.keep - 1; i >= 1; i-- {
		_ = os.Rename(fmt.Sprintf("%s.%d", l.path, i), fmt.Sprintf("%s.%d", l.path, i+1))
	}
	if err := os.Rename(l.path, l.path+".1"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return l.open()
}

// Close closes the log.
func (l *RotatingLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}

// Tail is the last n lines of the log at path, oldest first, reaching into
// the newest rotated log when the current one holds fewer, and the size of
// the current one. A log not written yet has no lines.
func Tail(path string, n int) ([]string, int64, error) {
	current, err := readFile(path)
	if err != nil {
		return nil, 0, err
	}
	lines := splitLines(current)
	if len(lines) < n {
		older, rerr := readFile(path + ".1")
		if rerr != nil {
			return nil, 0, rerr
		}
		lines = append(splitLines(older), lines...)
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, int64(len(current)), nil
}

// From is the whole lines written to the log at path past offset, and the
// offset after the last of them; a log smaller than offset was rotated
// and is read from its start.
func From(path string, offset int64) ([]string, int64, error) {
	data, err := readFile(path)
	if err != nil {
		return nil, offset, err
	}
	if int64(len(data)) < offset {
		offset = 0
	}
	rest := data[offset:]
	end := bytes.LastIndexByte(rest, '\n')
	if end < 0 {
		return nil, offset, nil
	}
	return splitLines(rest[:end+1]), offset + int64(end) + 1, nil
}

// readFile is the bytes of path, or none for a file not there.
func readFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- the service's own log
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return data, err
}

// splitLines are the lines of data without their line ends.
func splitLines(data []byte) []string {
	text := strings.TrimRight(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

var _ io.WriteCloser = (*RotatingLog)(nil)
