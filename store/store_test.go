package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFSTakeBurnRace(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	runTakeBurnRace(t, s)
}

func TestSQLiteTakeBurnRace(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenSQLite(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	runTakeBurnRace(t, s)
}

func TestPostgresTakeBurnRace(t *testing.T) {
	dsn := os.Getenv("DEADDROP_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set DEADDROP_TEST_DATABASE_URL to run postgres integration tests")
	}
	s, err := OpenPostgres(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.db.Exec(`TRUNCATE secrets`); err != nil {
		t.Fatal(err)
	}
	runTakeBurnRace(t, s)
}

func runTakeBurnRace(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()
	id := "AbCdEfGhIjKlMnOpQrStUv"
	meta := Meta{
		ID:            id,
		CreatedAt:     time.Now().UTC(),
		ExpiresAt:     time.Now().UTC().Add(time.Hour),
		BurnAfterRead: true,
		FormatVersion: 1,
	}
	blob := []byte("SEAL-fake-blob-for-store-test")
	if err := s.Create(ctx, meta, blob); err != nil {
		t.Fatal(err)
	}

	const n = 32
	var ok atomic.Int32
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			rec, err := s.Take(ctx, id)
			if err == nil {
				ok.Add(1)
				if string(rec.Blob) != string(blob) {
					t.Errorf("blob mismatch")
				}
			} else if !errors.Is(err, ErrNotFound) {
				t.Errorf("unexpected err: %v", err)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 {
		t.Fatalf("expected exactly 1 successful Take, got %d", ok.Load())
	}
	// second wave still not found
	if _, err := s.Take(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found after burn, got %v", err)
	}
}

func TestTakeMultiRead(t *testing.T) {
	for _, name := range []string{"fs", "sqlite"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			var s Store
			var err error
			if name == "fs" {
				s, err = OpenFS(dir)
			} else {
				s, err = OpenSQLite(dir)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()

			ctx := context.Background()
			id := "multiReadId1234567890ab"
			meta := Meta{
				ID:            id,
				CreatedAt:     time.Now().UTC(),
				ExpiresAt:     time.Now().UTC().Add(time.Hour),
				BurnAfterRead: false,
				FormatVersion: 1,
			}
			blob := []byte("multi")
			if err := s.Create(ctx, meta, blob); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 3; i++ {
				rec, err := s.Take(ctx, id)
				if err != nil {
					t.Fatalf("take %d: %v", i, err)
				}
				if string(rec.Blob) != "multi" {
					t.Fatal(rec.Blob)
				}
			}
		})
	}
}

func TestCreateExists(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	meta := Meta{
		ID:            "dupId0000000000000001",
		CreatedAt:     time.Now().UTC(),
		ExpiresAt:     time.Now().UTC().Add(time.Hour),
		FormatVersion: 1,
	}
	if err := s.Create(ctx, meta, []byte("a")); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, meta, []byte("b")); !errors.Is(err, ErrExists) {
		t.Fatalf("got %v", err)
	}
}

func TestExpiredTake(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenSQLite(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	id := "expiredId000000000001"
	meta := Meta{
		ID:            id,
		CreatedAt:     time.Now().UTC().Add(-2 * time.Hour),
		ExpiresAt:     time.Now().UTC().Add(-time.Hour),
		BurnAfterRead: false,
		FormatVersion: 1,
	}
	if err := s.Create(ctx, meta, []byte("gone")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Take(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestFSQuarantineStartupReap(t *testing.T) {
	dir := t.TempDir()
	// plant orphan quarantine files
	q := filepath.Join(dir, "quarantine")
	if err := os.MkdirAll(q, 0o700); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(q, "orphan.seal")
	if err := os.WriteFile(orphan, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := OpenFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal("quarantine orphan should be reaped on startup")
	}
}

func TestProcessLock(t *testing.T) {
	dir := t.TempDir()
	s1, err := OpenFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s1.Close()
	if _, err := OpenFS(dir); !errors.Is(err, ErrLocked) {
		t.Fatalf("expected ErrLocked, got %v", err)
	}
}

func TestDeleteExpired(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenSQLite(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	_ = s.Create(ctx, Meta{
		ID: "exp1", CreatedAt: time.Now().UTC().Add(-time.Hour),
		ExpiresAt: time.Now().UTC().Add(-time.Minute), FormatVersion: 1,
	}, []byte("a"))
	_ = s.Create(ctx, Meta{
		ID: "live1", CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(time.Hour), FormatVersion: 1,
	}, []byte("b"))
	n, err := s.DeleteExpired(ctx, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("deleted %d", n)
	}
	c, _ := s.Count(ctx)
	if c != 1 {
		t.Fatalf("count %d", c)
	}
}
