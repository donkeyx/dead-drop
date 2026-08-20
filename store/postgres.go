package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Postgres is a shared database store suitable for multiple application replicas.
type Postgres struct {
	db *sql.DB
}

// OpenPostgres opens and migrates a PostgreSQL database from a connection URL.
func OpenPostgres(databaseURL string) (*Postgres, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, errors.New("store: empty postgres database URL")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	s := &Postgres{db: db}
	if err := s.db.PingContext(context.Background()); err != nil {
		_ = s.db.Close()
		return nil, err
	}
	if err := s.migrate(context.Background()); err != nil {
		_ = s.db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Postgres) migrate(ctx context.Context) error {
	// Tables are schema-qualified so nothing is created in public.
	// The role needs CREATE on the database (for the schema) and on deaddrop.
	_, err := s.db.ExecContext(ctx, `
CREATE SCHEMA IF NOT EXISTS deaddrop;
CREATE TABLE IF NOT EXISTS deaddrop.secrets (
  id          TEXT PRIMARY KEY,
  blob        BYTEA NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL,
  expires_at  TIMESTAMPTZ NOT NULL,
  burn        BOOLEAN NOT NULL,
  size        BIGINT NOT NULL,
  fmt_version SMALLINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_secrets_expires ON deaddrop.secrets(expires_at);
CREATE TABLE IF NOT EXISTS deaddrop.stats (
  name  TEXT PRIMARY KEY,
  value BIGINT NOT NULL DEFAULT 0
);
`)
	return err
}

func postgresIncr(ctx context.Context, e execer, name string, n int64) error {
	if n == 0 {
		return nil
	}
	_, err := e.ExecContext(ctx, `
INSERT INTO deaddrop.stats (name, value) VALUES ($1, $2)
ON CONFLICT (name) DO UPDATE SET value = deaddrop.stats.value + EXCLUDED.value`, name, n)
	return err
}

func (s *Postgres) Close() error { return s.db.Close() }

func (s *Postgres) Create(ctx context.Context, meta Meta, blob []byte) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `
INSERT INTO deaddrop.secrets (id, blob, created_at, expires_at, burn, size, fmt_version)
VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		meta.ID, blob, meta.CreatedAt.UTC(), meta.ExpiresAt.UTC(), meta.BurnAfterRead,
		int64(len(blob)), meta.FormatVersion)
	if isPostgresUniqueViolation(err) {
		return ErrExists
	}
	if err != nil {
		return err
	}
	if err := postgresIncr(ctx, tx, statCreated, 1); err != nil {
		return err
	}
	if meta.HasPassphrase {
		if err := postgresIncr(ctx, tx, statPassphrase, 1); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Take locks the row inside a transaction so concurrent replicas cannot both
// deliver a burn-after-read record.
func (s *Postgres) Take(ctx context.Context, id string) (Record, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Record{}, err
	}
	defer func() { _ = tx.Rollback() }()

	rec, err := s.scanRecord(ctx, tx, id, true)
	if err != nil {
		return Record{}, err
	}
	if !rec.Meta.ExpiresAt.After(time.Now().UTC()) {
		if _, err := tx.ExecContext(ctx, `DELETE FROM deaddrop.secrets WHERE id = $1`, id); err != nil {
			return Record{}, err
		}
		if err := postgresIncr(ctx, tx, statExpired, 1); err != nil {
			return Record{}, err
		}
		if err := tx.Commit(); err != nil {
			return Record{}, err
		}
		return Record{}, ErrNotFound
	}
	if rec.Meta.BurnAfterRead {
		if _, err := tx.ExecContext(ctx, `DELETE FROM deaddrop.secrets WHERE id = $1`, id); err != nil {
			return Record{}, err
		}
		if err := postgresIncr(ctx, tx, statBurned, 1); err != nil {
			return Record{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Record{}, err
	}
	return rec, nil
}

func (s *Postgres) Get(ctx context.Context, id string) (Record, error) {
	return s.scanRecord(ctx, s.db, id, false)
}

func (s *Postgres) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM deaddrop.secrets WHERE id = $1`, id)
	return err
}

func (s *Postgres) DeleteExpired(ctx context.Context, now time.Time) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `DELETE FROM deaddrop.secrets WHERE expires_at <= $1`, now.UTC())
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := postgresIncr(ctx, tx, statExpired, n); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int(n), nil
}

func (s *Postgres) Stats(ctx context.Context) (Counters, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, value FROM deaddrop.stats`)
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

func (s *Postgres) Count(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM deaddrop.secrets WHERE expires_at > $1`, time.Now().UTC()).Scan(&n)
	return n, err
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (s *Postgres) scanRecord(ctx context.Context, q queryer, id string, forUpdate bool) (Record, error) {
	lock := ""
	if forUpdate {
		lock = " FOR UPDATE"
	}
	var rec Record
	var fmtVersion int16
	err := q.QueryRowContext(ctx, `
SELECT blob, created_at, expires_at, burn, size, fmt_version
FROM deaddrop.secrets WHERE id = $1`+lock, id).Scan(
		&rec.Blob, &rec.Meta.CreatedAt, &rec.Meta.ExpiresAt, &rec.Meta.BurnAfterRead,
		&rec.Meta.Size, &fmtVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, err
	}
	rec.Meta.ID = id
	rec.Meta.FormatVersion = uint8(fmtVersion)
	rec.Meta.Size = int64(len(rec.Blob))
	return rec, nil
}

func isPostgresUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

var _ Store = (*Postgres)(nil)
