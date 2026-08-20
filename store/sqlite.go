package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// SQLite is a single-writer SQLite store (pure Go driver).
type SQLite struct {
	db   *sql.DB
	mu   sync.Mutex
	lock *dirLock
	root string
}

// OpenSQLite opens dead-drop.db under dataDir and acquires the process lock.
func OpenSQLite(dataDir string) (*SQLite, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	lock, err := acquireDirLock(filepath.Join(dataDir, ".lock"))
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(dataDir, "dead-drop.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		_ = lock.release()
		return nil, err
	}
	// One connection: avoids concurrent transaction surprises.
	db.SetMaxOpenConns(1)
	s := &SQLite{db: db, lock: lock, root: dataDir}
	if err := s.migrate(); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLite) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS secrets (
  id          TEXT PRIMARY KEY,
  blob        BLOB NOT NULL,
  created_at  INTEGER NOT NULL,
  expires_at  INTEGER NOT NULL,
  burn        INTEGER NOT NULL,
  size        INTEGER NOT NULL,
  fmt_version INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_secrets_expires ON secrets(expires_at);
CREATE TABLE IF NOT EXISTS stats (
  name  TEXT PRIMARY KEY,
  value INTEGER NOT NULL DEFAULT 0
);
`)
	return err
}

func sqliteIncr(ctx context.Context, e execer, name string, n int64) error {
	if n == 0 {
		return nil
	}
	_, err := e.ExecContext(ctx, `
INSERT INTO stats (name, value) VALUES (?, ?)
ON CONFLICT(name) DO UPDATE SET value = value + excluded.value`, name, n)
	return err
}

func (s *SQLite) Close() error {
	err1 := s.db.Close()
	err2 := s.lock.release()
	if err1 != nil {
		return err1
	}
	return err2
}

func (s *SQLite) Create(ctx context.Context, meta Meta, blob []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	meta.Size = int64(len(blob))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `
INSERT INTO secrets (id, blob, created_at, expires_at, burn, size, fmt_version)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		meta.ID,
		blob,
		meta.CreatedAt.UTC().Unix(),
		meta.ExpiresAt.UTC().Unix(),
		boolToInt(meta.BurnAfterRead),
		meta.Size,
		meta.FormatVersion,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrExists
		}
		return err
	}
	if err := sqliteIncr(ctx, tx, statCreated, 1); err != nil {
		return err
	}
	if meta.HasPassphrase {
		if err := sqliteIncr(ctx, tx, statPassphrase, 1); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLite) Take(ctx context.Context, id string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Record{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var (
		blob       []byte
		created    int64
		expires    int64
		burn       int
		size       int64
		fmtVersion int
	)
	err = tx.QueryRowContext(ctx, `
SELECT blob, created_at, expires_at, burn, size, fmt_version FROM secrets WHERE id = ?`, id).
		Scan(&blob, &created, &expires, &burn, &size, &fmtVersion)
	if err == sql.ErrNoRows {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, err
	}

	now := time.Now().UTC().Unix()
	if expires > 0 && now >= expires {
		if _, err := tx.ExecContext(ctx, `DELETE FROM secrets WHERE id = ?`, id); err != nil {
			return Record{}, err
		}
		if err := sqliteIncr(ctx, tx, statExpired, 1); err != nil {
			return Record{}, err
		}
		if err := tx.Commit(); err != nil {
			return Record{}, err
		}
		return Record{}, ErrNotFound
	}

	rec := Record{
		Meta: Meta{
			ID:            id,
			CreatedAt:     time.Unix(created, 0).UTC(),
			ExpiresAt:     time.Unix(expires, 0).UTC(),
			BurnAfterRead: burn != 0,
			Size:          int64(len(blob)),
			FormatVersion: uint8(fmtVersion),
		},
		Blob: append([]byte(nil), blob...),
	}

	if burn != 0 {
		res, err := tx.ExecContext(ctx, `DELETE FROM secrets WHERE id = ?`, id)
		if err != nil {
			return Record{}, err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return Record{}, ErrNotFound
		}
		if err := sqliteIncr(ctx, tx, statBurned, 1); err != nil {
			return Record{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return Record{}, err
	}
	return rec, nil
}

func (s *SQLite) Get(ctx context.Context, id string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var (
		blob       []byte
		created    int64
		expires    int64
		burn       int
		size       int64
		fmtVersion int
	)
	err := s.db.QueryRowContext(ctx, `
SELECT blob, created_at, expires_at, burn, size, fmt_version FROM secrets WHERE id = ?`, id).
		Scan(&blob, &created, &expires, &burn, &size, &fmtVersion)
	if err == sql.ErrNoRows {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, err
	}
	now := time.Now().UTC().Unix()
	if expires > 0 && now >= expires {
		return Record{}, ErrNotFound
	}
	return Record{
		Meta: Meta{
			ID:            id,
			CreatedAt:     time.Unix(created, 0).UTC(),
			ExpiresAt:     time.Unix(expires, 0).UTC(),
			BurnAfterRead: burn != 0,
			Size:          int64(len(blob)),
			FormatVersion: uint8(fmtVersion),
		},
		Blob: append([]byte(nil), blob...),
	}, nil
}

func (s *SQLite) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.ExecContext(ctx, `DELETE FROM secrets WHERE id = ?`, id)
	return err
}

func (s *SQLite) DeleteExpired(ctx context.Context, now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `DELETE FROM secrets WHERE expires_at > 0 AND expires_at <= ?`, now.UTC().Unix())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if err := sqliteIncr(ctx, tx, statExpired, n); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int(n), nil
}

func (s *SQLite) Stats(ctx context.Context) (Counters, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.QueryContext(ctx, `SELECT name, value FROM stats`)
	if err != nil {
		return Counters{}, err
	}
	defer rows.Close()
	m := map[string]int64{}
	for rows.Next() {
		var name string
		var value int64
		if err := rows.Scan(&name, &value); err != nil {
			return Counters{}, err
		}
		m[name] = value
	}
	return countersFromMap(m), rows.Err()
}

func (s *SQLite) Count(ctx context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM secrets`).Scan(&n)
	return n, err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE") || strings.Contains(msg, "unique constraint")
}

var _ Store = (*SQLite)(nil)
