// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package mirror

import (
	"cmp"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// stepKind is what a step of a pass does.
type stepKind int

// The steps of a pass, in the order they are carried out.
const (
	stepMoveLocal stepKind = iota + 1
	stepMoveCloud
	stepRemove
	stepRemoveDrawing
	stepTrash
	stepUpload
	stepUpdate
	stepAdopt
	stepPull
	stepRefresh
	stepDetach
	stepForget
	stepSkip
)

// step is one change a pass makes.
type step struct {
	kind stepKind
	id   domain.ItemID
	// path is the local file, and to where a move takes it.
	path, to string
	file     *localFile
	doc      cloudDoc
	// conflict marks an update of a document changed on the tablet too,
	// and replace one that replaces the document whole.
	conflict, replace bool
	detail            string
}

// deletion reports whether the step deletes on one side what the other
// side deleted; down is a deletion of local files.
func (s step) deletion() (deletes, down bool) {
	switch s.kind {
	case stepRemove, stepRemoveDrawing:
		return true, true
	case stepTrash:
		return true, false
	default:
		return false, false
	}
}

// stillWriting is why a file written less than settle ago is skipped.
const stillWriting = "still being written"

// planner works out the steps of one pass from the journal, the cloud and
// the local files.
type planner struct {
	j     *journal
	snap  *snapshot
	local local
	now   time.Time
	spec  port.MirrorSpec
	// hash reads a file's SHA-256; a file that cannot be read is skipped.
	hash func(*localFile) (string, error)

	claimedLocal map[string]bool
	claimedCloud map[domain.ItemID]bool
	steps        []step
}

// plan is every step of the pass, in the order they are carried out.
func (p *planner) plan() []step {
	p.claimedLocal = map[string]bool{}
	p.claimedCloud = map[domain.ItemID]bool{}
	var missing []domain.ItemID
	for _, id := range slices.Sorted(maps.Keys(p.j.Items)) {
		if p.tracked(id) {
			missing = append(missing, id)
		}
	}
	p.localRenames(missing)
	p.newFiles()
	if p.spec.PullNew {
		p.tabletDocuments()
	}
	slices.SortStableFunc(p.steps, func(a, b step) int { return cmp.Or(cmp.Compare(a.kind, b.kind), strings.Compare(a.path, b.path)) })
	return p.steps
}

// tracked plans the document id of the journal. It reports a document
// whose local file is gone, which may have been renamed locally: that is
// decided once every tracked file is claimed.
func (p *planner) tracked(id domain.ItemID) (missing bool) {
	e := p.j.Items[id]
	p.claimedCloud[id] = true
	c, inCloud := p.snap.docs[id]
	if e.Detached {
		f, inLocal := p.local.find(e.Path)
		if inLocal {
			p.claimedLocal[f.Path] = true
		}
		if !inCloud && !inLocal {
			p.add(step{kind: stepForget, id: id, path: e.Path})
		}
		return false
	}
	if e.Path == "" {
		if !inCloud {
			p.add(step{kind: stepRemoveDrawing, id: id, path: e.Output})
		}
		return false
	}
	cur := e.Path
	f, inLocal := p.local.find(cur)
	if inLocal {
		cur = f.Path
		p.claimedLocal[cur] = true
	}
	if inLocal && inCloud && (!sameName(c.item.Name, e.Name) || c.item.Parent != e.Parent) {
		want := c.localPath(strings.TrimPrefix(path.Ext(cur), "."))
		if _, taken := p.local.find(want); !sameName(want, cur) && !p.claimedLocal[want] && !taken {
			p.add(step{kind: stepMoveLocal, id: id, path: cur, to: want, file: f, doc: c})
			p.claimedLocal[want] = true
			cur = want
		}
	}
	switch {
	case !inLocal && !inCloud:
		p.add(step{kind: stepForget, id: id, path: e.Path})
	case !inLocal:
		return true
	case !inCloud:
		p.add(step{kind: stepRemove, id: id, path: cur, file: f})
	default:
		p.changed(id, e, cur, f, c)
	}
	return false
}

// changed plans a document in step on both sides before: an update when
// its file changed, a refresh of the journal when anything else did.
func (p *planner) changed(id domain.ItemID, e entry, cur string, f *localFile, c cloudDoc) {
	cloudChanged := domain.DrawnFingerprint(c.item) != e.Content
	refresh := step{kind: stepRefresh, id: id, path: cur, file: f, doc: c}
	if f.sameStat(e) {
		if c.item.Hash != e.Record || cur != e.Path {
			p.add(refresh)
		}
		return
	}
	if !f.settled(p.now) {
		p.add(step{kind: stepSkip, id: id, path: cur, detail: stillWriting})
		return
	}
	sum, err := p.hash(f)
	if err != nil {
		p.add(step{kind: stepSkip, id: id, path: cur, detail: "cannot be read: " + err.Error()})
		return
	}
	if sum == e.SHA256 {
		p.add(refresh)
		return
	}
	if !strings.EqualFold(path.Ext(cur), ".pdf") {
		// An EPUB has no update that keeps annotations: one without any is
		// replaced whole; one with them is left as it is.
		if domain.Annotated(c.item) {
			p.add(step{kind: stepSkip, id: id, path: cur, detail: "an EPUB changed locally, annotated on the tablet: replacing it would lose " +
				"its annotations; rmctl put --replace replaces it by hand"})
			return
		}
		p.add(step{kind: stepUpdate, id: id, path: cur, file: f, doc: c, replace: true})
		return
	}
	p.add(step{kind: stepUpdate, id: id, path: cur, file: f, doc: c, conflict: cloudChanged})
}

// localRenames plans the documents whose local file is gone: one whose
// file turns up under another path, with the same content, was renamed or
// moved locally and follows; any other was deleted, and goes to the trash.
func (p *planner) localRenames(missing []domain.ItemID) {
	bySum := map[string][]*localFile{}
	if len(missing) > 0 {
		for _, rel := range p.local.paths() {
			f := p.local.files[rel]
			if p.claimedLocal[rel] || !f.settled(p.now) {
				continue
			}
			if sum, err := p.hash(f); err == nil {
				bySum[sum] = append(bySum[sum], f)
			}
		}
	}
	for _, id := range missing {
		e := p.j.Items[id]
		c, inCloud := p.snap.docs[id]
		if found := bySum[e.SHA256]; inCloud && len(found) == 1 && !p.claimedLocal[found[0].Path] {
			p.claimedLocal[found[0].Path] = true
			p.add(step{kind: stepMoveCloud, id: id, path: e.Path, to: found[0].Path, file: found[0], doc: c})
			continue
		}
		if !inCloud {
			p.add(step{kind: stepForget, id: id, path: e.Path})
			continue
		}
		p.add(step{kind: stepTrash, id: id, path: e.Path, doc: c})
	}
}

// newFiles plans the files no document follows: each pairs with the
// document of its name already in its folder, or is uploaded.
func (p *planner) newFiles() {
	byPath := map[string]domain.ItemID{}
	for _, id := range p.snap.sorted() {
		c := p.snap.docs[id]
		ext := c.item.Kind.Extension()
		if p.claimedCloud[id] || ext == "" {
			continue
		}
		key := foldName(c.localPath(ext))
		if _, taken := byPath[key]; !taken {
			byPath[key] = id
		}
	}
	for _, rel := range p.local.paths() {
		if p.claimedLocal[rel] {
			continue
		}
		f := p.local.files[rel]
		if !f.settled(p.now) {
			p.add(step{kind: stepSkip, path: rel, detail: stillWriting})
			continue
		}
		if _, err := p.hash(f); err != nil {
			p.add(step{kind: stepSkip, path: rel, detail: "cannot be read: " + err.Error()})
			continue
		}
		if id, ok := byPath[foldName(rel)]; ok && !p.claimedCloud[id] {
			p.claimedCloud[id] = true
			p.add(step{kind: stepAdopt, id: id, path: rel, file: f, doc: p.snap.docs[id]})
			continue
		}
		p.add(step{kind: stepUpload, path: rel, file: f})
	}
}

// tabletDocuments plans the documents of the folder no file follows: with
// PullNew each is drawn into the annotated directory.
func (p *planner) tabletDocuments() {
	for _, id := range p.snap.sorted() {
		if p.claimedCloud[id] {
			continue
		}
		p.claimedCloud[id] = true
		p.add(step{kind: stepPull, id: id, doc: p.snap.docs[id]})
	}
}

func (p *planner) add(s step) { p.steps = append(p.steps, s) }

// The mass-deletion guard: a pass refuses the deletions of one side when
// they are more than MaxDeletes, or more than half of the documents
// followed (two at least), or when that side looks unmounted or emptied.
const (
	// MaxDeletes is the most deletions one side takes in a pass.
	MaxDeletes = 50
	// minShare is the fewest deletions the half-of-everything rule refuses:
	// one document deleted out of one or two is a deletion like any other.
	minShare = 2
)

// guard is why the deletions of a pass on one side are refused, or "".
// empty says that side holds nothing at all.
func guard(deletions, tracked int, empty bool, side string) string {
	switch {
	case deletions == 0:
		return ""
	case empty:
		return "the " + side + " is empty: it may not be mounted or synced; nothing is deleted on the other side"
	case deletions > MaxDeletes:
		return pluralDeletions(deletions) + " is more than " + strconv.Itoa(MaxDeletes) + " at once"
	case deletions >= minShare && deletions*2 > tracked:
		return pluralDeletions(deletions) + " is more than half of the " + strconv.Itoa(tracked) + " documents followed"
	}
	return ""
}

func pluralDeletions(n int) string {
	if n == 1 {
		return "1 deletion"
	}
	return strconv.Itoa(n) + " deletions"
}
