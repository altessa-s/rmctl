// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package config

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/altessa-s/rmctl/internal/domain"
)

// Mirror is a [[mirror]] table: a local directory kept in step with a
// cloud folder of a profile's library.
type Mirror struct {
	Name   string `toml:"name"`
	Dir    string `toml:"dir"`
	Folder string `toml:"folder"`
	// Profile is the profile whose library it is; "" is the profile of the
	// run that runs it.
	Profile  string `toml:"profile,omitempty"`
	PullNew  bool   `toml:"pull_new,omitempty"`
	NoDelete bool   `toml:"no_delete,omitempty"`
}

// ErrMirrorTables means config.toml gives a mirror in a form rmctl cannot
// take out without rewriting what is around it: an inline table, or keys
// it cannot tell apart from another mirror's.
var ErrMirrorTables = errors.New("the mirror is not in a [[mirror]] table of its own; edit the file by hand")

// checkMirrors refuses a mirror rmctl could not run: a name that is not a
// mirror's, used twice, a directory that is not absolute, a folder that is
// not a path from the top, a profile that is not a profile's name.
func checkMirrors(mirrors []Mirror) error {
	seen := map[string]bool{}
	for i, m := range mirrors {
		key := fmt.Sprintf("mirror[%d]", i)
		switch {
		case !domain.ValidMirrorName(m.Name):
			return fmt.Errorf("%w: %s.name = %q: a name is 1-64 characters of a-z, 0-9, _ and -, starting with a letter or digit", ErrConfig, key, m.Name)
		case seen[m.Name]:
			return fmt.Errorf("%w: %s.name = %q: two mirrors have that name", ErrConfig, key, m.Name)
		case !filepath.IsAbs(m.Dir):
			return fmt.Errorf("%w: mirror %s: dir = %q: an absolute path", ErrConfig, m.Name, m.Dir)
		case !strings.HasPrefix(m.Folder, "/"):
			return fmt.Errorf("%w: mirror %s: folder = %q: a path from the top, such as \"/Papers\"", ErrConfig, m.Name, m.Folder)
		case m.Profile != "" && !profileName.MatchString(m.Profile):
			return fmt.Errorf("%w: mirror %s: profile = %q: %w", ErrConfig, m.Name, m.Profile, ErrProfile)
		}
		seen[m.Name] = true
	}
	return nil
}

// Mirrors are the mirrors the configuration file at path defines, in file
// order; a missing file defines none.
func Mirrors(path string, read ReadFunc) ([]Mirror, error) {
	if read == nil {
		read = ReadFile
	}
	data, exists, err := read(path)
	if err != nil || !exists {
		return nil, err
	}
	f, err := ParseFile(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return f.Mirrors, nil
}

// AddMirror appends the mirror's table to the file, creating it when
// missing; every other line stays as a person wrote it, and the result
// must read back as the same settings with the mirror added before it
// replaces the file.
func (w Writer) AddMirror(m Mirror) error {
	return w.edit(func(data []byte, current File) ([]byte, File, error) {
		for _, have := range current.Mirrors {
			if have.Name == m.Name {
				return nil, File{}, fmt.Errorf("%w: mirror %s is already defined", ErrConfigEdit, m.Name)
			}
		}
		want := current
		want.Mirrors = append(slices.Clone(current.Mirrors), m)
		return append(bytes.Clone(data), []byte(separator(data)+mirrorLines(m))...), want, nil
	})
}

// RemoveMirror takes the mirror's [[mirror]] table out of the file: its
// header and key lines go, every other line stays, and the result must
// read back as the same settings without the mirror. removed is false when
// the file defines no such mirror.
func (w Writer) RemoveMirror(name string) (removed bool, err error) {
	err = w.edit(func(data []byte, current File) ([]byte, File, error) {
		i := slices.IndexFunc(current.Mirrors, func(m Mirror) bool { return m.Name == name })
		if i < 0 {
			return nil, File{}, nil
		}
		next, werr := withoutMirror(data, name)
		if werr != nil {
			return nil, File{}, werr
		}
		if next == nil {
			// The mirror was all the file held: it is left empty.
			next = []byte{}
		}
		want := current
		want.Mirrors = slices.Delete(slices.Clone(current.Mirrors), i, i+1)
		removed = true
		return next, want, nil
	})
	return removed, err
}

// edit changes the file under its lock: change returns the new bytes and
// the settings they must read back as, or nil bytes for no change.
func (w Writer) edit(change func(data []byte, current File) ([]byte, File, error)) error {
	read := w.Read
	if read == nil {
		read = ReadFile
	}
	unlock, err := lockFile(w.Path)
	if err != nil {
		return err
	}
	defer unlock()
	data, existed, err := read(w.Path)
	if err != nil {
		return err
	}
	current, err := ParseFile(data)
	if err != nil {
		return fmt.Errorf("%s: %w", w.Path, err)
	}
	next, want, err := change(data, current)
	if err != nil || next == nil {
		return err
	}
	parsed, err := ParseFile(next)
	if err != nil {
		return fmt.Errorf("%s: %w: %w", w.Path, ErrMirrorTables, err)
	}
	if len(parsed.Mirrors) == 0 && len(want.Mirrors) == 0 {
		parsed.Mirrors, want.Mirrors = nil, nil
	}
	if !reflect.DeepEqual(parsed, want) {
		return fmt.Errorf("%s: %w", w.Path, ErrMirrorTables)
	}
	return replaceUnlessChanged(w.Path, data, existed, next)
}

// mirrorLines are the lines of the mirror's table.
func mirrorLines(m Mirror) string {
	var b strings.Builder
	b.WriteString("[[mirror]]\n")
	fmt.Fprintf(&b, "name = %s\ndir = %s\nfolder = %s\n", quote(m.Name), quote(m.Dir), quote(m.Folder))
	if m.Profile != "" {
		fmt.Fprintf(&b, "profile = %s\n", quote(m.Profile))
	}
	if m.PullNew {
		b.WriteString("pull_new = true\n")
	}
	if m.NoDelete {
		b.WriteString("no_delete = true\n")
	}
	return b.String()
}

// quote is s as a TOML basic string: quotes and backslashes escaped, and
// control characters as \uXXXX.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < firstPrintable || r == deleteChar:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// The characters quote escapes as \uXXXX: below the first printable one,
// and DEL.
const (
	firstPrintable = 0x20
	deleteChar     = 0x7f
)

// withoutMirror is data without the [[mirror]] table named name: from its
// header to its last key line. The comments and blank lines after that
// lead into what follows, and stay.
func withoutMirror(data []byte, name string) ([]byte, error) {
	lines := strings.SplitAfter(string(data), "\n")
	start, end := -1, -1
	for i := 0; i < len(lines); i++ {
		key, isHeader := tableKey(lines[i])
		if !isHeader || len(key) != 1 || key[0] != "mirror" || !strings.HasPrefix(strings.TrimSpace(lines[i]), "[[") {
			continue
		}
		j := i + 1
		for j < len(lines) {
			if _, h := tableKey(lines[j]); h {
				break
			}
			j++
		}
		var table struct {
			Name string `toml:"name"`
		}
		if err := toml.Unmarshal([]byte(strings.Join(lines[i+1:j], "")), &table); err == nil && table.Name == name {
			if start >= 0 {
				return nil, ErrMirrorTables
			}
			start, end = i, j
		}
		i = j - 1
	}
	if start < 0 {
		return nil, ErrMirrorTables
	}
	keep := end
	for keep > start+1 && blankOrComment(lines[keep-1]) {
		keep--
	}
	var out bytes.Buffer
	for _, l := range lines[:start] {
		out.WriteString(l)
	}
	rest := lines[keep:]
	if len(rest) > 0 && strings.TrimSpace(rest[0]) == "" && endsBlank(out.Bytes()) {
		rest = rest[1:]
	}
	for _, l := range rest {
		out.WriteString(l)
	}
	if strings.TrimSpace(strings.Join(rest, "")) == "" {
		return trimBlankEnd(out.Bytes()), nil
	}
	return out.Bytes(), nil
}
