// Copyright 2026 Greenmask
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pool

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	core "github.com/greenmaskio/greenmask/pkg/common/core"
)

// A dump snapshot must remain pinned even if no InnoDB table was read while
// the global read lock was held.
func TestDumpPoolInnoDBReadViews(t *testing.T) {
	dsn := os.Getenv("GREENMASK_VIRTUAL_KEY_MYSQL_DSN")
	if dsn == "" {
		t.Skip("isolated MySQL integration service is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(5)

	const table = "gm_snapshot_pool_319"
	if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS "+table); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+table+" (id INT PRIMARY KEY) ENGINE=InnoDB"); err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+table)
	if _, err := db.ExecContext(ctx, "INSERT INTO "+table+" VALUES (1)"); err != nil {
		t.Fatal(err)
	}

	cfg, err := connConfigFromURI(dsn)
	if err != nil {
		t.Fatal(err)
	}
	p := NewConsistentTxPool(cfg, 2)
	if err := p.Init(ctx); err != nil {
		t.Fatal(err)
	}
	defer p.Close(context.Background())

	// This row commits after FTWRL is released but before the first
	// InnoDB data read on every session.
	if _, err := db.ExecContext(ctx, "INSERT INTO "+table+" VALUES (2)"); err != nil {
		t.Fatal(err)
	}

	err = p.RunWithOperationalDB(ctx, func(ctx context.Context, tx core.DB) error {
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("metadata session saw %d rows; snapshot should contain only the original row", count)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for workerIndex := 0; workerIndex < 2; workerIndex++ {
		err := p.RunWithConn(ctx, func(_ context.Context, conn WorkerConn) error {
			rows, err := conn.RawConn().Execute("SELECT COUNT(*) FROM " + table)
			if err != nil {
				return err
			}
			count, err := rows.GetInt(0, 0)
			if err != nil {
				return err
			}
			if count != 1 {
				return fmt.Errorf("worker %d saw %d rows; snapshot should contain only the original row", conn.ID(), count)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
