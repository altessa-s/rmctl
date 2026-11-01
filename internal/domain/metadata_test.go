// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/domain"
)

var at = time.UnixMilli(1730000000000).UTC()

func TestNewFolderMetadataBytes(t *testing.T) {
	t.Parallel()
	got := string(domain.NewFolderMetadata("Books", "", at).Bytes())
	want := `{"deleted":false,"lastModified":"1730000000000","metadatamodified":false,"modified":false,` +
		`"parent":"","pinned":false,"synced":true,"type":"CollectionType","version":1,"visibleName":"Books"}`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestNewDocumentMetadataBytes(t *testing.T) {
	t.Parallel()
	got := string(domain.NewDocumentMetadata("Paper", "f1", at).Bytes())
	want := `{"createdTime":"1730000000000","deleted":false,"lastModified":"1730000000000","lastOpened":"",` +
		`"lastOpenedPage":-1,"metadatamodified":false,"modified":false,"new":true,"parent":"f1","pinned":false,` +
		`"source":"","synced":true,"type":"DocumentType","version":1,"visibleName":"Paper"}`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestParseMetadata(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in   string
		wantErr    bool
		visible    string
		parent     domain.ItemID
		trashed    bool
		pinned     bool
		modified   time.Time
		created    time.Time
		folderType bool
	}{
		{name: "folder", in: `{"visibleName":"A","type":"CollectionType","parent":"p"}`, visible: "A", parent: "p", folderType: true},
		{name: "trash parent", in: `{"visibleName":"A","type":"DocumentType","parent":"trash"}`, visible: "A", trashed: true},
		{name: "deleted flag", in: `{"visibleName":"A","type":"DocumentType","deleted":true}`, visible: "A", trashed: true},
		{
			name: "times", in: `{"visibleName":"A","type":"DocumentType","lastModified":"1730000000000","createdTime":"1730000000000"}`,
			visible: "A", modified: at, created: at,
		},
		{name: "bad time", in: `{"visibleName":"A","type":"DocumentType","lastModified":"soon"}`, visible: "A"},
		{name: "number time is not a stamp", in: `{"visibleName":"A","type":"DocumentType","lastModified":1}`, visible: "A"},
		{name: "pinned", in: `{"visibleName":"A","type":"DocumentType","pinned":true}`, visible: "A", pinned: true},
		{name: "no name", in: `{"type":"DocumentType"}`, wantErr: true},
		{name: "no type", in: `{"visibleName":"A"}`, wantErr: true},
		{name: "name not a string", in: `{"visibleName":1,"type":"DocumentType"}`, wantErr: true},
		{name: "array", in: `[]`, wantErr: true},
		{name: "null", in: `null`, wantErr: true},
		{name: "duplicate key", in: `{"visibleName":"A","visibleName":"B","type":"DocumentType"}`, wantErr: true},
		{name: "garbage", in: `{`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, err := domain.ParseMetadata([]byte(tt.in))
			if tt.wantErr {
				var malformed *domain.MalformedFileError
				if !errors.As(err, &malformed) || malformed.File != "metadata" || !strings.Contains(err.Error(), "metadata") {
					t.Fatalf("err %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if m.VisibleName() != tt.visible || m.ParentID() != tt.parent || m.Trashed() != tt.trashed ||
				m.Pinned() != tt.pinned || !m.LastModified().Equal(tt.modified) || !m.Created().Equal(tt.created) ||
				(m.Type() == domain.TypeCollection) != tt.folderType {
				t.Fatalf("%+v", m)
			}
		})
	}
}

func TestMetadataEditsKeepUnknownFields(t *testing.T) {
	t.Parallel()
	in := `{"visibleName":"A","type":"DocumentType","parent":"","version":3,"metadatamodified":false,"zzz":{"keep":[1,2]}}`
	m, err := domain.ParseMetadata([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	later := at.Add(time.Hour)
	tests := []struct {
		name string
		edit domain.Metadata
		want string
	}{
		{
			name: "rename", edit: m.Renamed("B", later),
			want: `{"lastModified":"1730003600000","metadatamodified":true,"parent":"","type":"DocumentType","version":4,"visibleName":"B","zzz":{"keep":[1,2]}}`,
		},
		{
			name: "move", edit: m.Moved("f", later),
			want: `{"lastModified":"1730003600000","metadatamodified":true,"parent":"f","type":"DocumentType","version":4,"visibleName":"A","zzz":{"keep":[1,2]}}`,
		},
		{
			name: "trash leaves deleted alone", edit: m.MovedToTrash(later),
			want: `{"lastModified":"1730003600000","metadatamodified":true,"parent":"trash","type":"DocumentType","version":4,"visibleName":"A","zzz":{"keep":[1,2]}}`,
		},
		{
			name: "restore", edit: m.Restored("", later),
			want: `{"deleted":false,"lastModified":"1730003600000","metadatamodified":true,"parent":"","type":"DocumentType","version":4,"visibleName":"A","zzz":{"keep":[1,2]}}`,
		},
		{
			name: "pin", edit: m.WithPinned(true, later),
			want: `{"lastModified":"1730003600000","metadatamodified":true,"parent":"","pinned":true,"type":"DocumentType","version":4,"visibleName":"A","zzz":{"keep":[1,2]}}`,
		},
	}
	for _, tt := range tests {
		if got := string(tt.edit.Bytes()); got != tt.want {
			t.Errorf("%s:\n got %s\nwant %s", tt.name, got, tt.want)
		}
	}
	if got := string(m.Bytes()); got != `{"metadatamodified":false,"parent":"","type":"DocumentType","version":3,"visibleName":"A","zzz":{"keep":[1,2]}}` {
		t.Fatalf("an edit changed the original: %s", got)
	}
}

// version and metadatamodified are optional, and an edit must not add them.
func TestEditAddsNoOptionalFields(t *testing.T) {
	t.Parallel()
	m, err := domain.ParseMetadata([]byte(`{"visibleName":"A","type":"DocumentType","version":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	got := string(m.Renamed("B", at).Bytes())
	if want := `{"lastModified":"1730000000000","type":"DocumentType","version":"x","visibleName":"B"}`; got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestWithPinnedUnchanged(t *testing.T) {
	t.Parallel()
	m, err := domain.ParseMetadata([]byte(`{"visibleName":"A","type":"DocumentType","pinned":true,"version":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := m.WithPinned(true, at.Add(time.Hour)); string(got.Bytes()) != string(m.Bytes()) {
		t.Fatalf("an unchanged pin was rewritten: %s", got.Bytes())
	}
}

func FuzzParseMetadata(f *testing.F) {
	f.Add(`{"visibleName":"A","type":"DocumentType","parent":"trash","version":1}`)
	f.Add(`{"visibleName":"","type":"CollectionType","deleted":true,"pinned":false,"x":null}`)
	f.Add(`{"visibleName":"A"}`)
	f.Fuzz(func(t *testing.T, in string) {
		m, err := domain.ParseMetadata([]byte(in))
		if err != nil {
			return
		}
		// What decodes encodes, decodes again to the same thing, and an
		// edit keeps it decodable.
		again, err := domain.ParseMetadata(m.Bytes())
		if err != nil || string(again.Bytes()) != string(m.Bytes()) {
			t.Fatalf("round trip: %v\n%s\n%s", err, m.Bytes(), again.Bytes())
		}
		if _, err := domain.ParseMetadata(m.Renamed("n", at).Bytes()); err != nil {
			t.Fatalf("an edit broke the metadata: %v", err)
		}
	})
}
