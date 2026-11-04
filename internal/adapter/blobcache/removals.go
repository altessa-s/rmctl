// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package blobcache

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// KeyRemovals is the name, among the state, of the removal generation: a
// value that changes whenever an entry leaves the blobs or the indexes,
// evicted or dropped. A sync records the generation it read before it
// checked that what it kept is all there, and its record vouches for the
// cache only while the generation is still the one it recorded.
const KeyRemovals = "removals"

// NoRemovals is the generation of a cache that never removed an entry:
// its file is missing.
const NoRemovals = "0"

// removalsFormat is a generation on disk: the number of removals counted,
// and a random tag that makes every bump a value never written before.
var removalsFormat = regexp.MustCompile(`^([0-9]{1,19})-[0-9a-f]{16}$`) //nolint:gochecknoglobals // a compiled pattern

// Removals is the removal generation: [NoRemovals] while the cache never
// removed an entry, and a value no other bump wrote after each removal.
// One read of one small file; it counts as no use. A generation that
// cannot be read, or does not parse, is not reported (false): what the
// cache removed is unknown, so nothing may rest on it until the next
// removal writes a sound one.
func (c *Cache) Removals() (string, bool) {
	data, err := os.ReadFile(filepath.Join(c.dir, SpaceState, KeyRemovals))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return NoRemovals, true
	case err != nil:
		c.log.Debug("cache read failed", "space", SpaceState, "err", err)
		return "", false
	}
	g := string(data)
	if !removalsFormat.MatchString(g) {
		return "", false
	}
	return g, true
}

// removed records that an entry of the blobs or the indexes is gone: it
// bumps the removal generation, then removes the entry [KeyCovered]. The
// caller has already removed the entry.
//
// Why no interleaving of processes leaves a stale record valid, with no
// lock between them: a sync reads the generation (g0) before it checks
// that every entry it keeps is there, and records g0 with its mark; a
// later sync trusts the mark only while the generation still reads g0.
// Take an entry the check found, gone by the time a later sync reads the
// generation. It was removed after the check, so after g0 was read, and
// its bump, which only starts once the removal succeeded, writes after g0
// was read. Every value written by a bump is new: its tag is random, so
// two processes that read the same count and both write count+1 still
// write different values, and a lost increment cannot bring back g0. A
// rename replaces the file whole, so any later read sees that bump's value
// or a later one, never g0 again. An entry removed before the check is
// missed by the check, and the sync records nothing. The count is only for
// people reading the file.
//
// Its callers also remove [KeyCovered] before they remove the entry, so a
// process stopped between the removal and the bump leaves no mark on the
// old generation. Removing it again after the bump keeps marks written
// before the generation existed, or in between, from vouching for a
// removal. A bump that cannot be
// written (the state cannot be written) is logged; the mark it would guard
// is written into the same directory.
func (c *Cache) removed() {
	var count uint64
	if g, ok := c.Removals(); ok && g != NoRemovals {
		count, _ = strconv.ParseUint(g[:strings.IndexByte(g, '-')], 10, 64)
	}
	tag := make([]byte, 8)
	_, _ = rand.Read(tag) // crypto/rand.Read never fails.
	next := strconv.FormatUint(count+1, 10) + "-" + hex.EncodeToString(tag)
	if err := c.write(filepath.Join(c.dir, SpaceState), KeyRemovals, []byte(next)); err != nil {
		c.log.Debug("cache write failed", "space", SpaceState, "err", err)
	}
	c.uncover()
}
