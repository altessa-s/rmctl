// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
)

// ProfileDirs are the profiles that have a directory under root: a
// directory named as a profile that holds the token file, or nothing but
// what rmctl writes there. A missing root has none.
func ProfileDirs(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", root, err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || !profileName.MatchString(e.Name()) {
			continue
		}
		own, err := profileDir(filepath.Join(root, e.Name()))
		if err != nil {
			return nil, err
		}
		if own {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

// profileDir reports whether dir holds the token file, or nothing but
// what rmctl writes in a profile's directory.
func profileDir(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", dir, err)
	}
	own := true
	for _, e := range entries {
		if e.Name() == tokenFile {
			return true, nil
		}
		own = own && ours(e)
	}
	return own, nil
}

// ours reports whether e is a file rmctl writes in a profile's directory:
// the token file, or one being written in its place.
func ours(e fs.DirEntry) bool {
	if !e.Type().IsRegular() {
		return false
	}
	name := e.Name()
	return name == tokenFile || (strings.HasPrefix(name, "."+tokenFile+".") && strings.HasSuffix(name, ".part"))
}

// DirExists reports whether dir is a directory.
func DirExists(dir string) (bool, error) {
	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	case err != nil:
		return false, err
	default:
		return info.IsDir(), nil
	}
}

// RemoveProfileDir removes the files rmctl keeps in the profile's
// directory, then the directory. Any other file is left where it is, and
// the directory with it: kept says so. existed is false when there was no
// directory.
func RemoveProfileDir(cfg Config) (existed, kept bool, err error) {
	entries, err := os.ReadDir(cfg.ConfigDir)
	if errors.Is(err, fs.ErrNotExist) {
		return false, false, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("read %s: %w", cfg.ConfigDir, err)
	}
	for _, e := range entries {
		if !ours(e) {
			kept = true
			continue
		}
		if err := os.Remove(filepath.Join(cfg.ConfigDir, e.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return true, false, err
		}
	}
	if kept {
		return true, true, nil
	}
	if err := os.Remove(cfg.ConfigDir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return true, false, err
	}
	return true, false, nil
}

// TableProfiles are the profiles that have a table in the configuration
// file at path, sorted; a missing file has none.
func TableProfiles(path string, read ReadFunc) ([]string, error) {
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
	return slices.Sorted(maps.Keys(f.Profiles)), nil
}

// ErrProfileTables means config.toml gives the profile settings in a form
// rmctl cannot take out without rewriting what is around them: dotted keys
// under [profiles], or an inline table.
var ErrProfileTables = errors.New("the profile's settings are not in tables of their own; edit the file by hand")

// RemoveProfile takes the profile's tables — [profiles.<name>] and every
// [profiles.<name>.…] — out of the file. Only their header and key lines
// go: every other line, comments and blank lines included, stays as it was,
// and the result must read back as the same settings without the profile's
// before it replaces the file. removed is false when the file has no table
// of the profile.
func (w Writer) RemoveProfile(profile string) (removed bool, err error) {
	if !profileName.MatchString(profile) {
		return false, fmt.Errorf("profile %q: %w", profile, ErrProfile)
	}
	read := w.Read
	if read == nil {
		read = ReadFile
	}
	// A first look without the lock: a file without the profile is left
	// alone, and no lock file is created for it.
	if names, lookErr := TableProfiles(w.Path, read); lookErr != nil || !slices.Contains(names, profile) {
		return false, lookErr
	}
	unlock, err := lockFile(w.Path)
	if err != nil {
		return false, err
	}
	defer unlock()
	data, existed, err := read(w.Path)
	if err != nil {
		return false, err
	}
	current, err := ParseFile(data)
	if err != nil {
		return false, fmt.Errorf("%s: %w", w.Path, err)
	}
	if _, ok := current.Profiles[profile]; !ok {
		return false, nil
	}
	next := withoutProfile(data, profile)
	parsed, err := ParseFile(next)
	if err != nil {
		return false, fmt.Errorf("%s: %w", w.Path, ErrProfileTables)
	}
	want := current
	want.Profiles = maps.Clone(current.Profiles)
	delete(want.Profiles, profile)
	if len(want.Profiles) == 0 {
		want.Profiles = nil
	}
	if len(parsed.Profiles) == 0 {
		parsed.Profiles = nil
	}
	if !reflect.DeepEqual(parsed, want) {
		return false, fmt.Errorf("%s: %w", w.Path, ErrProfileTables)
	}
	return true, replaceUnlessChanged(w.Path, data, existed, next)
}

// header is a table header line, [a.b] or [[a.b]], with an optional
// comment after it; its first group is the key.
var header = regexp.MustCompile(`^\s*\[\[?\s*(.*?)\s*\]\]?\s*(?:#.*)?$`)

// tableKey is the key of a header line, split at its dots with quotes and
// spaces taken off; ok is false for any other line.
func tableKey(line string) (key []string, ok bool) {
	m := header.FindStringSubmatch(strings.TrimRight(line, "\r\n"))
	if m == nil {
		return nil, false
	}
	var parts []string
	var cur strings.Builder
	var quote rune
	for _, r := range m[1] {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			cur.WriteRune(r)
		case r == '"' || r == '\'':
			quote = r
		case r == '.':
			parts = append(parts, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	return append(parts, strings.TrimSpace(cur.String())), true
}

// blankOrComment reports a line that holds nothing but a comment, if that.
func blankOrComment(line string) bool {
	t := strings.TrimSpace(line)
	return t == "" || strings.HasPrefix(t, "#")
}

// withoutProfile is data without the tables of profile: from each header
// to its last key line. The comments and blank lines after that last key
// lead into what follows, and stay.
func withoutProfile(data []byte, profile string) []byte {
	lines := strings.SplitAfter(string(data), "\n")
	ofProfile := func(line string) (table, ok bool) {
		key, ok := tableKey(line)
		return ok && len(key) >= 2 && key[0] == "profiles" && key[1] == profile, ok
	}
	var out bytes.Buffer
	for i := 0; i < len(lines); {
		if mine, _ := ofProfile(lines[i]); !mine {
			out.WriteString(lines[i])
			i++
			continue
		}
		end := i + 1
		for end < len(lines) {
			if _, isHeader := ofProfile(lines[end]); isHeader {
				break
			}
			end++
		}
		keep := end
		for keep > i+1 && blankOrComment(lines[keep-1]) {
			keep--
		}
		rest := lines[keep:end]
		// One blank line between what came before and what follows is
		// enough.
		if len(rest) > 0 && strings.TrimSpace(rest[0]) == "" && endsBlank(out.Bytes()) {
			rest = rest[1:]
		}
		for _, l := range rest {
			out.WriteString(l)
		}
		if end == len(lines) && strings.TrimSpace(strings.Join(rest, "")) == "" {
			// The file ended with the profile's table: the blank lines that
			// set it apart go with it.
			return trimBlankEnd(out.Bytes())
		}
		i = end
	}
	return out.Bytes()
}

// trimBlankEnd is b without the blank lines at its end; it still ends a
// line.
func trimBlankEnd(b []byte) []byte {
	t := bytes.TrimRight(b, " \t\r\n")
	if len(t) == 0 {
		return t
	}
	if bytes.Contains(b, []byte("\r\n")) {
		return append(t, '\r', '\n')
	}
	return append(t, '\n')
}

// endsBlank reports whether b is empty or ends in a blank line.
func endsBlank(b []byte) bool {
	if len(b) == 0 {
		return true
	}
	// A blank last line is a line break, then one of nothing.
	const blankEnd = 2
	lines := strings.SplitAfter(string(b), "\n")
	if len(lines) < blankEnd {
		return false
	}
	return strings.TrimSpace(lines[len(lines)-2]) == "" && lines[len(lines)-1] == ""
}
