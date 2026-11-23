package subset

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/greenmaskio/greenmask/internal/db/postgres/entries"
	"github.com/greenmaskio/greenmask/pkg/toolkit"
)

// TestForeignKeyTargetsUnique exercises the reported #548 shape on a real
// PostgreSQL server. All DDL and data are rolled back after the test.
func TestForeignKeyTargetsUnique(t *testing.T) {
	dsn := os.Getenv("GREENMASK_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("set GREENMASK_TEST_PG_DSN for the PostgreSQL regression")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())

	statements := []string{
		`CREATE SCHEMA gm_fk_unique_548`,
		`CREATE TABLE gm_fk_unique_548.parent (
            id int PRIMARY KEY, tenant_id int NOT NULL, UNIQUE (id, tenant_id)
        )`,
		`CREATE TABLE gm_fk_unique_548.child (
            id int PRIMARY KEY, parent_id int NOT NULL, tenant_id int NOT NULL,
            CONSTRAINT fk_simple FOREIGN KEY (parent_id)
                REFERENCES gm_fk_unique_548.parent (id),
            CONSTRAINT fk_composite FOREIGN KEY (parent_id, tenant_id)
                REFERENCES gm_fk_unique_548.parent (id, tenant_id)
        )`,
		`INSERT INTO gm_fk_unique_548.parent VALUES (10,1), (20,2)`,
		`INSERT INTO gm_fk_unique_548.child VALUES (100,10,1), (200,20,2)`,
	}
	for _, query := range statements {
		if _, err := tx.Exec(ctx, query); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}

	var parentOid, childOid uint32
	if err := tx.QueryRow(ctx, `SELECT 'gm_fk_unique_548.parent'::regclass::oid`).Scan(&parentOid); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `SELECT 'gm_fk_unique_548.child'::regclass::oid`).Scan(&childOid); err != nil {
		t.Fatal(err)
	}

	tables := []*entries.Table{
		{Table: &toolkit.Table{Schema: "gm_fk_unique_548", Name: "parent", Oid: toolkit.Oid(parentOid), PrimaryKey: []string{"id"}}},
		{Table: &toolkit.Table{Schema: "gm_fk_unique_548", Name: "child", Oid: toolkit.Oid(childOid), PrimaryKey: []string{"id"}}},
	}

	assertGraph := func(expectSimple bool) *Edge {
		t.Helper()
		graph, err := NewGraph(ctx, tx, tables, nil)
		if err != nil {
			t.Fatal(err)
		}
		want := 1
		if expectSimple {
			want = 2
		}
		if len(graph.graph[1]) != want {
			t.Fatalf("expected %d FK edges, got %d", want, len(graph.graph[1]))
		}
		var composite *Edge
		for _, edge := range graph.graph[1] {
			from, to := edge.From().Keys(), edge.To().Keys()
			if len(from) != len(to) {
				t.Fatalf("invalid FK arity: child=%d parent=%d", len(from), len(to))
			}
			if len(from) == 2 {
				if from[0].Name != "parent_id" || from[1].Name != "tenant_id" ||
					to[0].Name != "id" || to[1].Name != "tenant_id" {
					t.Fatalf("wrong composite key pairing: child=%+v parent=%+v", from, to)
				}
				composite = edge
			} else if len(from) != 1 || from[0].Name != "parent_id" || to[0].Name != "id" {
				t.Fatalf("wrong simple FK pairing: child=%+v parent=%+v", from, to)
			}
		}
		if composite == nil {
			t.Fatal("composite UNIQUE-key FK was not found")
		}
		// Exercise the actual subset query, not just the isolated JOIN.
		// This reproduces both reported #548 failure modes.
		tables[0].SubsetConds = []string{`"gm_fk_unique_548"."parent"."tenant_id" = 1`}
		if err := SetSubsetQueries(graph); err != nil {
			t.Fatal(err)
		}
		generated := tables[1].Query
		if generated == "" {
			t.Fatal("no subset query generated for child")
		}
		rows, err := tx.Query(ctx, generated)
		if err != nil {
			t.Fatalf("execute complete subset SQL: %v; SQL: %s", err, generated)
		}
		var result []int
		for rows.Next() {
			var id, parentID, tenantID int
			if err := rows.Scan(&id, &parentID, &tenantID); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			result = append(result, id)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		rows.Close()
		if len(result) != 1 || result[0] != 100 {
			t.Fatalf("wrong subset rows: %v; SQL: %s", result, generated)
		}
		return composite
	}

	edge := assertGraph(true)
	join := generateJoinClauseV2(edge, joinTypeInner, nil)
	if !strings.Contains(join, `"gm_fk_unique_548"."child"."tenant_id" = "gm_fk_unique_548"."parent"."tenant_id"`) {
		t.Fatalf("generated join ignored the referenced tenant key: %s", join)
	}
	rows, err := tx.Query(ctx,
		`SELECT "gm_fk_unique_548"."child"."id" FROM "gm_fk_unique_548"."child" `+
			join+` WHERE "gm_fk_unique_548"."parent"."tenant_id" = 1`)
	if err != nil {
		t.Fatalf("execute generated join: %v; SQL=%s", err, join)
	}
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	if len(ids) != 1 || ids[0] != 100 {
		t.Fatalf("tenant isolation failed: %v", ids)
	}

	// Both constraints coexist in the first case. Without the simple FK,
	// the old query builder would panic indexing the one-column parent PK.
	if _, err := tx.Exec(ctx, `ALTER TABLE gm_fk_unique_548.child DROP CONSTRAINT fk_simple`); err != nil {
		t.Fatal(err)
	}
	assertGraph(false)
}
