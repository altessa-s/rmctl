// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package mirror keeps a local directory and a cloud folder in step, both
// ways. It implements port.Mirrors, port.MirrorEngine and
// port.MirrorService.
//
// A pass reads the directory's journal (what was in step last time, by
// document identifier), the cloud folder and the local files, and plans:
//
//   - a new PDF or EPUB is uploaded, through the write use case, into the
//     folder of its directory, created when missing; one whose name the
//     folder already holds is paired with that document instead;
//   - a PDF changed locally replaces the document's PDF and keeps its
//     annotations (put --content-only); changed on the tablet too, it is a
//     conflict resolved the same way, and drawn again; an EPUB changed
//     locally replaces its document whole (put --replace) when the document
//     carries no annotations, and waits otherwise;
//   - a document renamed or moved on the tablet renames or moves its file,
//     and a file renamed or moved locally (found again by its SHA-256) its
//     document;
//   - a file deleted locally puts its document in the trash; a document
//     deleted, put in the trash or moved out of the folder on the tablet
//     moves its file into .rmctl-removed. Nothing is ever deleted outright,
//     and a guard refuses a mass of deletions on one side unless forced;
//   - a document whose pages changed and that carries annotations is drawn
//     into _annotated, through the render use case, or exported there as
//     .rmdoc in a build that draws nothing; with PullNew the documents
//     created on the tablet are drawn too.
//
// The journal is written whole after every pass, and every few seconds of
// a long one, so each pass starts from what was in step: the mirror's own
// changes come back as nothing to do.
// A watch holds the directory's lock, hears of local changes through
// port.DirWatcher (polling the directory's listing while that cannot
// watch) and follows the notification socket, and makes a full pass on start, after a
// reconnection, after the machine slept and on a fixed interval.
package mirror
