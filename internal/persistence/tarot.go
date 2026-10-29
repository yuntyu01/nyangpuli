package persistence

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/yuntyu01/nyangpuli-lab/internal/tarot"
)

var ErrTarotNotFound = errors.New("tarot session not found")
var ErrTarotSelection = errors.New("invalid tarot selection")

// CreateTarot saves the deck before any user selection. Account is determined
// by authentication, never by the request body.
func (s Store) CreateTarot(ctx context.Context, account string, session tarot.Session) (string, error) {
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(session)
	if err != nil {
		return "", err
	}
	key := hex.EncodeToString(id)
	_, err = s.DB.ExecContext(ctx, `INSERT INTO tarot_sessions(id,account_id,question,cards) VALUES($1,$2,$3,$4)`, key, account, session.Question, encoded)
	return key, err
}

func (s Store) GetTarot(ctx context.Context, account, id string) (tarot.Session, error) {
	var session tarot.Session
	var raw []byte
	err := s.DB.QueryRowContext(ctx, `SELECT cards FROM tarot_sessions WHERE id=$1 AND account_id=$2`, id, account).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return session, ErrTarotNotFound
	}
	if err != nil {
		return session, err
	}
	err = json.Unmarshal(raw, &session)
	return session, err
}

// SelectTarot serializes selections per session to prevent lost updates.
func (s Store) SelectTarot(ctx context.Context, account, id string, engine *tarot.Engine, position int) (tarot.Session, error) {
	var session tarot.Session
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return session, err
	}
	defer tx.Rollback()
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT cards FROM tarot_sessions WHERE id=$1 AND account_id=$2 FOR UPDATE`, id, account).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return session, ErrTarotNotFound
	}
	if err != nil {
		return session, err
	}
	if err = json.Unmarshal(raw, &session); err != nil {
		return session, err
	}
	if err = engine.Select(&session, position); err != nil {
		return session, ErrTarotSelection
	}
	raw, err = json.Marshal(session)
	if err != nil {
		return session, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tarot_sessions SET cards=$3 WHERE id=$1 AND account_id=$2`, id, account, raw); err != nil {
		return session, err
	}
	if err = tx.Commit(); err != nil {
		return session, err
	}
	return session, nil
}
