package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMem0OSSContractsAndScopedMutations(t *testing.T) {
	var routes []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		routes = append(routes, r.Method+" "+r.URL.Path)
		if r.Header.Get("X-API-Key") != "test-key" {
			t.Error("missing API key")
		}
		w.Header().Set("Content-Type", "application/json")
		var body map[string]any
		if r.Body != nil && (r.Method == http.MethodPost || r.Method == http.MethodPut) {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
		}
		switch r.Method + " " + r.URL.Path {
		case "POST /memories":
			if body["user_id"] != "alice" || body["infer"] != false {
				t.Errorf("bad scoped add: %+v", body)
			}
			fmt.Fprint(w, `{"results":[{"id":"m1","memory":"일요일 연락","event":"ADD"}]}`)
		case "POST /search":
			filters, _ := body["filters"].(map[string]any)
			if filters["user_id"] != "alice" || body["top_k"] != float64(5) || body["user_id"] != nil {
				t.Errorf("bad search filters: %+v", body)
			}
			fmt.Fprint(w, `{"results":[{"id":"m1","memory":"일요일 연락","user_id":"alice","score":0.9}]}`)
		case "GET /memories/m1":
			fmt.Fprint(w, `{"id":"m1","memory":"일요일 연락","user_id":"alice"}`)
		case "PUT /memories/m1":
			if body["text"] != "연락하지 않기" || len(body) != 1 {
				t.Errorf("unexpected owner or metadata mutation: %+v", body)
			}
			fmt.Fprint(w, `{"message":"Memory updated successfully"}`)
		case "DELETE /memories/m1":
			fmt.Fprint(w, `{"message":"Memory deleted successfully"}`)
		case "DELETE /memories":
			if r.URL.Query().Get("user_id") != "alice" {
				t.Error("unscoped deletion")
			}
			fmt.Fprint(w, `{"message":"All relevant memories deleted"}`)
		default:
			t.Errorf("unexpected route %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	c, err := NewMem0(server.URL, "test-key", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err = c.Add(ctx, "alice", []Message{{Role: "user", Content: "일요일에 연락하기로 했어."}}, false); err != nil {
		t.Fatal(err)
	}
	rows, err := c.Search(ctx, "alice", "연락 기준", 5)
	if err != nil || len(rows) != 1 {
		t.Fatalf("search: %v %+v", err, rows)
	}
	if _, err = c.Update(ctx, "alice", "m1", "연락하지 않기"); err != nil {
		t.Fatal(err)
	}
	if err = c.Delete(ctx, "alice", "m1"); err != nil {
		t.Fatal(err)
	}
	if err = c.DeleteUser(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	want := "POST /memories|POST /search|GET /memories/m1|PUT /memories/m1|GET /memories/m1|DELETE /memories/m1|DELETE /memories"
	if strings.Join(routes, "|") != want {
		t.Fatalf("routes %v", routes)
	}
}

func TestMem0RejectsForeignRecordsBeforeMutation(t *testing.T) {
	mutations := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut || r.Method == http.MethodDelete {
			mutations++
			fmt.Fprint(w, `{}`)
			return
		}
		if r.URL.Path == "/search" {
			fmt.Fprint(w, `{"results":[{"id":"m1","user_id":"bob","memory":"private"}]}`)
			return
		}
		fmt.Fprint(w, `{"id":"m1","user_id":"bob","memory":"private"}`)
	}))
	defer s.Close()
	c, _ := NewMem0(s.URL, "test-key", nil)
	ctx := context.Background()
	if _, err := c.Search(ctx, "alice", "query", 1); !errors.Is(err, ErrScope) {
		t.Fatalf("search leaked foreign records: %v", err)
	}
	if _, err := c.Update(ctx, "alice", "m1", "changed"); !errors.Is(err, ErrScope) {
		t.Fatalf("update allowed: %v", err)
	}
	if err := c.Delete(ctx, "alice", "m1"); !errors.Is(err, ErrScope) {
		t.Fatalf("delete allowed: %v", err)
	}
	if mutations != 0 {
		t.Fatal("foreign mutation reached upstream")
	}
}

func TestMem0RejectsRedirectsAndDoesNotEchoSensitiveErrorBody(t *testing.T) {
	seen := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen = true; fmt.Fprint(w, `{}`) }))
	defer target.Close()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search" {
			http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
			return
		}
		w.WriteHeader(500)
		fmt.Fprint(w, `{"secret":"private fact or credential"}`)
	}))
	defer s.Close()
	c, _ := NewMem0(s.URL, "test-key", nil)
	_, err := c.Search(context.Background(), "alice", "q", 1)
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != 307 || seen {
		t.Fatalf("redirect followed: %v", err)
	}
	_, err = c.Add(context.Background(), "alice", []Message{{Role: "user", Content: "private"}}, false)
	if err == nil || strings.Contains(err.Error(), "credential") || strings.Contains(err.Error(), "private fact") {
		t.Fatalf("error body leaked: %v", err)
	}
}

func TestMem0RequiresSecureExplicitConfiguration(t *testing.T) {
	for _, url := range []string{"http://example.com", "https://user:password@example.com", "https://example.com?token=secret", "not-a-url"} {
		if _, err := NewMem0(url, "test-key", nil); err == nil {
			t.Fatalf("accepted URL %s", url)
		}
	}
	if _, err := NewMem0("https://example.com", "", nil); err == nil {
		t.Fatal("missing key accepted")
	}
}
