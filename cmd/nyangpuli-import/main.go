// nyangpuli-import migrates explicitly mapped private lab JSON data.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/yuntyu01/nyangpuli-lab/internal/legacyimport"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "import failed:", err)
		os.Exit(1)
	}
}
func run() error {
	memoryPath := flag.String("memory", "", "legacy memory JSON file")
	readingsPath := flag.String("readings", "", "legacy readings JSON file")
	mappingPath := flag.String("mapping", "", "JSON object mapping every source user to a target account")
	apply := flag.Bool("apply", false, "commit the import; default validates and rolls back")
	flag.Parse()
	if *mappingPath == "" {
		return fmt.Errorf("mapping file is required")
	}
	b, err := os.ReadFile(*mappingPath)
	if err != nil {
		return fmt.Errorf("cannot read mapping file")
	}
	if len(b) > 1<<20 {
		return fmt.Errorf("mapping file too large")
	}
	var mapping map[string]string
	if json.Unmarshal(b, &mapping) != nil {
		return fmt.Errorf("invalid mapping file")
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("database initialization failed")
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	r, err := legacyimport.Import(ctx, db, legacyimport.Options{MemoryPath: *memoryPath, ReadingsPath: *readingsPath, Users: mapping, DryRun: !*apply})
	if err != nil {
		return err
	}
	// Counts only; never output credentials, identifiers or personal content.
	fmt.Printf("dry_run=%t already_imported=%t users=%d memories=%d readings=%d\n", r.DryRun, r.AlreadyImported, r.Users, r.MemoryRecords, r.Readings)
	return nil
}
