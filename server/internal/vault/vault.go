// Package vault owns the on-disk vault: traversal, safe path handling and
// atomic writes. It knows nothing about markdown semantics.
package vault

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrNotFound  = errors.New("file not found")
	ErrOutside   = errors.New("path escapes the vault root")
	ErrNotVault  = errors.New("vault root is not a directory")
	ErrTooLarge  = errors.New("file too large")
	MaxFileBytes = int64(8 << 20)
)

// Codec transforms file contents on their way to and from disk. It is the seam
// that makes an encrypted vault invisible to everything above this package
// (ADR-0005); a nil codec means the vault is stored as plain markdown.
type Codec interface {
	Encode(rel string, plain []byte) ([]byte, error)
	Decode(rel string, stored []byte) ([]byte, error)
}

// Vault is a directory of markdown files.
type Vault struct {
	Root  string
	codec Codec
}

// Open validates the root directory.
func Open(root string) (*Vault, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, ErrNotVault
	}
	return &Vault{Root: abs}, nil
}

// SetCodec installs an at-rest transform. Call it before the first read.
func (v *Vault) SetCodec(c Codec) { v.codec = c }

// Encrypted reports whether contents are transformed on disk.
func (v *Vault) Encrypted() bool { return v.codec != nil }

// FileInfo is one entry produced by Walk.
type FileInfo struct {
	Path    string // relative, slash-separated
	Size    int64
	ModTime time.Time
}

// IsHidden reports paths Lumen never indexes: dotfiles, dot-directories and
// the local cache.
func IsHidden(rel string) bool {
	for _, part := range strings.Split(rel, "/") {
		if part != "" && strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

// Abs resolves a vault-relative path, refusing anything outside the root.
func (v *Vault) Abs(rel string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash("/" + strings.TrimPrefix(rel, "/")))
	abs := filepath.Join(v.Root, clean)
	rp, err := filepath.Rel(v.Root, abs)
	if err != nil || rp == ".." || strings.HasPrefix(rp, ".."+string(filepath.Separator)) {
		return "", ErrOutside
	}
	return abs, nil
}

// Rel converts an absolute path back to a slash-separated vault path.
func (v *Vault) Rel(abs string) (string, error) {
	rp, err := filepath.Rel(v.Root, abs)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rp), nil
}

// Walk visits every markdown file in the vault, skipping hidden entries.
func (v *Vault) Walk(fn func(FileInfo) error) error {
	return filepath.WalkDir(v.Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // an unreadable entry must not abort the whole scan
		}
		rel, rerr := v.Rel(p)
		if rerr != nil {
			return nil
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if IsHidden(rel) {
				return fs.SkipDir
			}
			return nil
		}
		if IsHidden(rel) || !strings.EqualFold(filepath.Ext(p), ".md") {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		return fn(FileInfo{Path: rel, Size: info.Size(), ModTime: info.ModTime()})
	})
}

// Read returns the decoded contents of a vault file.
func (v *Vault) Read(rel string) ([]byte, error) {
	data, err := v.ReadRaw(rel)
	if err != nil {
		return nil, err
	}
	if v.codec == nil {
		return data, nil
	}
	return v.codec.Decode(rel, data)
}

// ReadRaw returns the bytes exactly as they sit on disk, bypassing the codec.
// Only migration and sync need this.
func (v *Vault) ReadRaw(rel string) ([]byte, error) {
	abs, err := v.Abs(rel)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, rel)
		}
		return nil, err
	}
	if st.Size() > MaxFileBytes {
		return nil, ErrTooLarge
	}
	return os.ReadFile(abs)
}

// Write encodes and replaces a file atomically.
func (v *Vault) Write(rel string, data []byte) error {
	if v.codec != nil {
		encoded, err := v.codec.Encode(rel, data)
		if err != nil {
			return err
		}
		data = encoded
	}
	return v.WriteRaw(rel, data)
}

// WriteRaw replaces a file atomically without applying the codec: a temporary
// file in the same directory is synced and renamed over the target, so a crash
// can never leave a partial note.
func (v *Vault) WriteRaw(rel string, data []byte) error {
	abs, err := v.Abs(rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(abs), ".lumen-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, abs)
}

// Trash moves a file into .lumen/trash instead of deleting it outright.
func (v *Vault) Trash(rel string) error {
	abs, err := v.Abs(rel)
	if err != nil {
		return err
	}
	dst := filepath.Join(v.Root, ".lumen", "trash",
		time.Now().Format("2006-01-02"), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(abs, dst); err == nil {
		return nil
	}
	// Rename can fail across devices; fall back to copy + remove.
	in, err := os.Open(abs)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Remove(abs)
}

// Stat returns the on-disk size and modification time of a vault file.
func (v *Vault) Stat(rel string) (FileInfo, error) {
	abs, err := v.Abs(rel)
	if err != nil {
		return FileInfo{}, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return FileInfo{}, fmt.Errorf("%w: %s", ErrNotFound, rel)
		}
		return FileInfo{}, err
	}
	return FileInfo{Path: rel, Size: st.Size(), ModTime: st.ModTime()}, nil
}

// Exists reports whether a vault-relative file is present.
func (v *Vault) Exists(rel string) bool {
	abs, err := v.Abs(rel)
	if err != nil {
		return false
	}
	_, err = os.Stat(abs)
	return err == nil
}
