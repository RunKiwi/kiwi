package checkpoint

import (
	"archive/tar"
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
	// The hash is deterministic: identical trees produce an identical hash
	// regardless of timestamps, ownership, or filesystem walk order.
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

// entry is one archivable node (directory or regular file), captured during the
// walk so the actual write can happen in a deterministic, sorted order.
type entry struct {
	rel   string
	abs   string
	isDir bool
	mode  os.FileMode
}

// Snapshot streams a deterministic tar of dir to a temp file (hashing as it
// writes via io.MultiWriter), then atomically renames it to <hash>.tar. It
// never buffers the whole archive or whole files in memory, so a multi-hundred-MB
// workspace does not cause an OOM spike. Directory entries (including empty
// directories) are archived so the tree is restored exactly. mtime/uid/gid are
// omitted so the hash depends only on path, permission bits, and content.
func (l *LocalSnapshotter) Snapshot(dir string) (string, string, error) {
	if err := os.MkdirAll(l.Root, 0o755); err != nil {
		return "", "", err
	}

	var entries []entry
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil // the root itself is implied by the target dir
		}
		switch {
		case info.IsDir():
			entries = append(entries, entry{rel: rel, abs: p, isDir: true, mode: info.Mode().Perm()})
		case info.Mode().IsRegular():
			entries = append(entries, entry{rel: rel, abs: p, mode: info.Mode().Perm()})
			// Non-regular, non-dir nodes (symlinks, sockets, devices) are skipped.
		}
		return nil
	})
	if err != nil {
		return "", "", err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })

	tmp, err := os.CreateTemp(l.Root, ".snap-*.tmp")
	if err != nil {
		return "", "", err
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()

	h := sha256.New()
	tw := tar.NewWriter(io.MultiWriter(tmp, h))
	for _, e := range entries {
		hdr := &tar.Header{Name: filepath.ToSlash(e.rel), Mode: int64(e.mode)}
		if e.isDir {
			hdr.Typeflag = tar.TypeDir
			hdr.Name += "/"
			if err := tw.WriteHeader(hdr); err != nil {
				return "", "", err
			}
			continue
		}
		fi, err := os.Stat(e.abs)
		if err != nil {
			return "", "", err
		}
		hdr.Typeflag = tar.TypeReg
		hdr.Size = fi.Size()
		if err := tw.WriteHeader(hdr); err != nil {
			return "", "", err
		}
		f, err := os.Open(e.abs)
		if err != nil {
			return "", "", err
		}
		if _, err := io.Copy(tw, f); err != nil {
			f.Close()
			return "", "", err
		}
		f.Close()
	}
	if err := tw.Close(); err != nil {
		return "", "", err
	}
	if err := tmp.Close(); err != nil {
		return "", "", err
	}

	hash := hex.EncodeToString(h.Sum(nil))
	uri := filepath.Join(l.Root, hash+".tar")
	if err := os.Rename(tmpName, uri); err != nil {
		return "", "", err
	}
	committed = true
	return uri, hash, nil
}

// Restore empties dir and extracts the blob at uri into it, streaming each file
// (no whole-file buffering) and guarding against Zip-Slip path traversal.
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
		if hdr.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode))
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
	}
	return nil
}
