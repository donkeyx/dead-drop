package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FS is a single-writer filesystem store.
type FS struct {
	root string
	mu   sync.Mutex // serializes Take/Create among goroutines in-process
	lock *dirLock
}

// OpenFS opens (or creates) a data directory and acquires an exclusive process lock.
// Startup reaps quarantine/* without restoring live ids.
func OpenFS(dataDir string) (*FS, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	for _, sub := range []string{"blobs", "meta", "quarantine"} {
		if err := os.MkdirAll(filepath.Join(dataDir, sub), 0o700); err != nil {
			return nil, err
		}
	}
	lock, err := acquireDirLock(filepath.Join(dataDir, ".lock"))
	if err != nil {
		return nil, err
	}
	fs := &FS{root: dataDir, lock: lock}
	if n, err := fs.reapQuarantine(); err != nil {
		_ = fs.Close()
		return nil, err
	} else if n > 0 {
		// best-effort; no metrics hook yet
		_ = n
	}
	return fs, nil
}

func (s *FS) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lock.release()
}

func (s *FS) Create(ctx context.Context, meta Meta, blob []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if meta.ID == "" {
		return fmt.Errorf("store: empty id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	blobPath := s.blobPath(meta.ID)
	metaPath := s.metaPath(meta.ID)
	if fileExists(blobPath) || fileExists(metaPath) {
		return ErrExists
	}
	if err := os.MkdirAll(filepath.Dir(blobPath), 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(metaPath), 0o700); err != nil {
		return err
	}

	meta.Size = int64(len(blob))
	mb, err := json.Marshal(metaJSONFrom(meta))
	if err != nil {
		return err
	}

	// write temp then rename
	if err := writeFileAtomic(blobPath, blob, 0o600); err != nil {
		return err
	}
	if err := writeFileAtomic(metaPath, mb, 0o600); err != nil {
		_ = os.Remove(blobPath)
		return err
	}
	c := s.readCounters()
	c.Created++
	if meta.HasPassphrase {
		c.Passphrase++
	}
	return s.writeCounters(c)
}

func (s *FS) Take(ctx context.Context, id string) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	metaPath := s.metaPath(id)
	blobPath := s.blobPath(id)
	if !fileExists(metaPath) || !fileExists(blobPath) {
		return Record{}, ErrNotFound
	}

	mb, err := os.ReadFile(metaPath)
	if err != nil {
		return Record{}, err
	}
	meta, err := metaFromJSON(mb)
	if err != nil {
		return Record{}, err
	}

	now := time.Now().UTC()
	if !meta.ExpiresAt.IsZero() && !now.Before(meta.ExpiresAt) {
		_ = os.Remove(blobPath)
		_ = os.Remove(metaPath)
		c := s.readCounters()
		c.Expired++
		_ = s.writeCounters(c)
		return Record{}, ErrNotFound
	}

	if meta.BurnAfterRead {
		// rename live → quarantine (only one wins)
		qBlob := filepath.Join(s.root, "quarantine", id+".seal")
		qMeta := filepath.Join(s.root, "quarantine", id+".json")
		if err := os.Rename(blobPath, qBlob); err != nil {
			if os.IsNotExist(err) {
				return Record{}, ErrNotFound
			}
			// rename race / already gone
			return Record{}, ErrNotFound
		}
		if err := os.Rename(metaPath, qMeta); err != nil {
			// try clean up blob quarantine
			_ = os.Remove(qBlob)
			return Record{}, ErrNotFound
		}
		blob, err := os.ReadFile(qBlob)
		// always unlink quarantine
		_ = os.Remove(qBlob)
		_ = os.Remove(qMeta)
		if err != nil {
			return Record{}, err
		}
		meta.Size = int64(len(blob))
		c := s.readCounters()
		c.Burned++
		_ = s.writeCounters(c)
		return Record{Meta: meta, Blob: blob}, nil
	}

	// multi-read: read in place
	blob, err := os.ReadFile(blobPath)
	if err != nil {
		return Record{}, err
	}
	meta.Size = int64(len(blob))
	return Record{Meta: meta, Blob: blob}, nil
}

func (s *FS) Get(ctx context.Context, id string) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	metaPath := s.metaPath(id)
	blobPath := s.blobPath(id)
	if !fileExists(metaPath) || !fileExists(blobPath) {
		return Record{}, ErrNotFound
	}
	mb, err := os.ReadFile(metaPath)
	if err != nil {
		return Record{}, err
	}
	meta, err := metaFromJSON(mb)
	if err != nil {
		return Record{}, err
	}
	now := time.Now().UTC()
	if !meta.ExpiresAt.IsZero() && !now.Before(meta.ExpiresAt) {
		return Record{}, ErrNotFound
	}
	blob, err := os.ReadFile(blobPath)
	if err != nil {
		return Record{}, err
	}
	meta.Size = int64(len(blob))
	return Record{Meta: meta, Blob: blob}, nil
}

func (s *FS) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = os.Remove(s.blobPath(id))
	_ = os.Remove(s.metaPath(id))
	return nil
}

func (s *FS) DeleteExpired(ctx context.Context, now time.Time) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	n := 0
	metaRoot := filepath.Join(s.root, "meta")
	err := filepath.Walk(metaRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".json" {
			return err
		}
		mb, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		meta, err := metaFromJSON(mb)
		if err != nil {
			return nil
		}
		if !meta.ExpiresAt.IsZero() && !now.Before(meta.ExpiresAt) {
			_ = os.Remove(s.blobPath(meta.ID))
			_ = os.Remove(s.metaPath(meta.ID))
			n++
		}
		return nil
	})
	if n > 0 {
		c := s.readCounters()
		c.Expired += int64(n)
		if werr := s.writeCounters(c); werr != nil && err == nil {
			err = werr
		}
	}
	return n, err
}

func (s *FS) Stats(ctx context.Context) (Counters, error) {
	if err := ctx.Err(); err != nil {
		return Counters{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readCounters(), nil
}

func (s *FS) Count(ctx context.Context) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	metaRoot := filepath.Join(s.root, "meta")
	_ = filepath.Walk(metaRoot, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && filepath.Ext(path) == ".json" {
			n++
		}
		return nil
	})
	return n, nil
}

func (s *FS) reapQuarantine() (int, error) {
	q := filepath.Join(s.root, "quarantine")
	entries, err := os.ReadDir(q)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		_ = os.Remove(filepath.Join(q, e.Name()))
		n++
	}
	return n, nil
}

func (s *FS) blobPath(id string) string {
	return filepath.Join(s.root, "blobs", shard(id), id+".seal")
}

func (s *FS) metaPath(id string) string {
	return filepath.Join(s.root, "meta", shard(id), id+".json")
}

func shard(id string) string {
	if len(id) >= 2 {
		return id[:2]
	}
	return "_0"
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

type metaJSON struct {
	ID            string    `json:"id"`
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	BurnAfterRead bool      `json:"burn_after_read"`
	Size          int64     `json:"size"`
	FormatVersion uint8     `json:"format_version"`
}

func (s *FS) statsPath() string { return filepath.Join(s.root, "stats.json") }

func (s *FS) readCounters() Counters {
	b, err := os.ReadFile(s.statsPath())
	if err != nil {
		return Counters{}
	}
	var c Counters
	if json.Unmarshal(b, &c) != nil {
		return Counters{}
	}
	return c
}

func (s *FS) writeCounters(c Counters) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return writeFileAtomic(s.statsPath(), b, 0o600)
}

func metaJSONFrom(m Meta) metaJSON {
	return metaJSON{
		ID:            m.ID,
		CreatedAt:     m.CreatedAt.UTC(),
		ExpiresAt:     m.ExpiresAt.UTC(),
		BurnAfterRead: m.BurnAfterRead,
		Size:          m.Size,
		FormatVersion: m.FormatVersion,
	}
}

func metaFromJSON(b []byte) (Meta, error) {
	var j metaJSON
	if err := json.Unmarshal(b, &j); err != nil {
		return Meta{}, err
	}
	return Meta{
		ID:            j.ID,
		CreatedAt:     j.CreatedAt.UTC(),
		ExpiresAt:     j.ExpiresAt.UTC(),
		BurnAfterRead: j.BurnAfterRead,
		Size:          j.Size,
		FormatVersion: j.FormatVersion,
	}, nil
}

// Ensure FS implements Store.
var _ Store = (*FS)(nil)
