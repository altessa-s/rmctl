// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package clitest

import (
	"context"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// Fake implements every input port over a fixed library. Err, when set, is
// returned by every call; the call is still recorded.
type Fake struct {
	// Unchanged makes Rename, Pin and Tag report every item unchanged.
	Unchanged bool
	// ContentPlan is what a put --content-only hears it will do.
	ContentPlan *port.ContentPlan
	// CacheAt is what Cache.Status answers, CacheErr fails Status and
	// Clear, and Offline marks the services as offline.
	CacheAt  port.CacheReport
	CacheErr error
	Offline  bool
	// SecretsAt is what Secrets.Status answers, Migration what Migrate does,
	// and SecretsErr fails both.
	SecretsAt  port.SecretsStatus
	Migration  port.SecretsMigration
	SecretsErr error
	// ReplaceOld is the document a put --replace finds of the same name;
	// zero is none, so the file is added.
	ReplaceOld domain.Item
	Library    *domain.Tree
	Err        error
	// AccountInfo is what Account.Info answers.
	AccountInfo port.AccountInfo
	// Path is what Get and Export answer when non-empty.
	Path string
	// Report is what Mirror and Upload answer.
	Report port.MirrorReport
	// Events are what Watch delivers before it returns WatchErr.
	Events   []port.Event
	WatchErr error
	// Inspector answers.
	RootState port.RootState
	BlobData  []byte
	Reports   []port.FileReport
	HashList  []port.HashReport
	Missing   []string
	Counts    map[string]int
	// Progress, when set, is reported once by Get and Put.
	Progress [2]int64
	// PutErr fails Put of these local files; the other files still succeed.
	PutErr map[string]error
	// Pages answers: the document's pages, what Unmark reports, and an error
	// of the page calls alone (such as domain.ErrPagesUnsupported).
	Document port.DocumentText
	Unmarked port.UnmarkReport
	PagesErr error
	// Archive answers: what Import adds, what Inspect lists, the documents
	// Archives.Pages reads, and an error of the archive calls alone (such as
	// domain.ErrArchivesUnsupported).
	Imported   []port.ImportedDocument
	Inspected  port.ArchiveReport
	Local      []port.DocumentText
	ArchiveErr error
	// Backed is what Backup answers, with the directory filled in, and
	// Restored what RestoreBackup answers.
	Backed   port.BackupReport
	Restored port.RestoreReport
	// Rendered is what Render and RenderFile answer, with the path filled
	// in when it has none, and RenderErr fails them alone (such as
	// domain.ErrRenderUnsupported).
	Rendered  port.RenderReport
	RenderErr error
	// RenderFolders are the queries Folder reports as folders, "/" always;
	// RenderedTree is what RenderTree answers, with the directory filled in when
	// it has none, and TreeErr the error it answers with.
	RenderFolders []string
	RenderedTree  port.RenderTreeReport
	TreeErr       error
	// ProfileList is what Profiles.List answers, ProfileInfo what Info
	// answers (with the name asked for filled in when it has none), and
	// ProfilesErr fails List, Info and Add. Removal is what Remove answers,
	// with RemoveErr: a removal that stopped part way.
	ProfileList []port.ProfileSummary
	ProfileInfo port.ProfileInfo
	ProfilesErr error
	Removal     port.ProfileRemoval
	RemoveErr   error
	// Synced is what Sync answers, after it reports SyncSteps to the
	// progress it was given.
	Synced    port.SyncReport
	SyncSteps []port.SyncProgress
	// NoListing makes Completion answer port.ErrNoListing, as a cache
	// nothing listed the library into; otherwise it lists Library.
	NoListing bool
	// Opened is what Open and OpenFile answer, with the path filled in
	// when it has none; RenderErr fails them before anything is drawn, and
	// OpenErr after, as a viewer that would not start.
	Opened  port.OpenReport
	OpenErr error

	mu    sync.Mutex
	calls []string
}

// Services is the fake behind every port.
func (f *Fake) Services() cliutil.Services {
	return cliutil.Services{
		Account: f, Browser: f, Fetcher: f, Writer: f, Watcher: f, Inspector: f, Pages: f,
		Archives: (*archivesFake)(f), Rendering: f, Syncer: f, Secrets: (*secretsFake)(f), Cache: (*cacheFake)(f), Offline: f.Offline,
		Profiles: (*profilesFake)(f), Completion: (*listingFake)(f), Viewing: (*viewingFake)(f),
	}
}

// Calls are the calls made, each "Method arg arg".
func (f *Fake) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *Fake) record(method string, args ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, strings.TrimSpace(method+" "+strings.Join(args, " ")))
	return f.Err
}

func (f *Fake) tree() *domain.Tree {
	if f.Library == nil {
		return Library()
	}
	return f.Library
}

func (f *Fake) progress(p port.Progress) {
	if p != nil && f.Progress[1] != 0 {
		p(f.Progress[0], f.Progress[1])
	}
}

func opt(b bool, name string) string {
	if b {
		return name
	}
	return ""
}

// Account.

func (f *Fake) Register(_ context.Context, code string, replace bool) error {
	return f.record("Register", code, opt(replace, "replace"))
}

func (f *Fake) Logout(_ context.Context, revoke bool) error {
	return f.record("Logout", opt(revoke, "revoke"))
}

func (f *Fake) Info(context.Context) (port.AccountInfo, error) {
	return f.AccountInfo, f.record("Info")
}

// Browser.

func (f *Fake) Tree(context.Context) (*domain.Tree, error) {
	if err := f.record("Tree"); err != nil {
		return nil, err
	}
	return f.tree(), nil
}

func (f *Fake) List(_ context.Context, query string, all bool) (*domain.Tree, []domain.Item, error) {
	if err := f.record("List", query, opt(all, "all")); err != nil {
		return nil, nil, err
	}
	t := f.tree()
	if query == "/" {
		return t, t.Children(domain.RootID, all), nil
	}
	it, found, err := t.Resolve(query, all)
	if err != nil {
		return nil, nil, err
	}
	if !found {
		return t, t.Children(domain.RootID, all), nil
	}
	if !it.Kind.IsFolder() {
		return t, []domain.Item{it}, nil
	}
	return t, t.Children(it.ID, all), nil
}

func (f *Fake) Stat(_ context.Context, query string) (*domain.Tree, domain.Item, error) {
	if err := f.record("Stat", query); err != nil {
		return nil, domain.Item{}, err
	}
	t := f.tree()
	it, found, err := t.Resolve(query, true)
	if err == nil && !found {
		err = domain.ErrNotFound
	}
	return t, it, err
}

// Usage implements port.Browser over the fake's library: the top level,
// or the folder whose identifier query is.
func (f *Fake) Usage(_ context.Context, query string, depth int, all bool) (*domain.Tree, domain.Usage, error) {
	if err := f.record("Usage", query, strconv.Itoa(depth), opt(all, "all")); err != nil {
		return nil, domain.Usage{}, err
	}
	tree := f.tree()
	return tree, tree.Usage(f.folder(query), depth, all), nil
}

// Duplicates implements port.Browser over the fake's library.
func (f *Fake) Duplicates(_ context.Context, query string, all bool) (*domain.Tree, []domain.DuplicateGroup, error) {
	if err := f.record("Duplicates", query, opt(all, "all")); err != nil {
		return nil, nil, err
	}
	tree := f.tree()
	return tree, tree.Duplicates(f.folder(query), all), nil
}

func (f *Fake) folder(query string) domain.ItemID {
	if query == "" || query == "/" {
		return domain.RootID
	}
	return domain.ItemID(query)
}

func (f *Fake) Find(_ context.Context, q port.FindQuery) (*domain.Tree, []domain.Item, error) {
	if err := f.record("Find", q.Pattern, strings.Join(q.Tags, ","), opt(q.Starred, "starred"), q.Under, opt(q.All, "all"), q.Text); err != nil {
		return nil, nil, err
	}
	t := f.tree()
	re, err := regexp.Compile(q.Pattern)
	if err != nil {
		return nil, nil, err
	}
	var out []domain.Item
	for _, it := range t.Items() {
		if (!q.All && t.IsTrashed(it.ID)) || (q.Starred && !it.Pinned) || !re.MatchString(t.Path(it.ID)) {
			continue
		}
		out = append(out, it)
	}
	return t, out, nil
}

// Fetcher.

func (f *Fake) Get(_ context.Context, query, dest string, progress port.Progress) (string, error) {
	f.progress(progress)
	return f.answerPath(dest), f.record("Get", query, dest)
}

func (f *Fake) Export(_ context.Context, queries []string, dest string) (port.ExportReport, error) {
	err := f.record("Export", append(slices.Clone(queries), dest)...)
	return port.ExportReport{Path: f.answerPath(dest), Documents: len(queries), Files: filesPerDocument * len(queries)}, err
}

// filesPerDocument is how many files the fake says each exported document
// had: a .metadata, a .content and an original.
const filesPerDocument = 3

// Import implements port.Writer.
func (f *Fake) Import(_ context.Context, local, dest string) ([]port.ImportedDocument, error) {
	if err := f.record("Import", local, dest); err != nil {
		return nil, err
	}
	if f.ArchiveErr != nil {
		return nil, f.ArchiveErr
	}
	if err := f.PutErr[local]; err != nil {
		return nil, err
	}
	return f.Imported, nil
}

// PlanImport implements port.Writer: Imported, without the call counting as
// an import.
func (f *Fake) PlanImport(_ context.Context, local, dest string) ([]port.ImportedDocument, error) {
	if err := f.record("PlanImport", local, dest); err != nil {
		return nil, err
	}
	if f.ArchiveErr != nil {
		return nil, f.ArchiveErr
	}
	return f.Imported, nil
}

// archivesFake is the Fake as port.Archives: its Pages would clash with
// port.Pages.Pages on the Fake itself.
type archivesFake Fake

var _ port.Archives = (*archivesFake)(nil)

// Inspect implements port.Archives.
func (a *archivesFake) Inspect(_ context.Context, path string) (port.ArchiveReport, error) {
	f := (*Fake)(a)
	if err := f.record("Inspect", path); err != nil {
		return port.ArchiveReport{}, err
	}
	r := f.Inspected
	r.Path = path
	return r, f.ArchiveErr
}

// Pages implements port.Archives.
func (a *archivesFake) Pages(_ context.Context, path string, pages []string) ([]port.DocumentText, error) {
	f := (*Fake)(a)
	if err := f.record("LocalPages", append([]string{path}, pages...)...); err != nil {
		return nil, err
	}
	return f.Local, f.ArchiveErr
}

func (f *Fake) answerPath(dest string) string {
	if f.Path != "" {
		return f.Path
	}
	return dest
}

func (f *Fake) Mirror(_ context.Context, query, dir string, o port.MirrorOptions) (port.MirrorReport, error) {
	return f.Report, f.record("Mirror", query, dir, opt(o.Incremental, "incremental"), opt(o.Delete, "delete"))
}

// Backup implements port.Fetcher.
func (f *Fake) Backup(_ context.Context, dir, query string, o port.BackupOptions) (port.BackupReport, error) {
	err := f.record("Backup", dir, query, "profile="+o.Profile, opt(o.All, "all"), opt(o.Prune, "prune"),
		opt(o.Force, "force"), opt(o.Rescope, "rescope"), "jobs="+strconv.Itoa(o.Jobs))
	r := f.Backed
	r.Dir = dir
	return r, err
}

// Rendering.

// Render implements port.Rendering.
func (f *Fake) Render(_ context.Context, query, dest string, o port.RenderOptions) (port.RenderReport, error) {
	return f.rendered("Render", query, dest, o)
}

// RenderFile implements port.Rendering.
func (f *Fake) RenderFile(_ context.Context, path, dest string, o port.RenderOptions) (port.RenderReport, error) {
	return f.rendered("RenderFile", path, dest, o)
}

// Folder implements port.Rendering; it records no call.
func (f *Fake) Folder(_ context.Context, query string) (bool, error) {
	f.mu.Lock()
	err := f.Err
	f.mu.Unlock()
	if err != nil {
		return false, err
	}
	return query == "/" || slices.Contains(f.RenderFolders, query), nil
}

// RenderTree implements port.Rendering.
func (f *Fake) RenderTree(_ context.Context, query, dir string, o port.RenderOptions, t port.RenderTreeOptions) (port.RenderTreeReport, error) {
	args := append(renderArgs(query, dir, o), opt(t.All, "all"), opt(t.Incremental, "incremental"), "jobs="+strconv.Itoa(t.Jobs))
	if err := f.record("RenderTree", args...); err != nil {
		return port.RenderTreeReport{}, err
	}
	r := f.RenderedTree
	if r.Dir == "" {
		r.Dir = dir
	}
	return r, f.TreeErr
}

// renderArgs are a rendering's arguments as a call records them.
func renderArgs(from, dest string, o port.RenderOptions) []string {
	args := []string{from, dest, "format=" + string(o.Format), "page=" + strconv.Itoa(o.Page), "mode=" + string(o.Mode),
		"grain=" + string(o.Grain), "paper=" + string(o.Paper), "scale=" + strconv.FormatFloat(o.Scale, 'g', -1, 64), "quality=" + strconv.Itoa(o.Quality)}
	if o.NoTemplates {
		args = append(args, "no-template")
	}
	if o.Unreadable != "" && o.Unreadable != port.UnreadablePlaceholder {
		args = append(args, "unreadable="+string(o.Unreadable))
	}
	if o.Document != "" {
		args = append(args, "id="+o.Document)
	}
	return args
}

func (f *Fake) rendered(method, from, dest string, o port.RenderOptions) (port.RenderReport, error) {
	if err := f.record(method, renderArgs(from, dest, o)...); err != nil {
		return port.RenderReport{}, err
	}
	if f.RenderErr != nil {
		return port.RenderReport{}, f.RenderErr
	}
	r := f.Rendered
	if r.Path == "" {
		r.Path = dest
	}
	return r, nil
}

// Writer.

// RestoreBackup implements port.Writer.
func (f *Fake) RestoreBackup(_ context.Context, dir string, o port.RestoreOptions) (port.RestoreReport, error) {
	args := append([]string{dir, "to=" + o.To}, o.IDs...)
	if err := f.record("RestoreBackup", append(args, opt(o.DryRun, "dry-run"))...); err != nil {
		return port.RestoreReport{}, err
	}
	return f.Restored, f.ArchiveErr
}

func (f *Fake) Mkdir(_ context.Context, p string, parents bool) (domain.Item, error) {
	return Item(Spec{ID: "f-new", Name: p[strings.LastIndex(p, "/")+1:], Kind: domain.KindFolder}),
		f.record("Mkdir", p, opt(parents, "parents"))
}

func (f *Fake) Put(_ context.Context, local, dest string, o port.PutOptions, progress port.Progress) (domain.Item, error) {
	f.progress(progress)
	if o.Replace && o.OnReplace != nil && f.ReplaceOld.ID != "" {
		o.OnReplace(f.ReplaceOld)
	}
	if o.ContentOnly && o.OnContentPlan != nil && f.ContentPlan != nil {
		o.OnContentPlan(*f.ContentPlan)
	}
	args := []string{local, dest, o.Name, strings.Join(o.Tags, ","), string(o.Cover), opt(o.Replace, "replace"), opt(o.DryRun, "dry-run")}
	if o.ContentOnly {
		args = append(args, "content-only", opt(o.DropAnnotations, "drop"))
	}
	if o.CurrentPage > 0 {
		args = append(args, "page="+strconv.Itoa(o.CurrentPage))
	}
	err := f.record("Put", args...)
	if err == nil {
		err = f.PutErr[local]
	}
	return Item(Spec{ID: "d-new", Name: "Uploaded", Kind: domain.KindPDF}), err
}

func (f *Fake) Move(_ context.Context, src, dst string) (domain.Item, error) {
	return Item(Spec{ID: "d-paper", Name: "Paper", Kind: domain.KindPDF}), f.record("Move", src, dst)
}

func (f *Fake) Trash(_ context.Context, q []string) ([]domain.Item, error) {
	return f.items(q), f.record("Trash", q...)
}

func (f *Fake) Restore(_ context.Context, q []string, dest string) ([]domain.Item, error) {
	args := q
	if dest != "" {
		args = append(slices.Clone(q), "to="+dest)
	}
	return f.items(q), f.record("Restore", args...)
}

func (f *Fake) Remove(_ context.Context, q []string) ([]domain.Item, error) {
	return f.items(q), f.record("Remove", q...)
}

func (f *Fake) items(queries []string) []domain.Item {
	var out []domain.Item
	for _, q := range queries {
		if it, found, err := f.tree().Resolve(q, true); err == nil && found {
			out = append(out, it)
		}
	}
	return out
}

// edited reports the items queries name as edited, or unchanged.
func (f *Fake) edited(queries []string) []port.Edited {
	items := f.items(queries)
	out := make([]port.Edited, 0, len(items))
	for _, it := range items {
		out = append(out, port.Edited{Item: it, Changed: !f.Unchanged})
	}
	return out
}

func (f *Fake) Rename(_ context.Context, query, name string) (port.Edited, error) {
	if err := f.record("Rename", query, name); err != nil {
		return port.Edited{}, err
	}
	out := f.edited([]string{query})
	if len(out) == 0 {
		return port.Edited{}, domain.ErrNotFound
	}
	out[0].Item.Name = name
	return out[0], nil
}

func (f *Fake) Pin(_ context.Context, queries []string, on bool) ([]port.Edited, error) {
	return f.edited(queries), f.record("Pin", append([]string{strconv.FormatBool(on)}, queries...)...)
}

func (f *Fake) Tag(_ context.Context, queries []string, add, remove []string) ([]port.Edited, error) {
	return f.edited(queries), f.record("Tag", append([]string{"+" + strings.Join(add, ","), "-" + strings.Join(remove, ",")}, queries...)...)
}

func (f *Fake) Upload(_ context.Context, dir, dest string, o port.UploadOptions) (port.MirrorReport, error) {
	return f.Report, f.record("Upload", dir, dest, opt(o.Incremental, "incremental"))
}

// Watcher.

func (f *Fake) Watch(ctx context.Context, on func(port.Event)) error {
	if err := f.record("Watch"); err != nil {
		return err
	}
	for _, e := range f.Events {
		on(e)
	}
	if f.WatchErr != nil {
		return f.WatchErr
	}
	<-ctx.Done()
	return ctx.Err()
}

// Inspector.

func (f *Fake) Root(context.Context) (port.RootState, error) {
	return f.RootState, f.record("Root")
}

func (f *Fake) Blob(_ context.Context, hash, name string) ([]byte, error) {
	return f.BlobData, f.record("Blob", hash, name)
}

func (f *Fake) SaveBlob(_ context.Context, hash, name, dest string) (string, error) {
	return dest, f.record("SaveBlob", hash, name, dest)
}

func (f *Fake) Files(_ context.Context, query string) (domain.Item, []port.FileReport, error) {
	return Item(Spec{ID: query, Name: "x", Kind: domain.KindPDF}), f.Reports, f.record("Files", query)
}

func (f *Fake) Hashes(context.Context) ([]port.HashReport, error) {
	return f.HashList, f.record("Hashes")
}

func (f *Fake) CheckFiles(context.Context) ([]string, error) {
	return f.Missing, f.record("CheckFiles")
}

func (f *Fake) Sweep(context.Context) (map[string]int, error) {
	return f.Counts, f.record("Sweep")
}

// inputPorts is every input port: the fake stands in for all the use cases.
type inputPorts interface {
	port.Account
	port.Browser
	port.Fetcher
	port.Writer
	port.Watcher
	port.Inspector
	port.Pages
}

var _ inputPorts = (*Fake)(nil)

// Pages.

func (f *Fake) pagesErr(method string, args ...string) error {
	if err := f.record(method, args...); err != nil {
		return err
	}
	return f.PagesErr
}

func (f *Fake) Pages(_ context.Context, query string, pages []string) (port.DocumentText, error) {
	if err := f.pagesErr("Pages", append([]string{query}, pages...)...); err != nil {
		return port.DocumentText{}, err
	}
	return f.Document, nil
}

func (f *Fake) Unmark(_ context.Context, query string, pages []string) (port.UnmarkReport, error) {
	if err := f.pagesErr("Unmark", append([]string{query}, pages...)...); err != nil {
		return port.UnmarkReport{}, err
	}
	return f.Unmarked, nil
}

func (f *Fake) Notebook(_ context.Context, local, dest string, o port.NotebookOptions) (domain.Item, error) {
	if err := f.pagesErr("Notebook", local, dest, o.Name, string(o.Device), strings.Join(o.Tags, ",")); err != nil {
		return domain.Item{}, err
	}
	if err := f.PutErr[local]; err != nil {
		return domain.Item{}, err
	}
	return Item(Spec{ID: "d-note", Name: NotesName, Kind: domain.KindNotebook}), nil
}

// PlanNotebook implements port.Pages: the notebook Notebook would make,
// without an identifier, and a failure of PutErr as the check's.
func (f *Fake) PlanNotebook(_ context.Context, local, dest string, o port.NotebookOptions) (domain.Item, error) {
	if err := f.pagesErr("PlanNotebook", local, dest, o.Name, string(o.Device), strings.Join(o.Tags, ",")); err != nil {
		return domain.Item{}, err
	}
	if err := f.PutErr[local]; err != nil {
		return domain.Item{}, err
	}
	return domain.Item{Name: NotesName, Kind: domain.KindNotebook}, nil
}

// secretsFake is the Fake as port.Secrets: Status would clash with nothing,
// but kept apart like the archives for symmetry of the doubles.
type secretsFake Fake

var _ port.Secrets = (*secretsFake)(nil)

// Status implements port.Secrets.
func (s *secretsFake) Status(context.Context) (port.SecretsStatus, error) {
	f := (*Fake)(s)
	if err := f.record("SecretsStatus"); err != nil {
		return port.SecretsStatus{}, err
	}
	return f.SecretsAt, f.SecretsErr
}

// Migrate implements port.Secrets.
func (s *secretsFake) Migrate(_ context.Context, to port.SecretsBackend, o port.MigrateOptions) (port.SecretsMigration, error) {
	f := (*Fake)(s)
	if err := f.record("Migrate", string(to), opt(o.Replace, "replace"), opt(o.SetDefault, "set-default")); err != nil {
		return port.SecretsMigration{}, err
	}
	return f.Migration, f.SecretsErr
}

// cacheFake is the Fake's port.Cache.
type cacheFake Fake

var _ port.Cache = (*cacheFake)(nil)

// Status implements port.Cache: the profiles of CacheAt, only the current
// one unless all.
func (c *cacheFake) Status(_ context.Context, all bool) (port.CacheReport, error) {
	f := (*Fake)(c)
	if err := f.record("CacheStatus", opt(all, "all")); err != nil {
		return port.CacheReport{}, err
	}
	r := f.CacheAt
	if !all {
		r.Profiles = slices.DeleteFunc(slices.Clone(r.Profiles), func(p port.ProfileCache) bool { return p.Profile != r.Profile })
	}
	return r, f.CacheErr
}

// Clear implements port.Cache.
func (c *cacheFake) Clear(ctx context.Context, all bool) ([]string, error) {
	f := (*Fake)(c)
	r, err := c.Status(ctx, all)
	if err != nil {
		return nil, err
	}
	if err := f.record("CacheClear", opt(all, "all")); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(r.Profiles))
	for _, p := range r.Profiles {
		names = append(names, p.Profile)
	}
	return names, nil
}

// profilesFake is the Fake's port.Profiles.
type profilesFake Fake

var _ port.Profiles = (*profilesFake)(nil)

// List implements port.Profiles.
func (p *profilesFake) List(context.Context) ([]port.ProfileSummary, error) {
	f := (*Fake)(p)
	if err := f.record("ProfileList"); err != nil {
		return nil, err
	}
	return f.ProfileList, f.ProfilesErr
}

// Info implements port.Profiles.
func (p *profilesFake) Info(_ context.Context, name string, account bool) (port.ProfileInfo, error) {
	f := (*Fake)(p)
	if err := f.record("ProfileInfo", name, opt(account, "account")); err != nil {
		return port.ProfileInfo{}, err
	}
	info := f.ProfileInfo
	if info.Name == "" {
		info.Name = name
	}
	return info, f.ProfilesErr
}

// Add implements port.Profiles.
func (p *profilesFake) Add(_ context.Context, name, code string) error {
	f := (*Fake)(p)
	if err := f.record("ProfileAdd", name, code); err != nil {
		return err
	}
	return f.ProfilesErr
}

// Remove implements port.Profiles.
func (p *profilesFake) Remove(_ context.Context, name string, o port.RemoveProfileOptions) (port.ProfileRemoval, error) {
	f := (*Fake)(p)
	if err := f.record("ProfileRemove", name, opt(o.Revoke, "revoke")); err != nil {
		return port.ProfileRemoval{}, err
	}
	return f.Removal, f.RemoveErr
}

// listingFake is the Fake's port.Completion.
type listingFake Fake

var _ port.Completion = (*listingFake)(nil)

// Listing implements port.Completion; it records no call.
func (l *listingFake) Listing(context.Context) (*domain.Tree, error) {
	f := (*Fake)(l)
	if f.NoListing {
		return nil, port.ErrNoListing
	}
	return f.tree(), nil
}

// viewingFake is the Fake's port.Viewing.
type viewingFake Fake

var _ port.Viewing = (*viewingFake)(nil)

// Open implements port.Viewing.
func (v *viewingFake) Open(_ context.Context, query string, o port.RenderOptions) (port.OpenReport, error) {
	return (*Fake)(v).opened("Open", query, o)
}

// OpenFile implements port.Viewing.
func (v *viewingFake) OpenFile(_ context.Context, path string, o port.RenderOptions) (port.OpenReport, error) {
	return (*Fake)(v).opened("OpenFile", path, o)
}

func (f *Fake) opened(method, from string, o port.RenderOptions) (port.OpenReport, error) {
	if err := f.record(method, renderArgs(from, "", o)...); err != nil {
		return port.OpenReport{}, err
	}
	if f.RenderErr != nil {
		return port.OpenReport{}, f.RenderErr
	}
	r := f.Opened
	if r.Path == "" {
		r.Path = "rendered/" + path.Base(from) + ".pdf"
	}
	return r, f.OpenErr
}
