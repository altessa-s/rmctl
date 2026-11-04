// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package blobcache

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
)

// ErrMismatch means a blob's bytes are not those of its hash.
var ErrMismatch = errors.New("blobcache: the file does not match its hash")

// ErrTooLarge means a blob is larger than the cache's whole budget.
var ErrTooLarge = errors.New("blobcache: the file is larger than the cache's budget")

// Size is what the entry key of space takes, if there is one. A hit counts
// as a use for eviction; the bytes are not read.
func (c *Cache) Size(space, key string) (int64, bool) {
	if !validKey(space, key) {
		return 0, false
	}
	path := filepath.Join(c.dir, space, key)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return 0, false
	}
	now := c.clock.Now()
	_ = os.Chtimes(path, now, now)
	return info.Size(), true
}

// Fill keeps the blob of hash that write produces. The bytes stream to a
// staging file, are checked against hash, and are renamed into place only
// when whole and right; then the cache is evicted down to its budget.
// Unlike the reads and writes of a command's cache, a fill reports what
// fails: it is what the command asked for.
func (c *Cache) Fill(hash string, write func(w io.Writer) error) error {
	if !validHash(hash) {
		return fmt.Errorf("blobcache: %q is not a sha256 hash", hash)
	}
	dir := filepath.Join(c.dir, SpaceBlobs)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+hash+".*.part")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	sum := sha256.New()
	counter := &counting{w: io.MultiWriter(tmp, sum)}
	werr := write(counter)
	if err = errors.Join(werr, tmp.Chmod(fileMode), tmp.Close()); err != nil {
		return err
	}
	switch {
	case hex.EncodeToString(sum.Sum(nil)) != hash:
		return ErrMismatch
	case counter.n > c.maxBytes:
		return ErrTooLarge
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	path := filepath.Join(dir, hash)
	if err = os.Rename(name, path); err != nil {
		return err
	}
	now := c.clock.Now()
	_ = os.Chtimes(path, now, now)
	c.account(counter.n)
	return nil
}

type counting struct {
	w io.Writer
	n int64
}

func (c *counting) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// Open is the blob of hash for reading, with its size, checked against
// the hash as it is read: a reader whose bytes turn out not to be the
// hash's fails at the end with ErrMismatch, and the entry is dropped. A
// hit counts as a use for eviction.
func (c *Cache) Open(hash string) (io.ReadCloser, int64, bool) {
	if !validHash(hash) {
		return nil, 0, false
	}
	root, err := os.OpenRoot(filepath.Join(c.dir, SpaceBlobs))
	if err != nil {
		return nil, 0, false
	}
	defer func() { _ = root.Close() }()
	f, err := root.Open(hash)
	if err != nil {
		return nil, 0, false
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, 0, false
	}
	now := c.clock.Now()
	_ = root.Chtimes(hash, now, now)
	return &checked{f: f, sum: sha256.New(), hash: hash, drop: func() { c.Drop(SpaceBlobs, hash) }}, info.Size(), true
}

// checked reads a blob, hashing it, and fails at its end if the hash is
// not the one it was opened by.
type checked struct {
	f    *os.File
	sum  hash.Hash
	hash string
	drop func()
}

func (r *checked) Read(p []byte) (int, error) {
	n, err := r.f.Read(p)
	r.sum.Write(p[:n])
	if errors.Is(err, io.EOF) && hex.EncodeToString(r.sum.Sum(nil)) != r.hash {
		r.drop()
		return n, ErrMismatch
	}
	return n, err
}

func (r *checked) Close() error { return r.f.Close() }
