// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/altessa-s/rmcloud-go"

	"github.com/altessa-s/go-atlas-core/io/files"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// fileType is the type every record this tool writes carries: the tablet
// reads "0" on every account, also those whose own records say 80000000.
const fileType = "0"

// rootName is the storage name of the root index.
const rootName = "root.docSchema"

// Cloud is the reMarkable cloud through rmcloud-go. It maps the transport's
// entries onto domain records and files and its errors onto the ports'; no
// library semantics live here.
type Cloud struct {
	client *rmcloud.Client
}

var (
	_ port.RootStore     = (*Cloud)(nil)
	_ port.DocumentStore = (*Cloud)(nil)
	_ port.IndexProber   = (*Cloud)(nil)
	_ port.BlobStore     = (*Cloud)(nil)
	_ port.Registry      = (*Cloud)(nil)
	_ port.Notifier      = (*Cloud)(nil)
)

// New returns the adapter over client. A nil client is a programming error
// and panics.
func New(client *rmcloud.Client) *Cloud {
	if client == nil {
		panic("cloud: a client is required")
	}
	return &Cloud{client: client}
}

// Root reads the root and its index. A library with no root yet is an empty
// root at generation 0.
func (c *Cloud) Root(ctx context.Context) (port.RootState, error) {
	root, index, err := c.client.RootIndex(ctx)
	if errors.Is(err, rmcloud.ErrNoRootYet) {
		return port.RootState{}, nil
	}
	if err != nil {
		return port.RootState{}, mapErr(err)
	}
	records := make([]domain.Record, 0, len(index.Entries))
	for _, e := range index.Entries {
		records = append(records, domain.Record{
			ID:       domain.ItemID(e.DocumentID),
			Hash:     e.Hash,
			Subfiles: e.Subfiles,
			Size:     e.Size,
		})
	}
	return port.RootState{Hash: root.Hash, Generation: root.Generation, Records: records}, nil
}

// CommitRoot uploads records as a root index and points the root at it, on
// top of base's generation, broadcasting the change to the user's devices.
// It returns the root it wrote.
func (c *Cloud) CommitRoot(ctx context.Context, base port.RootState, records []domain.Record) (port.RootState, error) {
	entries := make([]rmcloud.Entry, 0, len(records))
	for _, r := range records {
		entries = append(entries, rmcloud.Entry{
			Hash:       r.Hash,
			Type:       fileType,
			DocumentID: string(r.ID),
			Subfiles:   r.Subfiles,
			Size:       r.Size,
		})
	}
	hash, err := c.client.Upload(ctx, []byte(rmcloud.SerializeRoot(entries)), rootName)
	if err != nil {
		return port.RootState{}, mapErr(err)
	}
	generation, err := c.client.PutRoot(ctx, hash, base.Generation, rmcloud.Broadcast(true))
	if err != nil {
		return port.RootState{}, mapErr(err)
	}
	return port.RootState{Hash: hash, Generation: generation, Records: records}, nil
}

// IndexByFile reads the index of rec by its record hash. The transport
// checks every read against the hash it was asked for, so the read succeeds
// exactly when the record hash is the hash of the index file's own bytes;
// a node hash is a hash mismatch.
func (c *Cloud) IndexByFile(ctx context.Context, rec domain.Record) (bool, error) {
	_, err := c.client.Blob(ctx, rec.Hash, rmcloud.IndexName(string(rec.ID)))
	switch {
	case err == nil:
		return true, nil
	case rmcloud.IsKind(err, rmcloud.KindHashMismatch):
		return false, nil
	default:
		return false, mapErr(err)
	}
}

// Files reads the index of rec.
func (c *Cloud) Files(ctx context.Context, rec domain.Record) ([]domain.File, error) {
	index, err := c.client.Index(ctx, rec.Hash, rmcloud.IndexName(string(rec.ID)))
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]domain.File, 0, len(index.Entries))
	for _, e := range index.Entries {
		out = append(out, domain.File{Name: e.DocumentID, Hash: e.Hash, Size: e.Size})
	}
	return out, nil
}

// PutIndex uploads the index of id listing files. The record's hash is the
// hash of the index file's own bytes.
func (c *Cloud) PutIndex(ctx context.Context, id domain.ItemID, files []domain.File) (domain.Record, error) {
	entries := make([]rmcloud.Entry, 0, len(files))
	var size int64
	for _, f := range files {
		entries = append(entries, rmcloud.Entry{Hash: f.Hash, Type: fileType, DocumentID: f.Name, Size: f.Size})
		size += f.Size
	}
	hash, err := c.client.Upload(ctx, []byte(rmcloud.SerializeDocument(entries, string(id))), rmcloud.IndexName(string(id)))
	if err != nil {
		return domain.Record{}, mapErr(err)
	}
	return domain.Record{ID: id, Hash: hash, Subfiles: len(files), Size: size}, nil
}

// Read reads f and checks it against its hash.
func (c *Cloud) Read(ctx context.Context, f domain.File) ([]byte, error) {
	data, err := c.client.Blob(ctx, f.Hash, f.Name)
	return data, mapErr(err)
}

// Download streams f into w through a temporary file the transport checks
// against the hash before a byte reaches w.
func (c *Cloud) Download(ctx context.Context, f domain.File, w io.Writer, progress port.Progress) error {
	var expect rmcloud.Expectation
	if f.Size > 0 {
		expect = rmcloud.Expecting(f.Size)
	}
	var report rmcloud.Progress
	if progress != nil {
		report = rmcloud.Progress(progress)
	}
	path, err := c.client.BlobFile(ctx, f.Hash, f.Name, expect, report)
	if err != nil {
		return mapErr(err)
	}
	defer func() { _ = os.Remove(path) }()
	src, err := files.OpenRegular(path)
	if err != nil {
		return fmt.Errorf("open the download: %w", err)
	}
	defer func() { _ = src.Close() }()
	if _, err := io.Copy(w, src); err != nil {
		return fmt.Errorf("copy the download: %w", err)
	}
	return nil
}

// Upload stores body under name.
func (c *Cloud) Upload(ctx context.Context, name string, body []byte) (domain.File, error) {
	hash, err := c.client.Upload(ctx, body, name)
	if err != nil {
		return domain.File{}, mapErr(err)
	}
	return domain.File{Name: name, Hash: hash, Size: int64(len(body))}, nil
}

// UploadFile stores the local file at path under name.
func (c *Cloud) UploadFile(ctx context.Context, path, name string, progress port.Progress) (domain.File, error) {
	info, err := os.Stat(path)
	if err != nil {
		return domain.File{}, err
	}
	var report rmcloud.Progress
	if progress != nil {
		report = rmcloud.Progress(progress)
	}
	hash, err := c.client.UploadFile(ctx, path, name, report)
	if err != nil {
		return domain.File{}, mapErr(err)
	}
	return domain.File{Name: name, Hash: hash, Size: info.Size()}, nil
}

// ReadRaw reads a blob by hash. The transport checks every read against its
// hash, so this is Read under another name; a blob that does not match is
// an error rather than bytes.
func (c *Cloud) ReadRaw(ctx context.Context, hash, name string) ([]byte, error) {
	return c.Read(ctx, domain.File{Name: name, Hash: hash})
}

// Missing reports which of hashes the cloud does not hold.
func (c *Cloud) Missing(ctx context.Context, hashes []string) ([]string, error) {
	if len(hashes) == 0 {
		return nil, nil
	}
	missing, err := c.client.MissingFiles(ctx, hashes)
	return missing, mapErr(err)
}

// IsRegistered reports a stored device token.
func (c *Cloud) IsRegistered(ctx context.Context) (bool, error) {
	ok, err := c.client.IsRegistered(ctx)
	return ok, mapErr(err)
}

// Register exchanges a one-time code for a device token.
func (c *Cloud) Register(ctx context.Context, code string) error {
	return mapErr(c.client.RegisterDevice(ctx, code))
}

// Forget drops the stored tokens.
func (c *Cloud) Forget(ctx context.Context) error { return mapErr(c.client.ForgetDevice(ctx)) }

// Revoke removes the device from the account at the cloud. The stored
// tokens stay until Forget: logout calls both.
func (c *Cloud) Revoke(ctx context.Context) error { return mapErr(c.client.RevokeDevice(ctx)) }

// Plan reads the subscription from the user token.
func (c *Cloud) Plan(ctx context.Context) (port.Plan, error) {
	plan, err := c.client.Plan(ctx)
	if err != nil {
		return port.Plan{}, mapErr(err)
	}
	var out port.Plan
	if plan != nil {
		if plan.Level != nil {
			out.Level = *plan.Level
		}
		if plan.SyncScope != nil {
			out.SyncScope = *plan.SyncScope
		}
	}
	return out, nil
}

// Listen delivers notifications until ctx ends.
func (c *Cloud) Listen(ctx context.Context, on func(port.Event)) error {
	c.client.StartNotifications(ctx, func(n rmcloud.Notification) {
		if on != nil {
			on(event(n))
		}
	})
	<-ctx.Done()
	c.client.StopNotifications()
	return nil
}

func event(n rmcloud.Notification) port.Event {
	ev := port.Event{
		Name:           n.Name,
		SyncComplete:   n.SyncComplete,
		DocumentID:     domain.ItemID(n.DocumentID),
		VisibleName:    n.VisibleName,
		FromThisDevice: n.FromThisDevice,
		Frame:          n.Frame,
		Err:            n.Err,
	}
	switch n.Kind {
	case rmcloud.NotificationConnected:
		ev.Kind = port.EventConnected
	case rmcloud.NotificationDropped:
		ev.Kind = port.EventDropped
	case rmcloud.NotificationUnreadable:
		ev.Kind = port.EventUnreadable
	default:
		ev.Kind = port.EventLibrary
	}
	return ev
}

// mapErr names the transport failures the use cases branch on with the
// ports' errors, keeping the transport error in the chain.
func mapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case rmcloud.IsKind(err, rmcloud.KindRootConflict):
		return fmt.Errorf("%w: %w", port.ErrRootConflict, err)
	case rmcloud.IsKind(err, rmcloud.KindNotRegistered):
		return fmt.Errorf("%w: %w", port.ErrNotRegistered, err)
	default:
		return err
	}
}
