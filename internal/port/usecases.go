// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package port

import (
	"context"
	"time"

	"github.com/altessa-s/rmctl/internal/domain"
)

// The input ports: what the command line asks of the use cases. Each is
// implemented by one package under internal/app, and the command line
// imports nothing below this package and domain.

// AccountInfo is what the account command prints.
type AccountInfo struct {
	Registered bool
	Plan       Plan
}

// Account registers this machine and reports on the account.
type Account interface {
	// Register exchanges a one-time code for a device token. It refuses when
	// one is stored, unless replace.
	Register(ctx context.Context, code string, replace bool) error
	// Logout drops the token; revoke first removes the device at the cloud.
	Logout(ctx context.Context, revoke bool) error
	Info(ctx context.Context) (AccountInfo, error)
}

// FindQuery selects items: every non-empty field must match.
type FindQuery struct {
	// Pattern is a regular expression matched against the item's path.
	Pattern string
	Tags    []string
	Starred bool
	Under   string
	All     bool
	// Text is a case-insensitive substring of the typed text or the
	// highlighted text of a document's pages.
	Text string
}

// Browser reads the library.
type Browser interface {
	Tree(ctx context.Context) (*domain.Tree, error)
	// List is the folder a query names, or the item itself for a document.
	List(ctx context.Context, query string, all bool) (*domain.Tree, []domain.Item, error)
	Stat(ctx context.Context, query string) (*domain.Tree, domain.Item, error)
	Find(ctx context.Context, q FindQuery) (*domain.Tree, []domain.Item, error)
	// Usage is what the folder a query names takes, and each folder below
	// it down to depth levels; all counts the trash.
	Usage(ctx context.Context, query string, depth int, all bool) (*domain.Tree, domain.Usage, error)
	// Duplicates are the documents below the folder a query names that
	// share their original file; all counts the trash.
	Duplicates(ctx context.Context, query string, all bool) (*domain.Tree, []domain.DuplicateGroup, error)
}

// MirrorOptions shape a recursive download.
type MirrorOptions struct {
	// Incremental skips files already present with the size the cloud names.
	Incremental bool
	// Delete removes local files the cloud no longer holds.
	Delete bool
	Jobs   int
}

// MirrorReport is what a recursive transfer did.
type MirrorReport struct {
	Written, Skipped, Deleted int
}

// Fetcher downloads from the library.
type Fetcher interface {
	// Get writes a document's original to dest and returns the path written.
	// An empty dest is the document's name in the working directory.
	Get(ctx context.Context, query, dest string, progress Progress) (string, error)
	// Export writes documents whole into one .rmdoc archive. A folder
	// stands for every document below it; each document is in the archive
	// once, with every file of its index.
	Export(ctx context.Context, queries []string, dest string) (ExportReport, error)
	// Mirror downloads a folder's documents into dir, keeping the tree.
	Mirror(ctx context.Context, query, dir string, opts MirrorOptions) (MirrorReport, error)
	// Backup writes every document below the folder query names into dir,
	// one .rmdoc archive each, with a manifest. A document whose record
	// hash the manifest already holds is skipped; one the backup no longer
	// covers has its archive moved to dir/removed, or deleted with Prune.
	Backup(ctx context.Context, dir, query string, opts BackupOptions) (BackupReport, error)
}

// BackupOptions shape a backup.
type BackupOptions struct {
	// Profile is the profile the backup is of; a directory backing up
	// another profile is refused unless Force.
	Profile string
	// All takes the documents in the trash too.
	All bool
	// Prune deletes the archives of documents the backup no longer covers
	// instead of moving them to the removed directory.
	Prune bool
	Force bool
	Jobs  int
}

// BackupReport is what a backup did: Documents is how many the manifest
// lists, Written and Skipped how many archives were written or found
// unchanged, Moved and Pruned how many archives of documents gone were
// moved aside or deleted.
type BackupReport struct {
	Dir                                        string
	Documents, Written, Skipped, Moved, Pruned int
}

// Cover is which page a document shows as its cover.
type Cover string

// The covers a document can show.
const (
	// CoverLastOpened is the tablet's default: the page last read.
	CoverLastOpened Cover = "last"
	// CoverFirst is the first page.
	CoverFirst Cover = "first"
)

// PutOptions shape an upload.
type PutOptions struct {
	// Name overrides the visible name; empty is the file name without .pdf or .epub.
	Name string
	Tags []string
	// Cover is the cover page; empty is CoverLastOpened.
	Cover Cover
	// Replace replaces the original of the document of the same name in the
	// folder instead of adding a second one. Its tags are kept; its
	// annotations are not, since its pages are new.
	Replace bool
	// DryRun validates the file and resolves the destination, and uploads
	// nothing.
	DryRun bool
	// OnReplace, when set, hears of the document Replace is about to
	// replace, before anything is uploaded: its annotations will not survive.
	OnReplace func(old domain.Item)
	// ContentOnly replaces only the PDF of the PDF document of the same name
	// in the folder, keeping its pages' annotations; there must be one.
	// DropAnnotations lets it drop the annotations of pages the new PDF no
	// longer has, which it otherwise refuses.
	ContentOnly     bool
	DropAnnotations bool
	// OnContentPlan, when set, hears what ContentOnly will do, before
	// anything is uploaded, and also when it refuses.
	OnContentPlan func(ContentPlan)
	// CurrentPage is the page a PDF opens on, from 1; 0 leaves it to the
	// tablet.
	CurrentPage int
}

// ContentPlan is what a ContentOnly upload does to a document's pages.
type ContentPlan struct {
	// Document is the document whose PDF is replaced.
	Document domain.Item
	// OldPages and NewPages are the document's pages before and after;
	// OldOriginal and NewOriginal the pages of the old and the new PDF.
	OldPages, NewPages       int
	OldOriginal, NewOriginal int
	// Kept, Appended and Dropped count the pages that stay, the pages added
	// for the new PDF's extra pages, and the pages whose PDF page is gone.
	Kept, Appended, Dropped int
	// Annotated counts the kept pages that carry annotations, which stay.
	Annotated int
	// IDMap maps each kept page's identifier to the new one it takes, so
	// the tablet draws its preview from the new PDF; its annotations go
	// with it.
	IDMap map[string]string
	// Lost are the dropped pages that carry annotations.
	Lost []AnnotatedPage
}

// AnnotatedPage is a page with annotations.
type AnnotatedPage struct {
	// Number is the page's place in the document, from 1, and ID its
	// identifier.
	Number int
	ID     string
	// Files counts its annotation files: the .rm, and old-style highlights.
	Files int
}

// Writer changes the library. Every method commits one new root.
type Writer interface {
	Mkdir(ctx context.Context, path string, parents bool) (domain.Item, error)
	Put(ctx context.Context, local, dest string, opts PutOptions, progress Progress) (domain.Item, error)
	// Move moves src into the folder dst, or renames it when dst names no
	// folder and its parent exists.
	Move(ctx context.Context, src, dst string) (domain.Item, error)
	// Rename renames an item where it is; mv moves it.
	Rename(ctx context.Context, query, name string) (Edited, error)
	// Pin sets the pinned flag of items; one already so is left alone.
	Pin(ctx context.Context, queries []string, on bool) ([]Edited, error)
	// Tag adds the tags add to documents and takes the tags remove off
	// them; a document already so is left alone. Folders take no tags.
	Tag(ctx context.Context, queries []string, add, remove []string) ([]Edited, error)
	Trash(ctx context.Context, queries []string) ([]domain.Item, error)
	// Restore takes items out of the trash into the folder dest; "" is the
	// top level.
	Restore(ctx context.Context, queries []string, dest string) ([]domain.Item, error)
	// Remove deletes items for good, with every descendant of a folder.
	Remove(ctx context.Context, queries []string) ([]domain.Item, error)
	// Import adds every document of a local .rmdoc archive into the folder
	// dest, with its pages, annotations and tags, in one commit. A document
	// whose identifier the library already holds gets a new identifier,
	// and its files are renamed to it; the library's document is untouched.
	Import(ctx context.Context, local, dest string) ([]ImportedDocument, error)
	// PlanImport is what Import would add right now, with the identifiers
	// it would assign against the current root, and uploads nothing.
	PlanImport(ctx context.Context, local, dest string) ([]ImportedDocument, error)
	// Upload sends every PDF and EPUB below dir into the folder dest.
	Upload(ctx context.Context, dir, dest string, opts UploadOptions) (MirrorReport, error)
	// RestoreBackup imports the archives of a backup directory, in one
	// commit, each into its folder from the manifest, below opts.To; missing
	// folders are created. A document whose identifier the library holds
	// gets a new one, as Import gives it.
	RestoreBackup(ctx context.Context, dir string, opts RestoreOptions) (RestoreReport, error)
}

// RestoreOptions shape a restore from a backup.
type RestoreOptions struct {
	// To is the folder the manifest's paths are recreated under; "" or "/"
	// is the top level, which puts every document back where it was.
	To string
	// IDs are the documents to restore, each an identifier or a unique
	// prefix of one; none is every document of the backup.
	IDs []string
	// DryRun works out the restore against the current library and
	// uploads nothing.
	DryRun bool
}

// RestoreReport is what a restore did, or would do.
type RestoreReport struct {
	// Profile and Created are the backup's.
	Profile string
	Created time.Time
	// Documents are the documents restored, in manifest order.
	Documents []RestoredDocument
	// Folders are the paths of the folders created, parents first.
	Folders []string
}

// RestoredDocument is one document a restore imported: the item, the
// identifier it had in the backup, the archive it came from and the
// folder path it went into.
type RestoredDocument struct {
	Item   domain.Item
	From   domain.ItemID
	File   string
	Folder string
}

// Edited is an item after an edit, and whether the edit changed it: an
// edit that would change nothing is reported, not committed.
type Edited struct {
	Item    domain.Item
	Changed bool
}

// UploadOptions shape a recursive upload.
type UploadOptions struct {
	// Incremental skips a file whose name the folder already holds.
	Incremental bool
	Jobs        int
}

// Watcher follows the notification socket.
type Watcher interface {
	Watch(ctx context.Context, on func(Event)) error
}

// FileReport is one file of an item with its decoded text, for diagnostics.
type FileReport struct {
	File domain.File
	// Text is the file's contents when it is JSON or text, else nil.
	Text []byte
}

// HashReport says which hash a record carries.
type HashReport struct {
	Record domain.Record
	// ByIndexFile is true when the record hash is the hash of the index
	// file's bytes, false when it is the node hash.
	ByIndexFile bool
}

// Inspector answers protocol questions for rmctl debug.
type Inspector interface {
	Root(ctx context.Context) (RootState, error)
	Blob(ctx context.Context, hash, name string) ([]byte, error)
	// SaveBlob writes a blob to dest and returns the path written.
	SaveBlob(ctx context.Context, hash, name, dest string) (string, error)
	Files(ctx context.Context, query string) (domain.Item, []FileReport, error)
	Hashes(ctx context.Context) ([]HashReport, error)
	CheckFiles(ctx context.Context) ([]string, error)
	// Sweep counts, across every document, which .metadata and .content
	// fields occur and how often.
	Sweep(ctx context.Context) (map[string]int, error)
}
