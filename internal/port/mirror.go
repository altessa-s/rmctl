// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package port

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/altessa-s/rmctl/internal/domain"
)

// The failures of a mirror.
var (
	// ErrMirrorBusy means another process works on the mirrored directory.
	ErrMirrorBusy = errors.New("another rmctl is working on this directory")
	// ErrNoMirror means no mirror has the name.
	ErrNoMirror = errors.New("no such mirror")
	// ErrMirrorExists means a mirror has the name already, or works on an
	// overlapping directory or cloud folder.
	ErrMirrorExists = errors.New("a mirror already exists")
	// ErrMirrorSpec means a mirror's definition cannot be mirrored.
	ErrMirrorSpec = errors.New("bad mirror")
	// ErrMirrorRefused means a pass would do something the mirror does not
	// do on its own: delete a mass of documents or files, or mirror into a
	// folder it no longer recognizes.
	ErrMirrorRefused = errors.New("the mirror refused")
)

// MirrorSpec is one mirror: a local directory kept in step with a cloud
// folder.
type MirrorSpec struct {
	// Name names the mirror in config.toml; an ad-hoc mirror has none.
	Name string
	// Dir is the local directory, an absolute path.
	Dir string
	// Folder is the cloud folder, a path from the top ("/" is the whole
	// library).
	Folder string
	// Profile is the profile whose library it is; "" is the profile of the
	// run.
	Profile string
	// PullNew draws the documents created in the folder on the tablet into
	// the annotated directory too.
	PullNew bool
	// NoDelete keeps deletions on either side from reaching the other.
	NoDelete bool
}

// MirrorRunOptions shape a run of mirrors.
type MirrorRunOptions struct {
	// DryRun works out each pass and changes nothing.
	DryRun bool
	// ForceDelete carries out deletions the mass-deletion guard refuses.
	ForceDelete bool
	// Watch keeps the mirrors in step until the context ends: a pass on
	// every change on either side, and a full pass on start, after a
	// reconnection and after the machine slept.
	Watch bool
	// Poll is how often a watch looks at the local directory; Debounce how
	// long it waits for changes to settle; Rescan how often it makes a full
	// pass in any case. Zero is the default of each.
	Poll, Debounce, Rescan time.Duration
}

// MirrorActionKind is what a pass did, or would do, to one document.
type MirrorActionKind string

// The actions of a pass.
const (
	// MirrorUpload uploads a new local file into the cloud folder.
	MirrorUpload MirrorActionKind = "upload"
	// MirrorUpdate replaces the PDF of a document changed locally, keeping
	// its annotations.
	MirrorUpdate MirrorActionKind = "update"
	// MirrorReplace replaces whole a document without annotations whose
	// EPUB changed locally, as put --replace does.
	MirrorReplace MirrorActionKind = "replace"
	// MirrorConflict is an update of a document changed on both sides: the
	// local PDF goes up, the annotations stay, and it is drawn again.
	MirrorConflict MirrorActionKind = "conflict"
	// MirrorDraw draws an annotated document into the annotated directory;
	// MirrorExport writes its .rmdoc there instead, in a build that draws
	// nothing.
	MirrorDraw   MirrorActionKind = "draw"
	MirrorExport MirrorActionKind = "export"
	// MirrorMoveLocal renames or moves a local file after its document,
	// renamed or moved on the tablet; MirrorMoveCloud renames or moves a
	// document after its file, renamed or moved locally.
	MirrorMoveLocal MirrorActionKind = "move-local"
	MirrorMoveCloud MirrorActionKind = "move-cloud"
	// MirrorTrash puts the document of a file deleted locally in the trash.
	MirrorTrash MirrorActionKind = "trash"
	// MirrorRemove moves the file of a document gone from the folder into
	// the removed directory.
	MirrorRemove MirrorActionKind = "remove"
	// MirrorAdopt pairs a local file with the document of its name already
	// in the folder, transferring nothing.
	MirrorAdopt MirrorActionKind = "adopt"
	// MirrorDetach stops following a document deleted on one side when
	// deletions are not propagated.
	MirrorDetach MirrorActionKind = "detach"
	// MirrorSkip is a change left for a later pass, and why.
	MirrorSkip MirrorActionKind = "skip"
	// MirrorRefuse is a deletion the guard refused.
	MirrorRefuse MirrorActionKind = "refuse"
)

// MirrorAction is one thing a pass did or would do.
type MirrorAction struct {
	Kind MirrorActionKind
	// Path is the local path, relative to the mirrored directory, with
	// slashes; To is where a move takes it, or the drawing written.
	Path, To string
	// Document is the cloud document, when there is one.
	Document domain.ItemID
	// Detail says more: why a change was skipped, what a content update
	// does to the pages.
	Detail string
	// Err is why the action failed; the others go on.
	Err error
}

// MirrorPass is what one pass of a mirror did, or, dry, would do.
type MirrorPass struct {
	Mirror  string
	Dir     string
	Folder  string
	DryRun  bool
	Started time.Time
	Ended   time.Time
	// Actions are the actions, in the order they were carried out.
	Actions []MirrorAction
	// Pending counts the changes left for a later pass: files still being
	// written, deletions refused, actions that failed.
	Pending int
	// Lite says the annotated documents were exported as .rmdoc, as this
	// build draws nothing.
	Lite bool
	// Err is why the pass failed as a whole, or the first action that did.
	Err error
}

// MirrorState is a mirror as mirror ls shows it: its definition, and what
// its journal says of the last pass.
type MirrorState struct {
	Spec MirrorSpec
	// Tracked counts the documents in step, LastPass is when the last pass
	// ended, LastError why it failed, and Pending what it left.
	Tracked   int
	LastPass  time.Time
	LastError string
	Pending   int
	// LocalChanges counts the files added, changed or deleted locally since
	// the last pass, by their size and time: no cloud is asked.
	LocalChanges int
	// Err is why the directory or its journal could not be read.
	Err error
}

// MirrorConfig is where the mirrors are defined: the [[mirror]] tables of
// config.toml.
type MirrorConfig interface {
	// Mirrors are the mirrors defined, in file order.
	Mirrors() ([]MirrorSpec, error)
	// AddMirror appends a mirror's table and leaves every other line.
	AddMirror(spec MirrorSpec) error
	// RemoveMirror takes the mirror's table out; removed is false when
	// there was none.
	RemoveMirror(name string) (removed bool, err error)
}

// LocalEntry is a regular file below a directory: its path relative to
// it, with slashes, its size and its modification time.
type LocalEntry struct {
	Path    string
	Size    int64
	ModTime time.Time
}

// MirrorFiles is what a mirror needs of the file system beyond LocalFiles.
type MirrorFiles interface {
	// Scan lists the regular files below dir, sorted by path; a symbolic
	// link is not followed, and a directory prune reports true for, by its
	// path relative to dir with slashes, is not entered. A missing dir is
	// an error.
	Scan(dir string, prune func(rel string) bool) ([]LocalEntry, error)
	// Lock takes the lock file at path, or ErrMirrorBusy when another
	// process holds it; release lets it go.
	Lock(path string) (release func() error, err error)
}

// DirWatcher hears of the changes below a directory as they happen.
type DirWatcher interface {
	// Watch watches dir and every directory below it that skip does not
	// name (by its path relative to dir, with slashes), and the ones made
	// later, calling ready once they are all watched and changed for each
	// change below them that skip does not name, until ctx ends, when it
	// returns nil. It returns an error when it cannot watch, or no longer
	// can — no watcher on this system, too many directories, events lost —
	// and the caller falls back to looking at the directory now and then.
	Watch(ctx context.Context, dir string, skip func(rel string) bool, ready, changed func()) error
}

// Timer is the clock a watch waits on.
type Timer interface {
	Clock
	// Sleep waits d, or until ctx ends, which is its error.
	Sleep(ctx context.Context, d time.Duration) error
}

// MirrorEngine keeps the mirrors of one profile in step.
type MirrorEngine interface {
	// Pass makes one pass of spec.
	Pass(ctx context.Context, spec MirrorSpec, opts MirrorRunOptions) (MirrorPass, error)
	// Watch keeps spec in step until ctx ends, reporting every pass to on.
	Watch(ctx context.Context, spec MirrorSpec, opts MirrorRunOptions, on func(MirrorPass)) error
}

// MirrorEngines open the engine of any profile.
type MirrorEngines interface {
	Open(profile string) (engine MirrorEngine, release func() error, err error)
}

// MirrorSelection names the mirrors a run takes: Names, every mirror with
// All, or the one mirror AdHoc defines without config.toml.
type MirrorSelection struct {
	Names []string
	All   bool
	AdHoc *MirrorSpec
}

// Mirrors defines, lists, removes and runs mirrors.
type Mirrors interface {
	// Add defines a mirror; a name, a directory or a cloud folder another
	// mirror of the profile overlaps is refused.
	Add(ctx context.Context, spec MirrorSpec) (MirrorSpec, error)
	// List is every mirror with its state.
	List(ctx context.Context) ([]MirrorState, error)
	// Remove forgets a mirror's definition. No file and no document is
	// touched, the journal included.
	Remove(ctx context.Context, name string) (MirrorSpec, error)
	// Run makes a pass of each mirror selected, or, with opts.Watch, keeps
	// them in step until ctx ends, reading config.toml again for mirrors
	// added and removed when sel.All. One mirror that fails does not stop
	// the others.
	Run(ctx context.Context, sel MirrorSelection, opts MirrorRunOptions, on func(MirrorPass)) error
}

// ServiceSpec is what the autostart service runs.
type ServiceSpec struct {
	// Program is the executable, an absolute path, and Args its arguments.
	Program string
	Args    []string
	// Env are the variables the service runs with, beyond the system's.
	Env map[string]string
}

// ServiceInfo is the autostart service as the system knows it.
type ServiceInfo struct {
	// Manager is what starts it: launchd, systemd or Task Scheduler.
	Manager string
	// Name is its label, unit or task name, and File the definition
	// written.
	Name, File string
	Installed  bool
	Running    bool
	// Log is the log file the service writes.
	Log string
	// Notes are what a person should know: lingering, a log of the
	// manager's own.
	Notes []string
}

// Autostart installs the mirror service with the system's service manager,
// for the user alone, and keeps its log.
type Autostart interface {
	Install(ctx context.Context, spec ServiceSpec) (ServiceInfo, error)
	Uninstall(ctx context.Context) (ServiceInfo, error)
	Status(ctx context.Context) (ServiceInfo, error)
	// OpenLog opens the service's log for appending, rotated as it grows.
	OpenLog() (io.WriteCloser, error)
	// ReadLog is the last lines of the log, oldest first, and the offset
	// of its end; ReadLogFrom the lines written after offset, and the new
	// end. A log rotated meanwhile is read from its start.
	ReadLog(lines int) (out []string, end int64, err error)
	ReadLogFrom(offset int64) (out []string, end int64, err error)
}

// MirrorService is the autostart service of the mirrors: one process that
// runs every mirror defined, whatever its profile.
type MirrorService interface {
	Install(ctx context.Context) (ServiceInfo, error)
	Uninstall(ctx context.Context) (ServiceInfo, error)
	Status(ctx context.Context) (ServiceInfo, error)
	// Log is the last lines of the service's log; with follow, on is
	// called again for each line added, until ctx ends.
	Log(ctx context.Context, lines int, follow bool, on func(line string)) error
	// OpenLog opens the service's log, which a run as the service writes.
	OpenLog() (io.WriteCloser, error)
}
