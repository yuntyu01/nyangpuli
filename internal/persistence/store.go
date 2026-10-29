// Package persistence provides PostgreSQL migrations and account sessions.
package persistence

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Migrate runs each numbered migration once. An advisory transaction lock makes
// concurrent startups safe and the migration and its marker commit together.
func Migrate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(743502197)`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, name).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		script, e := migrations.ReadFile("migrations/" + name)
		if e != nil {
			return e
		}
		if _, err = tx.ExecContext(ctx, string(script)); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES($1)`, name); err != nil {
			return err
		}
	}
	return tx.Commit()
}

var ErrUnauthenticated = errors.New("unauthenticated")

type Store struct{ DB *sql.DB }

// CreateSession creates an opaque random bearer token. Only its SHA-256 digest
// is stored. Callers must ensure account ownership before issuing a session.
func (s Store) CreateSession(ctx context.Context, accountID string, expiresAt time.Time) (string, error) {
	if accountID == "" || !expiresAt.After(time.Now()) {
		return "", errors.New("invalid session")
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(random)
	digest := sha256.Sum256([]byte(token))
	result, err := s.DB.ExecContext(ctx, `INSERT INTO auth_sessions(token_hash,account_id,expires_at) SELECT $1,id,$3 FROM accounts WHERE id=$2 AND deleted_at IS NULL`, digest[:], accountID, expiresAt)
	if err != nil {
		return "", err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return "", err
	}
	if n != 1 {
		return "", ErrUnauthenticated
	}
	return token, nil
}
func (s Store) Authenticate(ctx context.Context, token string) (string, error) {
	if len(token) != 43 {
		return "", ErrUnauthenticated
	}
	digest := sha256.Sum256([]byte(token))
	var id string
	err := s.DB.QueryRowContext(ctx, `SELECT s.account_id FROM auth_sessions s JOIN accounts a ON a.id=s.account_id WHERE s.token_hash=$1 AND s.revoked_at IS NULL AND s.expires_at>now() AND a.deleted_at IS NULL`, digest[:]).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrUnauthenticated
	}
	return id, err
}
func (s Store) RevokeSession(ctx context.Context, token string) error {
	digest := sha256.Sum256([]byte(token))
	_, err := s.DB.ExecContext(ctx, `UPDATE auth_sessions SET revoked_at=now() WHERE token_hash=$1`, digest[:])
	return err
}
