package persistence

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPasswordAccountRecoveryAndLinkPostgres(t *testing.T) {
	dsn := os.Getenv("NYANG_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("NYANG_TEST_DATABASE_URL not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	store := Store{DB: db}
	username := fmt.Sprintf("test%d", time.Now().UnixNano())
	password := "test-password-1234"
	guest, err := store.CreateGuest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id, err := store.Authenticate(ctx, guest)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DELETE FROM accounts WHERE id=$1`, id)
	if err = store.LinkPassword(ctx, id, username, password); err != nil {
		t.Fatal(err)
	}
	if err = store.RevokeSession(ctx, guest); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Authenticate(ctx, guest); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("revocation: %v", err)
	}
	recovered, err := store.Login(ctx, username, password)
	if err != nil {
		t.Fatal(err)
	}
	recoveredID, err := store.Authenticate(ctx, recovered)
	if err != nil || recoveredID != id {
		t.Fatalf("recovery %v", err)
	}
	var hash, salt []byte
	if err = db.QueryRow(`SELECT password_hash,salt FROM password_credentials WHERE username=$1`, username).Scan(&hash, &salt); err != nil {
		t.Fatal(err)
	}
	if string(hash) == password || len(hash) != 32 || len(salt) != 16 {
		t.Fatal("password storage incorrect")
	}
	if _, err = store.Login(ctx, username, password+"incorrect"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password: %v", err)
	}
	// A linked account and username cannot be replaced or transferred.
	if err = store.LinkPassword(ctx, id, username+"x", password); !errors.Is(err, ErrCredentialConflict) {
		t.Fatalf("replacement: %v", err)
	}
	guest2, err := store.CreateGuest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id2, err := store.Authenticate(ctx, guest2)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DELETE FROM accounts WHERE id=$1`, id2)
	if err = store.LinkPassword(ctx, id2, username, password); !errors.Is(err, ErrCredentialConflict) {
		t.Fatalf("transfer: %v", err)
	}
	if err = store.DeleteAccount(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Login(ctx, username, password); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("deleted login: %v", err)
	}
	var n int
	if err = db.QueryRow(`SELECT count(*) FROM password_credentials WHERE account_id=$1`, id).Scan(&n); err != nil || n != 0 {
		t.Fatalf("deleted credential %d %v", n, err)
	}
}
func TestConcurrentCredentialClaimsAndFailedLoginLockPostgres(t *testing.T) {
	dsn := os.Getenv("NYANG_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("NYANG_TEST_DATABASE_URL not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	s := Store{DB: db}
	username := fmt.Sprintf("claim%d", time.Now().UnixNano())
	password := "test-password-1234"
	var wg sync.WaitGroup
	tokens := make(chan string, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); token, e := s.Register(ctx, username, password); tokens <- token; errs <- e }()
	}
	wg.Wait()
	close(tokens)
	close(errs)
	var token string
	for v := range tokens {
		if v != "" {
			if token != "" {
				t.Fatal("duplicate registration")
			}
			token = v
		}
	}
	var conflict int
	for e := range errs {
		if errors.Is(e, ErrCredentialConflict) {
			conflict++
		} else if e != nil {
			t.Fatal(e)
		}
	}
	if conflict != 1 || token == "" {
		t.Fatal("concurrent registration did not resolve uniquely")
	}
	id, err := s.Authenticate(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DELETE FROM accounts WHERE id=$1`, id)
	for i := 0; i < 5; i++ {
		if _, err = s.Login(ctx, username, "wrong-password"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatal(err)
		}
	}
	if _, err = s.Login(ctx, username, password); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("lock not enforced: %v", err)
	}
}

func TestLoginAndDeletionSerializePostgres(t *testing.T) {
	dsn := os.Getenv("NYANG_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("NYANG_TEST_DATABASE_URL not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	s := Store{DB: db}
	username := fmt.Sprintf("delete%d", time.Now().UnixNano())
	password := "test-password-1234"
	original, err := s.Register(ctx, username, password)
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.Authenticate(ctx, original)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DELETE FROM accounts WHERE id=$1`, id)
	var wg sync.WaitGroup
	var loginToken string
	var loginErr, deleteErr error
	wg.Add(2)
	go func() { defer wg.Done(); loginToken, loginErr = s.Login(ctx, username, password) }()
	go func() { defer wg.Done(); deleteErr = s.DeleteAccount(ctx, id) }()
	wg.Wait()
	if deleteErr != nil {
		t.Fatal(deleteErr)
	}
	if loginErr != nil && !errors.Is(loginErr, ErrInvalidCredentials) {
		t.Fatalf("login race: %v", loginErr)
	}
	if loginToken != "" {
		if _, err = s.Authenticate(ctx, loginToken); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("deleted account session revived: %v", err)
		}
	}
}

func TestLoginWaitsForHashCapacityBeforeAcquiringDatabase(t *testing.T) {
	for i := 0; i < cap(passwordSlots); i++ {
		passwordSlots <- struct{}{}
	}
	defer func() {
		for i := 0; i < cap(passwordSlots); i++ {
			<-passwordSlots
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	// A nil DB would panic if Login tried to acquire a connection while waiting.
	_, err := (Store{}).Login(ctx, "validname", "long-enough-password")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("capacity wait: %v", err)
	}
}
