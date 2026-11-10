// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package mirror

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/altessa-s/rmctl/internal/app/fetch"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// cloudDoc is a document below the mirrored folder and the local directory
// its folder is, relative to the mirrored one with slashes ("" at the top).
type cloudDoc struct {
	item domain.Item
	dir  string
}

// snapshot is the mirrored folder as the cloud holds it: its documents, and
// its folders by the local directory each is. Items in the trash stay out.
type snapshot struct {
	tree    *domain.Tree
	top     domain.ItemID
	docs    map[domain.ItemID]cloudDoc
	folders map[string]domain.ItemID
	// paths are the library paths of the folders, by local directory, for
	// creating the ones below them.
	paths map[string]string
}

// snapshotOf lays the folder top of tree out as directories: each folder
// below it by its safe name, two that collide without case told apart by
// identifier as render lays a folder out.
func snapshotOf(tree *domain.Tree, top domain.ItemID) *snapshot {
	s := &snapshot{
		tree: tree, top: top, docs: map[domain.ItemID]cloudDoc{},
		folders: map[string]domain.ItemID{"": top}, paths: map[string]string{"": libraryPath(tree, top)},
	}
	seen := map[domain.ItemID]bool{top: true}
	var walk func(folder domain.ItemID, dir string)
	walk = func(folder domain.ItemID, dir string) {
		children := slices.Clone(tree.Children(folder, false))
		slices.SortFunc(children, func(a, b domain.Item) int {
			return cmp.Or(a.Created.Compare(b.Created), strings.Compare(string(a.ID), string(b.ID)))
		})
		taken := map[string]bool{}
		for _, it := range children {
			if seen[it.ID] {
				continue
			}
			seen[it.ID] = true
			if !it.Kind.IsFolder() {
				s.docs[it.ID] = cloudDoc{item: it, dir: dir}
				continue
			}
			name := domain.SafeName(it.Name)
			if taken[strings.ToLower(name)] {
				name = fetch.Disambiguated(name, it.ID, "")
			}
			taken[strings.ToLower(name)] = true
			sub := path.Join(dir, name)
			s.folders[sub] = it.ID
			s.paths[sub] = tree.Path(it.ID)
			walk(it.ID, sub)
		}
	}
	walk(top, "")
	return s
}

// libraryPath is the path of folder top, "" for the top level.
func libraryPath(tree *domain.Tree, top domain.ItemID) string {
	if top == domain.RootID {
		return ""
	}
	return tree.Path(top)
}

// localPath is where the document c goes locally with the extension ext:
// its folder's directory, and its safe name with ext unless the name
// already ends with it.
func (c cloudDoc) localPath(ext string) string {
	return path.Join(c.dir, fetch.FileName(c.item.Name, strings.ToLower(ext)))
}

// sorted are the identifiers of docs in a stable order: by local path, then
// identifier.
func (s *snapshot) sorted() []domain.ItemID {
	ids := make([]domain.ItemID, 0, len(s.docs))
	for id := range s.docs {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b domain.ItemID) int {
		da, db := s.docs[a], s.docs[b]
		return cmp.Or(strings.Compare(path.Join(da.dir, da.item.Name), path.Join(db.dir, db.item.Name)), strings.Compare(string(a), string(b)))
	})
	return ids
}

// localFile is one file of the mirrored directory, and its SHA-256 once
// read.
type localFile struct {
	port.LocalEntry
	sha string
}

// settle is how long a file must have been left alone to be taken: one
// still being written, copied or downloaded waits for the next pass.
const settle = 2 * time.Second

// settled reports whether the file was last written settle ago or more. A
// time far in the future, from a clock that was wrong, counts as settled.
func (f *localFile) settled(now time.Time) bool {
	d := now.Sub(f.ModTime)
	return d >= settle || d < -time.Hour
}

// sameStat reports whether the file has the size and time e recorded.
func (f *localFile) sameStat(e entry) bool {
	return f.Size == e.Size && f.ModTime.Equal(e.ModTime)
}

// local are the files of the mirrored directory a mirror considers, by
// path, and how many files the directory holds that it ignores or does not
// upload.
type local struct {
	files map[string]*localFile
	// all counts every file below the directory but those of the mirror
	// itself: a directory with none is taken for one not mounted.
	all int
}

// scan lists the files of dir: those the mirror uploads, outside what it
// ignores.
func scan(disk port.MirrorFiles, dir string) (local, error) {
	entries, err := disk.Scan(dir, pruned)
	if err != nil {
		return local{}, err
	}
	out := local{files: map[string]*localFile{}}
	for _, e := range entries {
		if e.Path == domain.MirrorJournalName || e.Path == domain.MirrorLockName ||
			strings.HasPrefix(e.Path, domain.MirrorAnnotatedDir+"/") || strings.HasPrefix(e.Path, domain.MirrorRemovedDir+"/") {
			continue
		}
		out.all++
		if domain.MirrorIgnored(e.Path) || !domain.MirrorUploadable(e.Path) {
			continue
		}
		out.files[e.Path] = &localFile{LocalEntry: e}
	}
	return out, nil
}

// pruned reports the directories a scan does not enter: the mirror's own,
// and every one the mirror ignores.
func pruned(rel string) bool {
	return rel == domain.MirrorAnnotatedDir || rel == domain.MirrorRemovedDir || domain.MirrorIgnored(rel+"/x")
}

// paths are the paths of the files, sorted.
func (l local) paths() []string {
	out := make([]string, 0, len(l.files))
	for p := range l.files {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

// hash reads the file's SHA-256 once.
func hash(files port.LocalFiles, dir string, f *localFile) (string, error) {
	if f.sha != "" {
		return f.sha, nil
	}
	r, err := files.Open(abs(dir, f.Path))
	if err != nil {
		return "", err
	}
	defer func() { _ = r.Close() }()
	h := sha256.New()
	if _, err = io.Copy(h, r); err != nil {
		return "", err
	}
	f.sha = hex.EncodeToString(h.Sum(nil))
	return f.sha, nil
}

// abs is the local path of rel, a slash path relative to dir.
func abs(dir, rel string) string { return filepath.Join(dir, filepath.FromSlash(rel)) }

// signature names the state of the files of a directory, by path, size and
// time: a watch compares it from one look to the next.
func signature(l local) string {
	h := sha256.New()
	for _, p := range l.paths() {
		f := l.files[p]
		_, _ = fmt.Fprintf(h, "%s\x00%d\x00%d\n", p, f.Size, f.ModTime.UnixNano()) //nolint:errcheck // a hash takes every write
	}
	_, _ = fmt.Fprintf(h, "%d", l.all) //nolint:errcheck // see above
	return hex.EncodeToString(h.Sum(nil))
}
