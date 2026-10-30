package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/yuntyu01/nyangpuli-lab/internal/conversation"
	"github.com/yuntyu01/nyangpuli-lab/internal/generation"
	"github.com/yuntyu01/nyangpuli-lab/internal/memory"
)

type ConversationService interface {
	Create(context.Context, string, string, string) (conversation.Conversation, error)
	List(context.Context, string) ([]conversation.ListItem, error)
	Get(context.Context, string, string) (conversation.Conversation, error)
	Delete(context.Context, string, string) error
	AddLink(context.Context, string, string, string, string) (conversation.Link, error)
	Send(context.Context, string, string, string, string) (conversation.Turn, error)
}

func (s *Server) handleConversations(w http.ResponseWriter, r *http.Request, user string) bool {
	const base = "/v1/conversations"
	if r.URL.Path != base && !strings.HasPrefix(r.URL.Path, base+"/") {
		return false
	}
	if s.Conversations == nil {
		fail(w, 503, "conversations require PostgreSQL")
		return true
	}
	ctx := r.Context()
	if r.URL.Path == base {
		switch r.Method {
		case http.MethodPost:
			var req struct {
				LinkedReadingID string `json:"linked_reading_id"`
				Subject         string `json:"subject"`
			}
			if !read(w, r, &req) {
				return true
			}
			c, err := s.Conversations.Create(ctx, user, req.LinkedReadingID, req.Subject)
			if err != nil {
				conversationError(w, err)
			} else {
				write(w, 201, c)
			}
		case http.MethodGet:
			items, err := s.Conversations.List(ctx, user)
			if err != nil {
				conversationError(w, err)
			} else {
				write(w, 200, items)
			}
		default:
			fail(w, 405, "method not allowed")
		}
		return true
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, base+"/"), "/")
	if parts[0] == "" || len(parts) > 2 {
		fail(w, 404, "route not found")
		return true
	}
	id := parts[0]
	switch {
	case len(parts) == 1 && r.Method == http.MethodGet:
		c, err := s.Conversations.Get(ctx, user, id)
		if err != nil {
			conversationError(w, err)
		} else {
			write(w, 200, c)
		}
	case len(parts) == 1 && r.Method == http.MethodDelete:
		if err := s.Conversations.Delete(ctx, user, id); err != nil {
			conversationError(w, err)
		} else {
			write(w, 200, map[string]string{"status": "deleted"})
		}
	case len(parts) == 2 && parts[1] == "links" && r.Method == http.MethodPost:
		var req struct {
			ReadingID string `json:"reading_id"`
			Subject   string `json:"subject"`
		}
		if !read(w, r, &req) {
			return true
		}
		l, err := s.Conversations.AddLink(ctx, user, id, req.ReadingID, req.Subject)
		if err != nil {
			conversationError(w, err)
		} else {
			write(w, 201, l)
		}
	case len(parts) == 2 && parts[1] == "messages" && r.Method == http.MethodPost:
		var req struct {
			Content string `json:"content"`
		}
		if !read(w, r, &req) {
			return true
		}
		turn, err := s.Conversations.Send(ctx, user, id, r.Header.Get("Idempotency-Key"), req.Content)
		if err != nil {
			conversationError(w, err)
		} else {
			write(w, http.StatusAccepted, turn)
		}
	default:
		fail(w, 404, "route not found")
	}
	return true
}

func conversationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, conversation.ErrNotConfigured):
		fail(w, 503, "generation not configured; opt in with serve --enable-generation")
	case errors.Is(err, conversation.ErrNotFound):
		fail(w, 404, "conversation or reading not found")
	case errors.Is(err, conversation.ErrInvalid):
		fail(w, 400, "invalid conversation request, link subject or idempotency key")
	case errors.Is(err, conversation.ErrBusy):
		fail(w, 409, "previous reply is still being generated")
	case errors.Is(err, conversation.ErrConflict):
		fail(w, 409, "idempotency key or link already used with different content")
	default:
		fail(w, 500, "conversation operation failed")
	}
}

// NewConversationService wires counseling conversations to the PostgreSQL
// memory store and reading archive. A nil client keeps history readable but
// rejects new messages.
func NewConversationService(db *sql.DB, m MemoryStore, client *generation.Client, archive ReadingService) ConversationService {
	svc := &conversation.Service{Store: conversation.Store{DB: db}}
	if pg, ok := m.(*memory.PostgresStore); ok {
		svc.Memory = pg
	}
	if client != nil {
		svc.Generator = readingGenerator{client: client}
	}
	if links, ok := archive.(conversation.LinkSource); ok {
		svc.Links = links
	}
	return svc
}
