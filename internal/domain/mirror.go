// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"regexp"
	"slices"
	"strings"
)

// The names a mirrored directory keeps for itself. Each starts with a dot
// or an underscore that the mirror never uploads.
const (
	// MirrorAnnotatedDir holds the drawings of the documents annotated on
	// the tablet, laid out as the directory: Books/Novel.epub is drawn as
	// _annotated/Books/Novel.pdf.
	MirrorAnnotatedDir = "_annotated"
	// MirrorRemovedDir keeps, at their paths, the files of documents that
	// left the cloud folder: a mirror moves a file aside, never deletes it.
	MirrorRemovedDir = NotesRemovedDir
	// MirrorJournalName is the record of what was in step at the last pass.
	MirrorJournalName = ".rmctl-mirror.json"
	// MirrorLockName is the lock two processes take before working on one
	// directory.
	MirrorLockName = ".rmctl-mirror.lock"
)

// mirrorName is what a mirror may be called: as a profile.
var mirrorName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// ValidMirrorName reports whether name may name a mirror: 1-64 characters
// of a-z, 0-9, _ and -, starting with a letter or digit.
func ValidMirrorName(name string) bool { return mirrorName.MatchString(name) }

// temporaryPrefixes and temporarySuffixes mark the files an application
// is still writing, or keeps beside a document while it is open.
var (
	temporaryPrefixes = []string{"~$", ".~lock."} //nolint:gochecknoglobals // fixed lists
	temporarySuffixes = []string{                 //nolint:gochecknoglobals // fixed lists
		".part", ".partial", ".crdownload", ".download", ".tmp", ".temp", ".swp", "~",
	}
	systemFiles = []string{".ds_store", "thumbs.db", "desktop.ini", "icon\r"} //nolint:gochecknoglobals // fixed lists
)

// MirrorIgnored reports whether the file at rel, a slash path relative to
// a mirrored directory, stays out of the mirror: anything under a
// component that starts with a dot (the journal, the lock, .rmctl-removed,
// .git), anything under _annotated at the top, the files of the system
// (.DS_Store, Thumbs.db, desktop.ini) and the files an application is
// writing or keeps while a document is open (~$..., .part, .crdownload,
// .tmp, .swp).
func MirrorIgnored(rel string) bool {
	parts := strings.Split(path.Clean(strings.TrimPrefix(rel, "/")), "/")
	if len(parts) == 0 || parts[0] == MirrorAnnotatedDir || parts[0] == ".." {
		return true
	}
	for _, p := range parts {
		if strings.HasPrefix(p, ".") {
			return true
		}
	}
	base := strings.ToLower(parts[len(parts)-1])
	if slices.Contains(systemFiles, base) {
		return true
	}
	for _, p := range temporaryPrefixes {
		if strings.HasPrefix(base, p) {
			return true
		}
	}
	for _, s := range temporarySuffixes {
		if strings.HasSuffix(base, s) {
			return true
		}
	}
	return false
}

// MirrorUploadable reports whether the mirror uploads the file at rel: a
// PDF or an EPUB, as put takes them.
func MirrorUploadable(rel string) bool {
	switch strings.ToLower(path.Ext(rel)) {
	case ".pdf", ".epub":
		return true
	default:
		return false
	}
}

// MirrorStem is the name a file at rel takes in the cloud: its base name
// without the extension, as put names an upload.
func MirrorStem(rel string) string {
	base := path.Base(rel)
	return base[:len(base)-len(path.Ext(base))]
}

// MirrorAnnotatedPath is where the drawing of the document mirrored at rel
// goes: under MirrorAnnotatedDir, at the same path, with ext in place of
// the file's own extension.
func MirrorAnnotatedPath(rel, ext string) string {
	return path.Join(MirrorAnnotatedDir, strings.TrimSuffix(rel, path.Ext(rel))+"."+ext)
}

// Annotated reports whether a document carries marks of the tablet: a page
// of strokes, or old-style highlights.
func Annotated(it Item) bool {
	for _, f := range it.Files {
		if f.Extension() == "rm" || strings.HasPrefix(f.Name, string(it.ID)+".highlights/") {
			return true
		}
	}
	return false
}

// DrawnFingerprint names what a drawing of the document shows: every file
// of its index but the .metadata, which changes when the document is only
// opened, moved or renamed, and the thumbnails the tablet redraws. Two
// equal fingerprints draw the same pages.
func DrawnFingerprint(it Item) string {
	var lines []string
	for _, f := range it.Files {
		if f.Extension() == "metadata" || IsThumbnail(it.ID, f) {
			continue
		}
		lines = append(lines, f.Name+":"+f.Hash)
	}
	slices.Sort(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}
