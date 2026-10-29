package readings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/yuntyu01/nyangpuli-lab/internal/persistence"
	"github.com/yuntyu01/nyangpuli-lab/internal/purchase"
)

func waitDetailState(t *testing.T, s *PostgresService, user, id, want string) JobStatus {
	t.Helper()
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		j, e := s.DetailStatus(context.Background(), user, id)
		if e != nil {
			t.Fatal(e)
		}
		if j.State == want {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("detail did not reach", want)
	return JobStatus{}
}
func TestAsyncDetailPostgresReconnectRefundAndDelete(t *testing.T) {
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
	if err = persistence.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	user := fmt.Sprintf("async-detail-%d", time.Now().UnixNano())
	other := user + "-b"
	if _, err = db.Exec(`INSERT INTO accounts(id) VALUES($1),($2)`, user, other); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DELETE FROM accounts WHERE id IN ($1,$2)`, user, other)
	summaryGenerator := &pgGenerator{}
	base := &PostgresService{DB: db, Builder: pgBuilder{}, Generator: summaryGenerator, AllowTestUnlock: true}
	input := json.RawMessage(`{"q":"synthetic"}`)
	summary, err := base.Create(ctx, user, "summary-a", "counsel", "baseline", input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = base.StartDetail(ctx, user, summary.ID); !errors.Is(err, ErrLocked) {
		t.Fatalf("unpaid %v", err)
	}
	dev := purchase.Development{DB: db}
	transaction := "test-" + user
	if err = dev.Simulate(ctx, user, summary.ID, transaction, purchase.Purchased); err != nil {
		t.Fatal(err)
	}
	worker := &pgGenerator{started: make(chan struct{}, 2), release: make(chan struct{})}
	service := &PostgresService{DB: db, Builder: pgBuilder{}, Generator: worker, AllowTestUnlock: true}
	requestCtx, cancel := context.WithCancel(ctx)
	job, err := service.StartDetail(requestCtx, user, summary.ID)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if job.ID == "" || job.State != "pending_or_interrupted" || job.Summary != nil {
		t.Fatalf("start %+v", job)
	}
	select {
	case <-worker.started:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not start")
	}
	var wg sync.WaitGroup
	out := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			duplicate, e := service.StartDetail(ctx, user, summary.ID)
			if e == nil && (duplicate.ID != job.ID || duplicate.State != "pending_or_interrupted") {
				e = fmt.Errorf("unexpected duplicate")
			}
			out <- e
		}()
	}
	wg.Wait()
	close(out)
	for e := range out {
		if e != nil {
			t.Fatal(e)
		}
	}
	if worker.calls.Load() != 1 {
		t.Fatalf("duplicate calls: %d", worker.calls.Load())
	}
	if _, err = service.DetailStatus(ctx, other, summary.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other user %v", err)
	}
	if _, err = service.Detail(ctx, user, summary.ID); !errors.Is(err, ErrOutcomeUnknown) {
		t.Fatalf("synchronous duplicate %v", err)
	}
	close(worker.release)
	ready := waitDetailState(t, service, user, summary.ID, "ready")
	if ready.Summary != nil || ready.ID != job.ID {
		t.Fatalf("status disclosed result %+v", ready)
	}
	restored := &PostgresService{DB: db, AllowTestUnlock: true}
	read, err := restored.Get(user, summary.ID)
	if err != nil || read.Detail != "detail" || read.Summary != "summary" {
		t.Fatalf("restore %+v %v", read, err)
	}
	// Refund and result read use the same account lock: a reader starting
	// after the refund acquires the lock must see the committed revocation.
	refundTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer refundTx.Rollback()
	var lockedAccount string
	if err = refundTx.QueryRowContext(ctx, `SELECT id FROM accounts WHERE id=$1 FOR UPDATE`, user).Scan(&lockedAccount); err != nil {
		t.Fatal(err)
	}
	if _, err = refundTx.ExecContext(ctx, `UPDATE dev_purchase_transactions SET status='refunded',revision=3 WHERE transaction_id=$1`, transaction); err != nil {
		t.Fatal(err)
	}
	type readResult struct {
		value View
		err   error
	}
	reads := make(chan readResult, 1)
	go func() { value, e := restored.Get(user, summary.ID); reads <- readResult{value, e} }()
	select {
	case <-reads:
		t.Fatal("read bypassed in-progress refund lock")
	case <-time.After(100 * time.Millisecond):
	}
	if err = refundTx.Commit(); err != nil {
		t.Fatal(err)
	}
	afterRefund := <-reads
	if afterRefund.err != nil || afterRefund.value.Detail != "" || !afterRefund.value.DetailLocked {
		t.Fatalf("refund snapshot leak: %+v %v", afterRefund.value, afterRefund.err)
	}
	if _, err = restored.DetailStatus(ctx, user, summary.ID); !errors.Is(err, ErrLocked) {
		t.Fatalf("refunded status %v", err)
	}
	read, err = restored.Get(user, summary.ID)
	if err != nil || read.Detail != "" || !read.DetailLocked {
		t.Fatalf("refunded result %+v %v", read, err)
	}

	// A refund during the provider call must not disclose the resulting detail.
	second, err := base.Create(ctx, user, "summary-b", "counsel", "baseline", input)
	if err != nil {
		t.Fatal(err)
	}
	tx2 := transaction + "-2"
	if err = dev.Simulate(ctx, user, second.ID, tx2, purchase.Purchased); err != nil {
		t.Fatal(err)
	}
	g2 := &pgGenerator{started: make(chan struct{}, 1), release: make(chan struct{})}
	s2 := &PostgresService{DB: db, Generator: g2, AllowTestUnlock: true}
	j2, err := s2.StartDetail(ctx, user, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	<-g2.started
	if err = dev.Simulate(ctx, user, second.ID, tx2, purchase.Refunded); err != nil {
		t.Fatal(err)
	}
	close(g2.release)
	deadline := time.Now().Add(5 * time.Second)
	for {
		var state string
		if err = db.QueryRow(`SELECT state FROM generation_jobs WHERE id=$1`, j2.ID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "ready" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err = s2.DetailStatus(ctx, user, second.ID); !errors.Is(err, ErrLocked) {
		t.Fatalf("refund during call %v", err)
	}
	v, err := s2.Get(user, second.ID)
	if err != nil || v.Detail != "" || !v.DetailLocked {
		t.Fatalf("refund leak %+v %v", v, err)
	}

	// A refund before the provider call is known not to have incurred cost.
	// A new entitlement can re-arm exactly that job without duplicate calls.
	noCall, err := base.Create(ctx, user, "summary-not-started", "counsel", "baseline", input)
	if err != nil {
		t.Fatal(err)
	}
	tx3 := transaction + "-3"
	if err = dev.Simulate(ctx, user, noCall.ID, tx3, purchase.Purchased); err != nil {
		t.Fatal(err)
	}
	before, err := base.getRecord(ctx, user, noCall.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeVersion, err := base.readJobMemoryGeneration(ctx, user, noCall.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeEvidence, _ := json.Marshal(before)
	reserved, created, err := (persistence.Store{DB: db}).ClaimDetailJobWithMemory(ctx, user, "never-called-"+user, "detail:"+noCall.ID, before.RequestHash, beforeEvidence, beforeVersion, noCall.ID, true)
	if err != nil || !created {
		t.Fatalf("reserve: %v", err)
	}
	if err = dev.Simulate(ctx, user, noCall.ID, tx3, purchase.Refunded); err != nil {
		t.Fatal(err)
	}
	neverCalled := &pgGenerator{}
	preflight := &PostgresService{DB: db, Generator: neverCalled, AllowTestUnlock: true}
	preflight.generateDetail(user, noCall.ID, before, reserved.ID, reserved.DeletionGeneration)
	var notStartedState string
	if err = db.QueryRow(`SELECT state FROM generation_jobs WHERE id=$1`, reserved.ID).Scan(&notStartedState); err != nil || notStartedState != "not_started" || neverCalled.calls.Load() != 0 {
		t.Fatalf("before provider: %s %v", notStartedState, err)
	}
	if err = dev.Simulate(ctx, user, noCall.ID, transaction+"-new", purchase.Purchased); err != nil {
		t.Fatal(err)
	}
	rearmGenerator := &pgGenerator{started: make(chan struct{}, 1), release: make(chan struct{})}
	rearm := &PostgresService{DB: db, Generator: rearmGenerator, AllowTestUnlock: true}
	beforeRetry, err := rearm.DetailStatus(ctx, user, noCall.ID)
	if err != nil || beforeRetry.State != "not_started" {
		t.Fatalf("status before retry: %+v %v", beforeRetry, err)
	}
	rearmed, err := rearm.StartDetail(ctx, user, noCall.ID)
	if err != nil || rearmed.ID != reserved.ID || rearmed.State != "pending_or_interrupted" {
		t.Fatalf("rearm: %+v %v", rearmed, err)
	}
	<-rearmGenerator.started
	if _, err = rearm.StartDetail(ctx, user, noCall.ID); err != nil {
		t.Fatal(err)
	}
	if rearmGenerator.calls.Load() != 1 {
		t.Fatalf("rearmed duplicate: %d", rearmGenerator.calls.Load())
	}
	close(rearmGenerator.release)
	waitDetailState(t, rearm, user, noCall.ID, "ready")
	// The original synchronous endpoint must also finish the existing job ID
	// after a proven no-call job is re-armed.
	syncCase, err := base.Create(ctx, user, "summary-sync-rearm", "counsel", "baseline", input)
	if err != nil {
		t.Fatal(err)
	}
	syncTx := transaction + "-sync"
	if err = dev.Simulate(ctx, user, syncCase.ID, syncTx, purchase.Purchased); err != nil {
		t.Fatal(err)
	}
	syncRecord, err := base.getRecord(ctx, user, syncCase.ID)
	if err != nil {
		t.Fatal(err)
	}
	syncVersion, err := base.readJobMemoryGeneration(ctx, user, syncCase.ID)
	if err != nil {
		t.Fatal(err)
	}
	syncEvidence, _ := json.Marshal(syncRecord)
	syncJob, created, err := (persistence.Store{DB: db}).ClaimDetailJobWithMemory(ctx, user, "sync-no-call-"+user, "detail:"+syncCase.ID, syncRecord.RequestHash, syncEvidence, syncVersion, syncCase.ID, true)
	if err != nil || !created {
		t.Fatalf("sync reserve: %v", err)
	}
	if err = dev.Simulate(ctx, user, syncCase.ID, syncTx, purchase.Refunded); err != nil {
		t.Fatal(err)
	}
	preflight.generateDetail(user, syncCase.ID, syncRecord, syncJob.ID, syncJob.DeletionGeneration)
	if err = dev.Simulate(ctx, user, syncCase.ID, transaction+"-sync-new", purchase.Purchased); err != nil {
		t.Fatal(err)
	}
	syncDetail, err := base.Detail(ctx, user, syncCase.ID)
	if err != nil || syncDetail.Detail != "detail" {
		t.Fatalf("synchronous rearm: %+v %v", syncDetail, err)
	}
	var syncState string
	if err = db.QueryRow(`SELECT state FROM generation_jobs WHERE id=$1`, syncJob.ID).Scan(&syncState); err != nil || syncState != "ready" {
		t.Fatalf("existing job not saved: %s %v", syncState, err)
	}
	// A pending claim left by a stopped process is never automatically retried.
	interrupted, err := base.Create(ctx, user, "summary-interrupted", "counsel", "baseline", input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = base.UnlockForTest(user, interrupted.ID); err != nil {
		t.Fatal(err)
	}
	prior, err := base.getRecord(ctx, user, interrupted.ID)
	if err != nil {
		t.Fatal(err)
	}
	version, err := base.readJobMemoryGeneration(ctx, user, interrupted.ID)
	if err != nil {
		t.Fatal(err)
	}
	evidence, _ := json.Marshal(prior)
	pending, created, err := (persistence.Store{DB: db}).ClaimDetailJobWithMemory(ctx, user, "interrupted-"+user, "detail:"+interrupted.ID, prior.RequestHash, evidence, version, interrupted.ID, true)
	if err != nil || !created {
		t.Fatalf("prepare pending: %v", err)
	}
	crashGenerator := &pgGenerator{}
	afterRestart := &PostgresService{DB: db, Generator: crashGenerator, AllowTestUnlock: true}
	replayed, err := afterRestart.StartDetail(ctx, user, interrupted.ID)
	if err != nil || replayed.ID != pending.ID || replayed.State != "pending_or_interrupted" || crashGenerator.calls.Load() != 0 {
		t.Fatalf("interrupted retry: %+v %v", replayed, err)
	}
	// Account deletion while generating removes the job and prevents resurrection.
	third, err := base.Create(ctx, other, "summary-c", "counsel", "baseline", input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = base.UnlockForTest(other, third.ID); err != nil {
		t.Fatal(err)
	}
	g3 := &pgGenerator{started: make(chan struct{}, 1), release: make(chan struct{})}
	s3 := &PostgresService{DB: db, Generator: g3, AllowTestUnlock: true}
	j3, err := s3.StartDetail(ctx, other, third.ID)
	if err != nil {
		t.Fatal(err)
	}
	<-g3.started
	if err = (persistence.Store{DB: db}).DeleteAccount(ctx, other); err != nil {
		t.Fatal(err)
	}
	close(g3.release)
	// Even if a stopped worker later attempts to save, the database guard must
	// reject it after deletion independently of goroutine scheduling.
	if err = (persistence.Store{DB: db}).FinishJob(ctx, other, j3.ID, 0, "ready", json.RawMessage(`{"detail":"private"}`)); !errors.Is(err, persistence.ErrJobNotPending) {
		t.Fatalf("late save allowed: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM generation_jobs WHERE id=$1`, j3.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("resurrected %d %v", count, err)
	}
	if _, err = s3.DetailStatus(ctx, other, third.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted %v", err)
	}
}
