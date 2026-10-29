package persistence

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

var ErrInvalidCredentials = errors.New("invalid credentials or temporarily unavailable")
var ErrCredentialConflict = errors.New("credential cannot be linked")
var ErrInvalidCredentialInput = errors.New("username or password does not meet requirements")
var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{2,63}$`)

// The semaphore bounds concurrent memory-hard derivations within this process.
// Public deployment also needs distributed, network-aware abuse protection.
var passwordSlots = make(chan struct{}, 4)

const sessionDuration = 30 * 24 * time.Hour

func normalizeUsername(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
func validateCredentialInput(username, password string) error {
	if !usernamePattern.MatchString(username) || len(password) < 12 || len(password) > 128 {
		return ErrInvalidCredentialInput
	}
	return nil
}
func derivePassword(ctx context.Context, password string, salt []byte) ([]byte, error) {
	select {
	case passwordSlots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-passwordSlots }()
	return argon2.IDKey([]byte(password), salt, 3, 64*1024, 1, 32), nil
}
func randomHex(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func newSessionToken() (string, []byte, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(token))
	return token, h[:], nil
}
func insertSession(ctx context.Context, tx *sql.Tx, account string) (string, error) {
	token, hash, err := newSessionToken()
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO auth_sessions(token_hash,account_id,expires_at) VALUES($1,$2,$3)`, hash, account, time.Now().UTC().Add(sessionDuration))
	if err != nil {
		return "", err
	}
	return token, nil
}

// CreateGuest returns a session for a new unlinked account. Losing that token
// loses access unless the user first links credentials.
func (s Store) CreateGuest(ctx context.Context) (string, error) {
	id, err := randomHex(16)
	if err != nil {
		return "", err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO accounts(id) VALUES($1)`, id); err != nil {
		return "", err
	}
	token, err := insertSession(ctx, tx, id)
	if err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return token, nil
}

// Register creates a new account, credentials and session atomically.
func (s Store) Register(ctx context.Context, username, password string) (string, error) {
	username = normalizeUsername(username)
	if err := validateCredentialInput(username, password); err != nil {
		return "", err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash, err := derivePassword(ctx, password, salt)
	if err != nil {
		return "", err
	}
	id, err := randomHex(16)
	if err != nil {
		return "", err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO accounts(id) VALUES($1)`, id); err != nil {
		return "", err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO password_credentials(username,account_id,salt,password_hash) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, username, id, salt, hash)
	if err != nil {
		return "", err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return "", err
	}
	if n != 1 {
		return "", ErrCredentialConflict
	}
	token, err := insertSession(ctx, tx, id)
	if err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return token, nil
}

// LinkPassword attaches a recovery login to the authenticated guest account;
// username/account uniqueness prevents silently merging or replacing accounts.
func (s Store) LinkPassword(ctx context.Context, account, username, password string) error {
	username = normalizeUsername(username)
	if err := validateCredentialInput(username, password); err != nil {
		return err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	hash, err := derivePassword(ctx, password, salt)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(ctx, `SELECT id FROM accounts WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, account).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUnauthenticated
	}
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO password_credentials(username,account_id,salt,password_hash) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, username, id, salt, hash)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrCredentialConflict
	}
	return tx.Commit()
}

// Login serializes failures for a username in the database, locking for 15
// minutes after five failures; the public response is the same for unknown,
// invalid and temporarily locked accounts.
func (s Store) Login(ctx context.Context, username, password string) (string, error) {
	username = normalizeUsername(username)
	if len(password) > 128 || !usernamePattern.MatchString(username) {
		_, err := derivePassword(ctx, "", make([]byte, 16))
		if err != nil {
			return "", err
		}
		return "", ErrInvalidCredentials
	}
	// Bound expensive derivations before acquiring a database connection or lock.
	select {
	case passwordSlots <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-passwordSlots }()
	deriveHeld := func(value string, salt []byte) []byte { return argon2.IDKey([]byte(value), salt, 3, 64*1024, 1, 32) }
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	// Lock account before credential, matching deletion and linking lock order.
	var id string
	err = tx.QueryRowContext(ctx, `SELECT account_id FROM password_credentials WHERE username=$1`, username).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		_ = deriveHeld(password, make([]byte, 16))
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return "", ErrInvalidCredentials
	}
	if err != nil {
		return "", err
	}
	var activeID string
	err = tx.QueryRowContext(ctx, `SELECT id FROM accounts WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&activeID)
	if errors.Is(err, sql.ErrNoRows) {
		_ = deriveHeld(password, make([]byte, 16))
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return "", ErrInvalidCredentials
	}
	if err != nil {
		return "", err
	}
	var salt, expected []byte
	var failures int
	var locked sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT salt,password_hash,failed_attempts,locked_until FROM password_credentials WHERE username=$1 AND account_id=$2 FOR UPDATE`, username, id).Scan(&salt, &expected, &failures, &locked)
	if errors.Is(err, sql.ErrNoRows) {
		_ = deriveHeld(password, make([]byte, 16))
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return "", ErrInvalidCredentials
	}
	if err != nil {
		return "", err
	}
	actual := deriveHeld(password, salt)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if locked.Valid && locked.Time.After(time.Now()) {
		return "", ErrInvalidCredentials
	}
	if subtle.ConstantTimeCompare(actual, expected) != 1 {
		failures++
		if failures >= 5 {
			_, err = tx.ExecContext(ctx, `UPDATE password_credentials SET failed_attempts=0,locked_until=now()+interval '15 minutes' WHERE username=$1`, username)
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE password_credentials SET failed_attempts=$2 WHERE username=$1`, username, failures)
		}
		if err != nil {
			return "", err
		}
		if err = tx.Commit(); err != nil {
			return "", err
		}
		return "", ErrInvalidCredentials
	}
	if _, err = tx.ExecContext(ctx, `UPDATE password_credentials SET failed_attempts=0,locked_until=NULL WHERE username=$1`, username); err != nil {
		return "", err
	}
	token, err := insertSession(ctx, tx, id)
	if err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return token, nil
}
