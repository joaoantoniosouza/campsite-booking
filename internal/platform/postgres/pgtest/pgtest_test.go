//go:build integration

package pgtest

import (
	"context"
	"sync"
	"testing"

	"github.com/campsite-booking/campsite-booking/internal/platform/postgres"
)

func TestSetup_ReturnsMigratedPool(t *testing.T) {
	pool := Setup(t)

	var extExists bool
	err := pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'btree_gist')`).Scan(&extExists)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if !extExists {
		t.Error("expected btree_gist extension to be present (migrations applied)")
	}
}

func TestSetup_IsolatesStateBetweenSubtests(t *testing.T) {
	t.Run("A writes a row", func(t *testing.T) {
		pool := Setup(t)
		_, err := pool.Exec(context.Background(),
			`CREATE TABLE IF NOT EXISTS pgtest_isolation (id INT PRIMARY KEY)`)
		if err != nil {
			t.Fatalf("create table failed: %v", err)
		}
		_, err = pool.Exec(context.Background(), `INSERT INTO pgtest_isolation (id) VALUES (1)`)
		if err != nil {
			t.Fatalf("insert failed: %v", err)
		}
	})

	t.Run("B does not see A's row after restore", func(t *testing.T) {
		pool := Setup(t)

		var tableExists bool
		err := pool.QueryRow(context.Background(),
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'pgtest_isolation')`,
		).Scan(&tableExists)
		if err != nil {
			t.Fatalf("query failed: %v", err)
		}
		if tableExists {
			t.Error("expected subtest B to not see subtest A's table after Restore")
		}
	})
}

func TestSetup_ConcurrentCommittedInsertsAllLand(t *testing.T) {
	pool := Setup(t)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS pgtest_concurrency (id INT PRIMARY KEY)`); err != nil {
		t.Fatalf("create table failed: %v", err)
	}

	const n = 10
	var wg sync.WaitGroup
	errs := make(chan error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			err := postgres.WithTx(ctx, pool, func(txCtx context.Context) error {
				_, err := postgres.Executor(txCtx, pool).Exec(txCtx,
					`INSERT INTO pgtest_concurrency (id) VALUES ($1)`, id)
				return err
			})
			errs <- err
		}(i)
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent WithTx insert failed: %v", err)
		}
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pgtest_concurrency`).Scan(&count); err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != n {
		t.Errorf("expected %d rows, got %d", n, count)
	}
}
