// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package library

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// maxManifest bounds the backup manifest read: far above any library.
const maxManifest = 256 << 20

// ReadManifest reads the manifest of the backup directory dir; found is
// false when it holds none.
func ReadManifest(files port.LocalFiles, dir string) (domain.BackupManifest, bool, error) {
	path := filepath.Join(dir, domain.BackupManifestName)
	_, isDir, exists, err := files.Stat(path)
	switch {
	case err != nil:
		return domain.BackupManifest{}, false, err
	case !exists:
		return domain.BackupManifest{}, false, nil
	case isDir:
		return domain.BackupManifest{}, false, fmt.Errorf("%s: %w", path, domain.ErrBackupManifest)
	}
	r, err := files.Open(path)
	if err != nil {
		return domain.BackupManifest{}, false, err
	}
	defer func() { _ = r.Close() }()
	var buf bytes.Buffer
	if _, err = io.Copy(&buf, io.LimitReader(r, maxManifest)); err != nil {
		return domain.BackupManifest{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	m, err := domain.ParseBackupManifest(buf.Bytes())
	if err != nil {
		return domain.BackupManifest{}, false, fmt.Errorf("%s: %w", path, err)
	}
	return m, true, nil
}
