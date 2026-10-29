package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/yuntyu01/nyangpuli-lab/internal/api"
	"github.com/yuntyu01/nyangpuli-lab/internal/evaluation"
	"github.com/yuntyu01/nyangpuli-lab/internal/generation"
	"github.com/yuntyu01/nyangpuli-lab/internal/lab"
	"github.com/yuntyu01/nyangpuli-lab/internal/memory"
	"github.com/yuntyu01/nyangpuli-lab/internal/persistence"
	"github.com/yuntyu01/nyangpuli-lab/internal/purchase"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: nyangpuli compare|serve|token [options]")
	}
	if args[0] == "token" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		fmt.Println(hex.EncodeToString(b))
		return nil
	}
	switch args[0] {
	case "compare":
		return compare(args[1:])
	case "serve":
		return serve(args[1:])
	default:
		return errors.New("unknown command")
	}
}
func compare(args []string) error {
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	domain := fs.String("domain", "tarot", "tarot, saju, daily, compatibility, counsel")
	input := fs.String("input", "", "JSON request file; - for stdin")
	caseID := fs.String("case", "synthetic-001", "stable fictional/anonymized case ID")
	root := fs.String("root", ".", "repository root (Saju adapter location)")
	generate := fs.Bool("generate", false, "opt in to configured provider calls; may incur cost")
	blindDir := fs.String("blind-out", "", "new private directory for blind review files; requires --generate")
	output := fs.String("out", "", "write full result to a NEW file instead of stdout")
	seed := fs.Int64("seed", 20261028, "blind label randomization seed")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *input == "" {
		return errors.New("--input is required")
	}
	if *blindDir != "" && !*generate {
		return errors.New("--blind-out requires --generate; evidence is not a reading-quality result")
	}
	if *blindDir != "" {
		if _, err := os.Stat(*blindDir); err == nil {
			return errors.New("blind output directory already exists; no model call made")
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if *output != "" {
		if _, err := os.Stat(*output); err == nil {
			return errors.New("output already exists; choose a new run path")
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	var reader io.Reader = os.Stdin
	if *input != "-" {
		f, err := os.Open(*input)
		if err != nil {
			return err
		}
		defer f.Close()
		reader = f
	}
	data, err := io.ReadAll(io.LimitReader(reader, 128*1024+1))
	if err != nil {
		return err
	}
	if len(data) > 128*1024 {
		return errors.New("input exceeds 128 KiB")
	}
	l, err := lab.New(*root)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	result, err := l.Compare(ctx, *domain, *caseID, data)
	if err != nil {
		return err
	}
	var client *generation.Client
	if *generate {
		cfg, e := generation.ConfigFromEnv()
		if e != nil {
			return e
		}
		client, e = generation.New(cfg, nil)
		if e != nil {
			return e
		}
	}
	generated, genErr := generation.Run(ctx, result.Inputs, client, *generate)
	envelope := struct {
		Comparison lab.Result          `json:"comparison"`
		Generation []generation.Result `json:"generation"`
	}{result, generated}
	b, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if *output == "" {
		_, err = os.Stdout.Write(b)
	} else {
		if err = os.MkdirAll(filepath.Dir(*output), 0700); err != nil {
			return err
		}
		var f *os.File
		f, err = os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err == nil {
			_, err = f.Write(b)
			closeErr := f.Close()
			if err == nil {
				err = closeErr
			}
		}
	}
	if err != nil {
		return err
	}
	if genErr != nil {
		return genErr
	}
	if *blindDir != "" {
		return evaluation.Export(*blindDir, generated, *seed)
	}
	return nil
}
func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:8080", "loopback address only; production deployment needs a reviewed gateway")
	allowContainerBind := fs.Bool("allow-container-bind", false, "allow non-loopback bind only with NYANG_ENV=development")
	root := fs.String("root", ".", "repository root")
	path := fs.String("memory", os.Getenv("NYANG_MEMORY_PATH"), "local JSON store (default data/private/memory.json)")
	readingPath := fs.String("readings", "", "private reading snapshots (default data/private/readings.json)")
	enableGeneration := fs.Bool("enable-generation", false, "enable provider calls in reading API using explicit LLM config; may incur cost")
	testUnlocks := fs.Bool("test-unlocks", false, "enable DEV-ONLY simulated detail unlock; no actual payment")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := validateBindAddress(*addr, *allowContainerBind, os.Getenv("NYANG_ENV")); err != nil {
		return err
	}
	// PostgreSQL session authentication is opt-in while the existing lab
	// storage is being migrated. Do not accept static lab tokens in production.
	production := os.Getenv("NYANG_ENV") == "production"
	dsn := os.Getenv("DATABASE_URL")
	if *testUnlocks && os.Getenv("NYANG_ENV") != "development" {
		return errors.New("test unlocks require NYANG_ENV=development")
	}
	if production {
		return errors.New("production mode is unavailable until private data storage and account recovery are migrated")
	}
	var err error
	var db *sql.DB
	var tokens map[string]string
	if dsn != "" {
		db, err = sql.Open("pgx", dsn)
		if err != nil {
			return err
		}
		defer db.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err = db.PingContext(ctx); err != nil {
			return fmt.Errorf("database connection failed: %w", err)
		}
		if err = persistence.Migrate(ctx, db); err != nil {
			return fmt.Errorf("database migration failed: %w", err)
		}
	} else {
		tokens, err = tokenUsers()
		if err != nil {
			return err
		}
	}
	if *path == "" {
		*path = filepath.Join(*root, "data", "private", "memory.json")
	}
	var store api.MemoryStore
	if db != nil {
		// Existing JSON data must be explicitly migrated; never silently ignore it.
		if _, statErr := os.Stat(*path); statErr == nil {
			return errors.New("existing JSON memory requires explicit migration before database mode")
		} else if !os.IsNotExist(statErr) {
			return statErr
		}
		store = &memory.PostgresStore{DB: db}
	} else {
		store, err = memory.Open(*path)
		if err != nil {
			return err
		}
	}
	l, err := lab.New(*root)
	if err != nil {
		return err
	}
	app := &api.Server{Lab: l, Memory: store, Tokens: tokens}
	if db != nil {
		app.Auth = persistence.Store{DB: db}
		app.AccountDeletion = persistence.Store{DB: db}
		app.TarotStore = persistence.Store{DB: db}
	}
	var client *generation.Client
	if *enableGeneration {
		config, e := generation.ConfigFromEnv()
		if e != nil {
			return e
		}
		client, e = generation.New(config, nil)
		if e != nil {
			return e
		}
	}
	if *readingPath == "" {
		*readingPath = filepath.Join(*root, "data", "private", "readings.json")
	}
	if db != nil {
		if _, statErr := os.Stat(*readingPath); statErr == nil {
			return errors.New("existing JSON readings require explicit migration before database mode")
		} else if !os.IsNotExist(statErr) {
			return statErr
		}
		app.Readings = api.NewPostgresReadingService(db, l, store, client, *testUnlocks)
		if *testUnlocks {
			app.DevelopmentPurchaser = purchase.Development{DB: db}
		}
	} else {
		app.Readings, err = api.NewReadingService(l, store, client, *readingPath)
		if err != nil {
			return err
		}
	}
	app.AllowTestUnlock = *testUnlocks
	if base := os.Getenv("MEM0_BASE_URL"); base != "" {
		app.Mem0, err = memory.NewMem0(base, os.Getenv("MEM0_API_KEY"), nil)
		if err != nil {
			return err
		}
	}
	server := &http.Server{Addr: *addr, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	fmt.Fprintln(os.Stderr, "냥풀이 연구 API:", *addr, "model_generation_enabled=", *enableGeneration, "simulated_unlock_enabled=", *testUnlocks)
	if err = server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Development containers need a non-loopback listener for port forwarding.
// This explicit opt-in is never enabled solely by the environment variable.
func validateBindAddress(addr string, allowContainerBind bool, environment string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if host == "localhost" || (ip != nil && ip.IsLoopback()) {
		return nil
	}
	if allowContainerBind && environment == "development" && (host == "" || (ip != nil && ip.IsUnspecified())) {
		return nil
	}
	return errors.New("non-loopback bind requires --allow-container-bind and NYANG_ENV=development")
}
func tokenUsers() (map[string]string, error) {
	users := map[string]string{}
	if raw := os.Getenv("NYANG_USERS_JSON"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &users); err != nil {
			return nil, errors.New("NYANG_USERS_JSON must map user IDs to tokens")
		}
	} else {
		user := os.Getenv("NYANG_USER_ID")
		if user == "" {
			user = "local-demo"
		}
		users[user] = os.Getenv("NYANG_API_TOKEN")
	}
	tokens := map[string]string{}
	for user, token := range users {
		if strings.TrimSpace(user) == "" || len(user) > 256 || len(token) < 32 || strings.HasPrefix(token, "replace-") {
			return nil, errors.New("configure unique random tokens of at least 32 characters; run nyangpuli token")
		}
		if _, ok := tokens[token]; ok {
			return nil, errors.New("each user needs a distinct token")
		}
		tokens[token] = user
	}
	if len(tokens) == 0 {
		return nil, errors.New("at least one authenticated user is required")
	}
	return tokens, nil
}
