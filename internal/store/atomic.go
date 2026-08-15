// Package store is the persistent local state of DebrisLedger: an append-only
// JSONL catalogue and work ledger, JSON plan snapshots, a metadata file and a
// SHA-256 hash-chained audit log.
//
// Everything is written through atomic replace: content goes to a temporary
// file in the destination directory, is flushed to stable storage, and is then
// renamed over the target. A crash therefore leaves either the old file or the
// new one, never a half-written one.
package store

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// filePerm and dirPerm are the permissions used for everything the store
// creates.
const (
	filePerm os.FileMode = 0o644
	dirPerm  os.FileMode = 0o755
)

// WriteFileAtomic writes data to path via a temporary file and a rename.
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-"+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("create temporary file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(filePerm); err != nil && !os.IsPermission(err) {
		cleanup()
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		_ = os.Remove(tmpName)
		return fmt.Errorf("replace %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename %s to %s: %w", tmpName, path, err)
	}
	return nil
}

// AppendLine appends one newline-terminated record to an append-only file.
// The file is opened in append mode and flushed before the handle is closed, so
// concurrent readers never see a partial record.
func AppendLine(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		data = append(append([]byte{}, data...), '\n')
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, filePerm)
	if err != nil {
		return fmt.Errorf("open %s for append: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("append to %s: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}

// HashBytes returns the lower-case hex SHA-256 of data.
func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// HashFile returns the SHA-256 of a file's contents together with its size.
func HashFile(path string) (string, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, fmt.Errorf("read %s: %w", path, err)
	}
	return HashBytes(data), len(data), nil
}

// HashStrings hashes a deterministic join of the given fields. Fields are
// joined with a byte that cannot appear inside them, so no two different field
// lists can produce the same input.
func HashStrings(fields ...string) string {
	return HashBytes([]byte(strings.Join(fields, "\x1f")))
}

// exists reports whether a path exists.
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// listFiles returns the sorted regular-file names inside dir. A missing
// directory yields an empty list rather than an error, which keeps the report
// commands usable on a fresh store.
func listFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".tmp-") {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out, nil
}
