// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package porttest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// ErrNoBlob is what Cloud returns for a hash it does not hold.
var ErrNoBlob = errors.New("porttest: no such blob")

// Cloud is an in-memory reMarkable cloud: content-addressed blobs, item
// indexes, a root with a generation, a registration and a notification
// feed. It implements every cloud port. It is safe for concurrent use.
//
// Failures are injected by method name through Fail, and root conflicts
// through Conflicts. Calls records the name of every call in order.
type Cloud struct {
	mu sync.Mutex

	blobs      map[string][]byte
	indexes    map[string][]domain.File
	records    []domain.Record
	rootHash   string
	generation int64

	// Fail makes the named method (Root, CommitRoot, Files, PutIndex, IndexByFile, Read,
	// Download, Upload, UploadFile, ReadRaw, Missing, IsRegistered,
	// Register, Forget, Revoke, Plan, Listen) return the error.
	Fail map[string]error
	// Conflicts is how many CommitRoot calls still answer ErrRootConflict.
	Conflicts int
	// OnConflict, when set, runs after each conflict Conflicts makes, with
	// the double unlocked: the change another writer made meanwhile.
	OnConflict func()
	// ReadLocal reads the file UploadFile is given; nil is os.ReadFile.
	ReadLocal func(path string) ([]byte, error)

	// Registered, ValidCode and AccountPlan drive the Registry.
	Registered  bool
	ValidCode   string
	AccountPlan port.Plan
	// Events are delivered by Listen, which then waits for the context.
	Events []port.Event

	calls []string
}

// NewCloud returns an empty cloud with no root yet.
func NewCloud() *Cloud {
	return &Cloud{
		blobs:   map[string][]byte{},
		indexes: map[string][]domain.File{},
		Fail:    map[string]error{},
	}
}

// cloudPorts is every driven port of the cloud: the double stands in for
// the whole adapter.
type cloudPorts interface {
	port.RootStore
	port.DocumentStore
	port.IndexProber
	port.BlobStore
	port.Registry
	port.Notifier
}

var _ cloudPorts = (*Cloud)(nil)

// Hash is the content address of data.
func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Calls is every method called so far, in order.
func (c *Cloud) Calls() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.calls)
}

// Count is how many times method was called.
func (c *Cloud) Count(method string) int {
	n := 0
	for _, m := range c.Calls() {
		if m == method {
			n++
		}
	}
	return n
}

// Generation is the root's current generation.
func (c *Cloud) Generation() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.generation
}

// Records is the current root.
func (c *Cloud) Records() []domain.Record {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.records)
}

// Blob returns the stored bytes of hash.
func (c *Cloud) Blob(hash string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, ok := c.blobs[hash]
	return slices.Clone(b), ok
}

// DropBlob forgets a blob, to make a read of it fail.
func (c *Cloud) DropBlob(hash string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.blobs, hash)
}

// begin records a call and returns its injected failure.
func (c *Cloud) begin(method string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, method)
	return c.Fail[method]
}

// Root implements port.RootStore.
func (c *Cloud) Root(context.Context) (port.RootState, error) {
	if err := c.begin("Root"); err != nil {
		return port.RootState{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return port.RootState{Hash: c.rootHash, Generation: c.generation, Records: slices.Clone(c.records)}, nil
}

// CommitRoot implements port.RootStore.
func (c *Cloud) CommitRoot(_ context.Context, base port.RootState, records []domain.Record) (port.RootState, error) {
	if err := c.begin("CommitRoot"); err != nil {
		return port.RootState{}, err
	}
	c.mu.Lock()
	if c.Conflicts > 0 {
		c.Conflicts--
		meanwhile := c.OnConflict
		c.mu.Unlock()
		if meanwhile != nil {
			meanwhile()
		}
		return port.RootState{}, port.ErrRootConflict
	}
	defer c.mu.Unlock()
	if base.Generation != c.generation || base.Hash != c.rootHash {
		return port.RootState{}, port.ErrRootConflict
	}
	c.records = slices.Clone(records)
	text := serialize(".", recordFiles(records))
	c.rootHash = Hash([]byte(text))
	c.blobs[c.rootHash] = []byte(text)
	c.generation++
	return port.RootState{Hash: c.rootHash, Generation: c.generation, Records: slices.Clone(records)}, nil
}

// IndexByFile implements port.IndexProber: true when the index stored under
// rec.Hash hashes to rec.Hash.
func (c *Cloud) IndexByFile(_ context.Context, rec domain.Record) (bool, error) {
	if err := c.begin("IndexByFile"); err != nil {
		return false, err
	}
	data, err := c.blob(rec.Hash)
	if err != nil {
		return false, err
	}
	return Hash(data) == rec.Hash, nil
}

// Readdress stores the index of rec again under hash, the way a node hash
// addresses an index, and returns the record that points at it.
func (c *Cloud) Readdress(rec domain.Record, hash string) domain.Record {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.blobs[hash] = c.blobs[rec.Hash]
	c.indexes[hash] = slices.Clone(c.indexes[rec.Hash])
	rec.Hash = hash
	return rec
}

// SetRoot replaces the root without a conflict check, for test setup.
func (c *Cloud) SetRoot(records []domain.Record) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = slices.Clone(records)
	c.rootHash = Hash([]byte(serialize(".", recordFiles(records))))
	c.generation++
}

// Files implements port.DocumentStore.
func (c *Cloud) Files(_ context.Context, rec domain.Record) ([]domain.File, error) {
	if err := c.begin("Files"); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	files, ok := c.indexes[rec.Hash]
	if !ok {
		return nil, fmt.Errorf("%w: index %s", ErrNoBlob, rec.Hash)
	}
	return slices.Clone(files), nil
}

// PutIndex implements port.DocumentStore. The index is serialized as schema
// 4, sorted by name, and addressed by the hash of those bytes.
func (c *Cloud) PutIndex(_ context.Context, id domain.ItemID, files []domain.File) (domain.Record, error) {
	if err := c.begin("PutIndex"); err != nil {
		return domain.Record{}, err
	}
	return c.storeIndex(id, files), nil
}

func (c *Cloud) storeIndex(id domain.ItemID, files []domain.File) domain.Record {
	text := serialize(string(id), files)
	hash := Hash([]byte(text))
	var size int64
	for _, f := range files {
		size += f.Size
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.blobs[hash] = []byte(text)
	c.indexes[hash] = slices.Clone(files)
	return domain.Record{ID: id, Hash: hash, Subfiles: len(files), Size: size}
}

// IndexText is the schema 4 serialization the cloud addresses an index by.
func IndexText(id domain.ItemID, files []domain.File) string {
	return serialize(string(id), files)
}

// Read implements port.BlobStore.
func (c *Cloud) Read(_ context.Context, f domain.File) ([]byte, error) {
	if err := c.begin("Read"); err != nil {
		return nil, err
	}
	return c.blob(f.Hash)
}

// Download implements port.BlobStore.
func (c *Cloud) Download(_ context.Context, f domain.File, w io.Writer, progress port.Progress) error {
	if err := c.begin("Download"); err != nil {
		return err
	}
	data, err := c.blob(f.Hash)
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	if progress != nil {
		progress(int64(len(data)), int64(len(data)))
	}
	return nil
}

// Upload implements port.BlobStore.
func (c *Cloud) Upload(_ context.Context, name string, body []byte) (domain.File, error) {
	if err := c.begin("Upload"); err != nil {
		return domain.File{}, err
	}
	return c.Put(name, body), nil
}

// Put stores body under name without recording a call, for test setup.
func (c *Cloud) Put(name string, body []byte) domain.File {
	hash := Hash(body)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.blobs[hash] = slices.Clone(body)
	return domain.File{Name: name, Hash: hash, Size: int64(len(body))}
}

// UploadFile implements port.BlobStore.
func (c *Cloud) UploadFile(_ context.Context, path, name string, progress port.Progress) (domain.File, error) {
	if err := c.begin("UploadFile"); err != nil {
		return domain.File{}, err
	}
	read := c.ReadLocal
	if read == nil {
		read = os.ReadFile
	}
	body, err := read(path)
	if err != nil {
		return domain.File{}, err
	}
	if progress != nil {
		progress(int64(len(body)), int64(len(body)))
	}
	return c.Put(name, body), nil
}

// ReadRaw implements port.BlobStore.
func (c *Cloud) ReadRaw(_ context.Context, hash, _ string) ([]byte, error) {
	if err := c.begin("ReadRaw"); err != nil {
		return nil, err
	}
	return c.blob(hash)
}

// Missing implements port.BlobStore.
func (c *Cloud) Missing(_ context.Context, hashes []string) ([]string, error) {
	if err := c.begin("Missing"); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var missing []string
	for _, h := range hashes {
		if _, ok := c.blobs[h]; !ok {
			missing = append(missing, h)
		}
	}
	return missing, nil
}

func (c *Cloud) blob(hash string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, ok := c.blobs[hash]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNoBlob, hash)
	}
	return slices.Clone(data), nil
}

// IsRegistered implements port.Registry.
func (c *Cloud) IsRegistered(context.Context) (bool, error) {
	if err := c.begin("IsRegistered"); err != nil {
		return false, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Registered, nil
}

// ErrInvalidCode is what Register returns for a code other than ValidCode.
var ErrInvalidCode = errors.New("porttest: invalid one-time code")

// Register implements port.Registry.
func (c *Cloud) Register(_ context.Context, code string) error {
	if err := c.begin("Register"); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if code != c.ValidCode {
		return ErrInvalidCode
	}
	c.Registered = true
	return nil
}

// Forget implements port.Registry.
func (c *Cloud) Forget(context.Context) error {
	if err := c.begin("Forget"); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Registered = false
	return nil
}

// Revoke implements port.Registry.
func (c *Cloud) Revoke(context.Context) error {
	if err := c.begin("Revoke"); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.Registered {
		return port.ErrNotRegistered
	}
	c.Registered = false
	return nil
}

// Plan implements port.Registry.
func (c *Cloud) Plan(context.Context) (port.Plan, error) {
	if err := c.begin("Plan"); err != nil {
		return port.Plan{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.Registered {
		return port.Plan{}, port.ErrNotRegistered
	}
	return c.AccountPlan, nil
}

// Listen implements port.Notifier: it delivers Events, then waits for the
// context and returns its error.
func (c *Cloud) Listen(ctx context.Context, on func(port.Event)) error {
	if err := c.begin("Listen"); err != nil {
		return err
	}
	c.mu.Lock()
	events := slices.Clone(c.Events)
	c.mu.Unlock()
	for _, e := range events {
		on(e)
	}
	<-ctx.Done()
	return ctx.Err()
}

func recordFiles(records []domain.Record) []domain.File {
	files := make([]domain.File, len(records))
	for i, r := range records {
		files[i] = domain.File{Name: string(r.ID), Hash: r.Hash, Size: r.Size}
	}
	return files
}

// serialize writes a schema 4 index: header, count line, then one sorted
// line per file. Subfiles of a file line are 0.
func serialize(owner string, files []domain.File) string {
	sorted := slices.Clone(files)
	slices.SortStableFunc(sorted, func(a, b domain.File) int { return strings.Compare(a.Name, b.Name) })
	var total int64
	for _, f := range sorted {
		total += f.Size
	}
	var b bytes.Buffer
	b.WriteString("4\n0:" + owner + ":" + strconv.Itoa(len(sorted)) + ":" + strconv.FormatInt(total, 10) + "\n")
	for _, f := range sorted {
		b.WriteString(f.Hash + ":0:" + f.Name + ":0:" + strconv.FormatInt(f.Size, 10) + "\n")
	}
	return b.String()
}
