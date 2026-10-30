package conversation

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"
)

var (
	ErrNotFound = errors.New("conversation not found")
	ErrBusy     = errors.New("previous reply is still being generated")
	ErrConflict = errors.New("request key already used with different content")
	ErrInvalid  = errors.New("invalid conversation input")
)

// Store keeps conversations scoped to an active account. Every query joins the
// owning account, so another user's conversation is indistinguishable from a
// missing one.
type Store struct{ DB *sql.DB }

type Conversation struct {
	ID        string       `json:"id"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
	Links     []Link       `json:"links"`
	Messages  []Message    `json:"messages"`
	Summary   *SummaryInfo `json:"summary,omitempty"`
}

// SummaryInfo exposes the coverage point, not the generated summary text.
type SummaryInfo struct {
	ThroughSeq int64     `json:"through_seq"`
	Current    bool      `json:"current"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type ListItem struct {
	ID        string    `json:"id"`
	UpdatedAt time.Time `json:"updated_at"`
	Preview   string    `json:"preview"`
	Links     []string  `json:"link_domains"`
}

func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s Store) Create(ctx context.Context, user string) (Conversation, error) {
	id, err := newID()
	if err != nil {
		return Conversation{}, err
	}
	var c Conversation
	err = s.DB.QueryRowContext(ctx, `INSERT INTO conversations(id,account_id) SELECT $1,id FROM accounts WHERE id=$2 AND deleted_at IS NULL RETURNING id,created_at,updated_at`, id, user).Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Conversation{}, ErrNotFound
	}
	c.Links, c.Messages = []Link{}, []Message{}
	return c, err
}

func (s Store) owned(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, user, id string, lock bool) error {
	query := `SELECT c.id FROM conversations c JOIN accounts a ON a.id=c.account_id WHERE c.id=$1 AND c.account_id=$2 AND a.deleted_at IS NULL`
	if lock {
		query += ` FOR UPDATE OF c`
	}
	var found string
	err := q.QueryRowContext(ctx, query, id, user).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func (s Store) AddLink(ctx context.Context, user, id string, l Link) (Link, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Link{}, err
	}
	defer tx.Rollback()
	if err = s.owned(ctx, tx, user, id, true); err != nil {
		return Link{}, err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM conversation_links WHERE conversation_id=$1`, id).Scan(&count); err != nil {
		return Link{}, err
	}
	var existing Link
	err = tx.QueryRowContext(ctx, `SELECT id,reading_id,domain,subject,snapshot,created_at FROM conversation_links WHERE conversation_id=$1 AND reading_id=$2`, id, l.ReadingID).Scan(&existing.ID, &existing.ReadingID, &existing.Domain, &existing.Subject, &existing.Snapshot, &existing.CreatedAt)
	if err == nil {
		if existing.Subject != l.Subject {
			return Link{}, ErrConflict
		}
		return existing, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Link{}, err
	}
	if count >= 5 {
		return Link{}, ErrInvalid
	}
	if l.ID, err = newID(); err != nil {
		return Link{}, err
	}
	if err = tx.QueryRowContext(ctx, `INSERT INTO conversation_links(id,conversation_id,account_id,reading_id,domain,subject,snapshot) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING created_at`, l.ID, id, user, l.ReadingID, l.Domain, l.Subject, []byte(l.Snapshot)).Scan(&l.CreatedAt); err != nil {
		return Link{}, err
	}
	return l, tx.Commit()
}

func (s Store) List(ctx context.Context, user string) ([]ListItem, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT c.id,c.updated_at,COALESCE((SELECT content FROM messages m WHERE m.conversation_id=c.id AND m.role='user' ORDER BY seq DESC LIMIT 1),''),COALESCE((SELECT json_agg(domain ORDER BY created_at) FROM conversation_links l WHERE l.conversation_id=c.id),'[]') FROM conversations c JOIN accounts a ON a.id=c.account_id WHERE c.account_id=$1 AND a.deleted_at IS NULL ORDER BY c.updated_at DESC,c.id LIMIT 100`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ListItem{}
	for rows.Next() {
		var item ListItem
		var domains []byte
		if err = rows.Scan(&item.ID, &item.UpdatedAt, &item.Preview, &domains); err != nil {
			return nil, err
		}
		if utf8.RuneCountInString(item.Preview) > 40 {
			item.Preview = string([]rune(item.Preview)[:40]) + "…"
		}
		if err = json.Unmarshal(domains, &item.Links); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s Store) Get(ctx context.Context, user, id string, generation int64) (Conversation, error) {
	var c Conversation
	err := s.DB.QueryRowContext(ctx, `SELECT c.id,c.created_at,c.updated_at FROM conversations c JOIN accounts a ON a.id=c.account_id WHERE c.id=$1 AND c.account_id=$2 AND a.deleted_at IS NULL`, id, user).Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Conversation{}, ErrNotFound
	}
	if err != nil {
		return Conversation{}, err
	}
	if c.Links, err = s.links(ctx, id); err != nil {
		return Conversation{}, err
	}
	if c.Messages, err = s.messages(ctx, id, 500); err != nil {
		return Conversation{}, err
	}
	sum, err := s.summary(ctx, id)
	if err != nil {
		return Conversation{}, err
	}
	if sum != nil {
		c.Summary = &SummaryInfo{ThroughSeq: sum.ThroughSeq, Current: sum.MemoryGeneration == generation, UpdatedAt: sum.UpdatedAt}
	}
	return c, nil
}

func (s Store) Delete(ctx context.Context, user, id string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM conversations WHERE id=$1 AND account_id=$2`, id, user)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s Store) links(ctx context.Context, id string) ([]Link, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,reading_id,domain,subject,snapshot,created_at FROM conversation_links WHERE conversation_id=$1 ORDER BY created_at,id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Link{}
	for rows.Next() {
		var l Link
		if err = rows.Scan(&l.ID, &l.ReadingID, &l.Domain, &l.Subject, &l.Snapshot, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// messages returns the latest limit messages in ascending order.
func (s Store) messages(ctx context.Context, id string, limit int) ([]Message, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,seq,role,content,state,meta,created_at FROM (SELECT * FROM messages WHERE conversation_id=$1 AND seq IS NOT NULL ORDER BY seq DESC LIMIT $2) m ORDER BY seq`, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Message{}
	for rows.Next() {
		var m Message
		var meta []byte
		if err = rows.Scan(&m.ID, &m.Seq, &m.Role, &m.Content, &m.State, &meta, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.Meta = meta
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s Store) summary(ctx context.Context, id string) (*Summary, error) {
	var sum Summary
	err := s.DB.QueryRowContext(ctx, `SELECT content,through_seq,memory_generation,updated_at FROM conversation_summaries WHERE conversation_id=$1`, id).Scan(&sum.Content, &sum.ThroughSeq, &sum.MemoryGeneration, &sum.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sum, nil
}

// BeginTurn stores the user message and a pending assistant message in one
// transaction. A repeated request key returns the original pair; a different
// content under the same key is a conflict. One reply is generated at a time.
func (s Store) BeginTurn(ctx context.Context, user, id, key, content string) (Message, Message, bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Message{}, Message{}, false, err
	}
	defer tx.Rollback()
	if err = s.owned(ctx, tx, user, id, true); err != nil {
		return Message{}, Message{}, false, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,seq,role,content,state,meta,created_at FROM messages WHERE conversation_id=$1 AND request_key=$2 ORDER BY seq`, id, key)
	if err != nil {
		return Message{}, Message{}, false, err
	}
	existing := []Message{}
	for rows.Next() {
		var m Message
		var meta []byte
		if err = rows.Scan(&m.ID, &m.Seq, &m.Role, &m.Content, &m.State, &meta, &m.CreatedAt); err != nil {
			rows.Close()
			return Message{}, Message{}, false, err
		}
		m.Meta = meta
		existing = append(existing, m)
	}
	rows.Close()
	if len(existing) == 2 {
		if existing[0].Content != content {
			return Message{}, Message{}, false, ErrConflict
		}
		return existing[0], existing[1], false, tx.Commit()
	}
	if len(existing) != 0 {
		return Message{}, Message{}, false, errors.New("incomplete stored turn")
	}
	// A reply pending longer than the worker timeout was interrupted. Its cost is
	// unknown, so it is marked unknown rather than retried automatically.
	if _, err = tx.ExecContext(ctx, `UPDATE messages SET state='unknown' WHERE conversation_id=$1 AND state='pending' AND created_at <= now()-interval '3 minutes'`, id); err != nil {
		return Message{}, Message{}, false, err
	}
	var busy bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM messages WHERE conversation_id=$1 AND state='pending')`, id).Scan(&busy); err != nil {
		return Message{}, Message{}, false, err
	}
	if busy {
		return Message{}, Message{}, false, ErrBusy
	}
	var seq int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(max(seq),0) FROM messages WHERE conversation_id=$1`, id).Scan(&seq); err != nil {
		return Message{}, Message{}, false, err
	}
	um := Message{Seq: seq + 1, Role: "user", Content: content, State: "complete", Meta: json.RawMessage(`{}`)}
	am := Message{Seq: seq + 2, Role: "assistant", Content: "", State: "pending", Meta: json.RawMessage(`{}`)}
	for _, m := range []*Message{&um, &am} {
		if m.ID, err = newID(); err != nil {
			return Message{}, Message{}, false, err
		}
		if err = tx.QueryRowContext(ctx, `INSERT INTO messages(id,conversation_id,role,content,seq,request_key,state) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING created_at`, m.ID, id, m.Role, m.Content, m.Seq, key, m.State).Scan(&m.CreatedAt); err != nil {
			return Message{}, Message{}, false, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE conversations SET updated_at=now() WHERE id=$1`, id); err != nil {
		return Message{}, Message{}, false, err
	}
	return um, am, true, tx.Commit()
}

type turnInputs struct {
	History    []Message
	Summary    *Summary
	Generation int64
	Suppressed map[string]bool
	Links      []Link
}

func (s Store) inputs(ctx context.Context, user, id string, limit int) (turnInputs, error) {
	var in turnInputs
	if err := s.DB.QueryRowContext(ctx, `SELECT a.memory_generation FROM conversations c JOIN accounts a ON a.id=c.account_id WHERE c.id=$1 AND c.account_id=$2 AND a.deleted_at IS NULL`, id, user).Scan(&in.Generation); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return in, ErrNotFound
		}
		return in, err
	}
	var err error
	if in.History, err = s.messages(ctx, id, limit); err != nil {
		return in, err
	}
	if in.Summary, err = s.summary(ctx, id); err != nil {
		return in, err
	}
	if in.Links, err = s.links(ctx, id); err != nil {
		return in, err
	}
	in.Suppressed = map[string]bool{}
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT s.source_message_id FROM memory_suppressions s JOIN messages m ON m.id=s.source_message_id WHERE s.account_id=$1 AND m.conversation_id=$2`, user, id)
	if err != nil {
		return in, err
	}
	defer rows.Close()
	for rows.Next() {
		var mid string
		if err = rows.Scan(&mid); err != nil {
			return in, err
		}
		in.Suppressed[mid] = true
	}
	return in, rows.Err()
}

// finish stores the outcome only while the reply is still pending, so a late
// worker cannot overwrite an interrupted turn or write after account deletion.
func (s Store) finish(ctx context.Context, user, messageID, state, content string, meta json.RawMessage) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `UPDATE messages m SET state=$3,content=$4,meta=$5 FROM conversations c JOIN accounts a ON a.id=c.account_id WHERE m.id=$1 AND m.conversation_id=c.id AND c.account_id=$2 AND a.deleted_at IS NULL AND m.state='pending'`, messageID, user, state, content, []byte(meta))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// saveSummary keeps the newest coverage and refuses a summary built before a
// memory correction or deletion that happened while it was generated.
func (s Store) saveSummary(ctx context.Context, user, id, content string, through, generation int64, meta json.RawMessage) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO conversation_summaries(conversation_id,account_id,content,through_seq,memory_generation,meta)
SELECT c.id,c.account_id,$3,$4,$5,$6 FROM conversations c JOIN accounts a ON a.id=c.account_id WHERE c.id=$1 AND c.account_id=$2 AND a.deleted_at IS NULL AND a.memory_generation=$5
ON CONFLICT (conversation_id) DO UPDATE SET content=EXCLUDED.content,through_seq=EXCLUDED.through_seq,memory_generation=EXCLUDED.memory_generation,meta=EXCLUDED.meta,updated_at=now()
WHERE conversation_summaries.memory_generation<>EXCLUDED.memory_generation OR conversation_summaries.through_seq<EXCLUDED.through_seq`, id, user, content, through, generation, []byte(meta))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}
