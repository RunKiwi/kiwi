package checkpoint

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Snapshotter persists and restores a workspace directory as a
// content-addressed blob. LocalSnapshotter is the filesystem implementation
// used for local/dev runs; an S3-backed implementation (pkg/objstore) is a
// drop-in replacement behind this same interface (issue #36).
type Snapshotter interface {
	// Snapshot archives dir and returns a storage URI and a content hash.
	// The hash is deterministic: identical file contents produce an identical
	// hash regardless of timestamps or archive ordering.
	Snapshot(dir string) (uri string, hash string, err error)
	// Restore replaces the contents of dir with the blob at uri.
	Restore(uri string, dir string) error
}

// LocalSnapshotter writes content-hashed tar blobs under Root.
type LocalSnapshotter struct {
	Root string
}

func NewLocalSnapshotter(root string) *LocalSnapshotter {
	return &LocalSnapshotter{Root: root}
}

// Snapshot walks dir, archives every regular file with normalized (zeroed
// timestamp/owner) tar headers in sorted order, and hashes the archive bytes.
// Determinism is what makes the round-trip test (write -> restore -> identical
// hash) meaningful.
func (l *LocalSnapshotter) Snapshot(dir string) (string, string, error) {
	if err := os.MkdirAll(l.Root, 0o755); err != nil {
		return "", "", err
	}

	var files []string
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return "", "", err
	}
	sort.Strings(files)

	var buf bytes.Buffer
	h := sha256.New()
	tw := tar.NewWriter(io.MultiWriter(&buf, h))
	for _, p := range files {
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return "", "", err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return "", "", err
		}
		fi, err := os.Stat(p)
		if err != nil {
			return "", "", err
		}
		// mtime/uid/gid are intentionally omitted so the hash depends only on
		// path, permission bits, and content.
		hdr := &tar.Header{
			Name: filepath.ToSlash(rel),
			Mode: int64(fi.Mode().Perm()),
			Size: int64(len(data)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return "", "", err
		}
		if _, err := tw.Write(data); err != nil {
			return "", "", err
		}
	}
	if err := tw.Close(); err != nil {
		return "", "", err
	}

	hash := hex.EncodeToString(h.Sum(nil))
	uri := filepath.Join(l.Root, hash+".tar")
	if err := os.WriteFile(uri, buf.Bytes(), 0o644); err != nil {
		return "", "", err
	}
	return uri, hash, nil
}

// Restore empties dir and extracts the blob at uri into it, guarding against
// Zip-Slip path traversal.
func (l *LocalSnapshotter) Restore(uri string, dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	f, err := os.Open(uri)
	if err != nil {
		return err
	}
	defer f.Close()

	clean := filepath.Clean(dir)
	tr := tar.NewReader(f)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(hdr.Name))
		if target != clean && !strings.HasPrefix(target, clean+string(os.PathSeparator)) {
			return fmt.Errorf("illegal path in snapshot: %s", hdr.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, data, os.FileMode(hdr.Mode)); err != nil {
			return err
		}
	}
	return nil
}
