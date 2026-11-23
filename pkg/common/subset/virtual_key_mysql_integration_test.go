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

package subset

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	core "github.com/greenmaskio/greenmask/pkg/common/core"
	"github.com/greenmaskio/greenmask/pkg/common/graphbuilder"
	"github.com/greenmaskio/greenmask/pkg/common/subsetbuilder"
	mysqlmodels "github.com/greenmaskio/greenmask/pkg/mysql/models"
)

// pinnedSubsetSession adapts the test's InnoDB transaction to the dump session
// contract. The production MySQL session supplies the same scoped interface.
type pinnedSubsetSession struct{ tx *sql.Tx }

func (s pinnedSubsetSession) RunWithOperationalDB(ctx context.Context, fn func(context.Context, core.DB) error) error {
	return fn(ctx, s.tx)
}

func (s pinnedSubsetSession) RunWithEngineResource(context.Context, func(context.Context, any) error) error {
	return core.ErrEngineResourceNotSupported
}

func (s pinnedSubsetSession) Close(context.Context) error { return nil }

// The CI job supplies an isolated MySQL 8.4 service. No user database is used.
func TestVirtualKeyMySQLSnapshotAndSubset(t *testing.T) {
	dsn := os.Getenv("GREENMASK_VIRTUAL_KEY_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set GREENMASK_VIRTUAL_KEY_MYSQL_DSN for the MySQL integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(4)

	const schema = "greenmask_ci"
	const customers = "gm_virtual_customer_319"
	const invoices = "gm_virtual_invoice_319"
	for _, statement := range []string{
		"DROP TABLE IF EXISTS `" + schema + "`.`" + invoices + "`",
		"DROP TABLE IF EXISTS `" + schema + "`.`" + customers + "`",
		"CREATE TABLE `" + schema + "`.`" + customers + "` (tenant_id INT NULL, code VARCHAR(40) NULL) ENGINE=InnoDB",
		"CREATE TABLE `" + schema + "`.`" + invoices + "` (id INT PRIMARY KEY, tenant_id INT NOT NULL, code VARCHAR(40) NOT NULL) ENGINE=InnoDB",
		"INSERT INTO `" + schema + "`.`" + customers + "` VALUES (1, 'shared'), (2, 'shared'), (1, 'other')",
		"INSERT INTO `" + schema + "`.`" + invoices + "` VALUES (11, 1, 'shared'), (22, 2, 'shared'), (33, 1, 'other')",
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	defer func() {
		db.ExecContext(context.Background(), "DROP TABLE IF EXISTS `"+schema+"`.`"+invoices+"`")
		db.ExecContext(context.Background(), "DROP TABLE IF EXISTS `"+schema+"`.`"+customers+"`")
	}()

	parent := core.Table{
		ID: 0, Schema: schema, Name: customers,
		Columns:          []core.Column{{Name: "tenant_id"}, {Name: "code"}},
		SubsetConditions: []string{"`" + schema + "`.`" + customers + "`.`tenant_id` = 1"},
	}
	child := core.Table{
		ID: 1, Schema: schema, Name: invoices,
		PrimaryKey: []string{"id"},
		Columns:    []core.Column{{Name: "id", NotNull: true}, {Name: "tenant_id", NotNull: true}, {Name: "code", NotNull: true}},
	}
	tables := []core.Table{parent, child}
	definitions := []core.VirtualPrimaryKey{{Schema: schema, Name: customers, Columns: []string{"tenant_id", "code"}}}
	relation := []core.VirtualReference{{
		FromSchema: schema, FromTable: invoices, ToSchema: schema, ToTable: customers,
		Columns: []string{"tenant_id", "code"},
	}}

	begin := func() *sql.Tx {
		t.Helper()
		tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		return tx
	}
	first := begin()
	// Establish InnoDB's actual read view before a concurrent writer commits.
	var total int
	if err := first.QueryRowContext(ctx, "SELECT COUNT(*) FROM `"+schema+"`.`"+customers+"`").Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Fatalf("unexpected initial count %d", total)
	}

	// Verify the refactored v1 planning path, not a second legacy subset engine.
	const tableKind core.ObjectKind = "mysql.table"
	physical := core.IntrospectionResult{
		Engine: core.DBMSEngineMySQL,
		KindsMap: map[core.ObjectKind][]core.Object{
			tableKind: {
				{ID: 0, Kind: tableKind, Name: customers, Payload: &mysqlmodels.Table{
					ID: 0, Schema: schema, Name: customers,
					Columns: []mysqlmodels.Column{{Name: "tenant_id"}, {Name: "code"}},
				}},
				{ID: 1, Kind: tableKind, Name: invoices, Payload: &mysqlmodels.Table{
					ID: 1, Schema: schema, Name: invoices, PrimaryKey: []string{"id"},
					Columns: []mysqlmodels.Column{{Name: "id", NotNull: true}, {Name: "tenant_id", NotNull: true}, {Name: "code", NotNull: true}},
				}},
			},
		},
	}
	planning, err := VerifiedPlanningIntrospection(
		ctx, pinnedSubsetSession{first}, DialectMySQL,
		physical, tableKind, definitions, relation,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(physical.KindsMap[tableKind][1].Payload.(*mysqlmodels.Table).References) != 0 {
		t.Fatal("virtual relationships leaked into physical introspection")
	}
	if len(planning.KindsMap[tableKind][1].Payload.(core.Table).References) != 1 {
		t.Fatal("planning view did not receive the virtual relationship")
	}
	dependencyGraph, err := graphbuilder.New(tableKind).BuildGraph(ctx, planning)
	if err != nil {
		t.Fatal(err)
	}
	if len(dependencyGraph.ObjectGraph.Edges[1]) != 1 {
		t.Fatalf("expected one virtual edge, got %+v", dependencyGraph.ObjectGraph.Edges)
	}
	subsetResult, err := subsetbuilder.New(subsetbuilder.DialectMySQL, tableKind).BuildSubset(
		ctx, core.SubsetBuilderInput{
			Introspection:   physical,
			DependencyGraph: dependencyGraph,
			TableConfigs: []core.TableConfig{{
				Schema: schema, Name: customers, SubsetConds: parent.SubsetConditions,
			}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	v1SQL := subsetResult.SubsetMap[1]
	if v1SQL == "" {
		t.Fatal("v1 subset builder returned no invoice SQL")
	}
	v1Rows, err := first.QueryContext(ctx, v1SQL)
	if err != nil {
		t.Fatalf("v1 subset query failed (%s): %v", v1SQL, err)
	}
	var v1IDs []int
	for v1Rows.Next() {
		var id, tenant int
		var code string
		if err := v1Rows.Scan(&id, &tenant, &code); err != nil {
			t.Fatal(err)
		}
		if tenant != 1 {
			t.Fatalf("v1 query selected a cross-tenant row: %+v", id)
		}
		v1IDs = append(v1IDs, id)
	}
	if err := v1Rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := v1Rows.Close(); err != nil {
		t.Fatal(err)
	}
	if len(v1IDs) != 2 {
		t.Fatalf("expected two tenant-1 rows from v1 builder, got %v; SQL: %s", v1IDs, v1SQL)
	}

	// A write after the snapshot must not invalidate the key within this read
	// transaction. A new transaction must detect the new duplicate.
	if _, err := db.ExecContext(ctx, "INSERT INTO `"+schema+"`.`"+customers+"` VALUES (1, 'shared')"); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyVirtualPrimaryKeys(ctx, first, DialectMySQL, tables, definitions); err != nil {
		t.Fatalf("snapshot changed after a concurrent write: %v", err)
	}
	if err := first.Rollback(); err != nil {
		t.Fatal(err)
	}

	second := begin()
	_, err = VerifyVirtualPrimaryKeys(ctx, second, DialectMySQL, tables, definitions)
	if err == nil || !strings.Contains(err.Error(), "duplicate tuples") {
		t.Fatalf("expected duplicate rejection, got %v", err)
	}
	second.Rollback()

	if _, err := db.ExecContext(ctx, "DELETE FROM `"+schema+"`.`"+customers+"` WHERE tenant_id = 1 AND code = 'shared' LIMIT 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO `"+schema+"`.`"+invoices+"` VALUES (44, 1, 'orphan')"); err != nil {
		t.Fatal(err)
	}
	orphanSnapshot := begin()
	_, err = VerifiedPlanningIntrospection(ctx, pinnedSubsetSession{orphanSnapshot}, DialectMySQL, physical, tableKind, definitions, relation)
	if err == nil || !strings.Contains(err.Error(), "no matching parent") {
		t.Fatalf("expected orphan rejection, got %v", err)
	}
	orphanSnapshot.Rollback()

	if _, err := db.ExecContext(ctx, "DELETE FROM `"+schema+"`.`"+invoices+"` WHERE id = 44"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO `"+schema+"`.`"+customers+"` VALUES (NULL, 'null')"); err != nil {
		t.Fatal(err)
	}
	third := begin()
	_, err = VerifyVirtualPrimaryKeys(ctx, third, DialectMySQL, tables, definitions)
	if err == nil || !strings.Contains(err.Error(), "NULL values") {
		t.Fatalf("expected NULL rejection, got %v", err)
	}
	third.Rollback()

	// A not_null declaration is a data assertion, even when the schema allows
	// NULLs. It must fail rather than silently turning a LEFT JOIN into INNER.
	if _, err := db.ExecContext(ctx, "DELETE FROM `"+schema+"`.`"+customers+"` WHERE tenant_id IS NULL"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "ALTER TABLE `"+schema+"`.`"+invoices+"` MODIFY code VARCHAR(40) NULL"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO `"+schema+"`.`"+invoices+"` VALUES (55, 1, NULL)"); err != nil {
		t.Fatal(err)
	}
	nullableChild := child
	nullableChild.Columns = []core.Column{{Name: "id", NotNull: true}, {Name: "tenant_id", NotNull: true}, {Name: "code"}}
	notNullRef := append([]core.VirtualReference(nil), relation...)
	notNullRef[0].NotNull = true
	nullSnapshot := begin()
	nullablePhysical := core.IntrospectionResult{KindsMap: map[core.ObjectKind][]core.Object{
		tableKind: {
			{ID: 0, Kind: tableKind, Name: customers, Payload: parent},
			{ID: 1, Kind: tableKind, Name: invoices, Payload: nullableChild},
		},
	}}
	_, err = VerifiedPlanningIntrospection(ctx, pinnedSubsetSession{nullSnapshot}, DialectMySQL, nullablePhysical, tableKind, definitions, notNullRef)
	if err == nil || !strings.Contains(err.Error(), "not_null asserted") {
		t.Fatalf("expected not_null assertion failure, got %v", err)
	}
	_, err = VerifiedPlanningIntrospection(ctx, pinnedSubsetSession{nullSnapshot}, DialectMySQL, nullablePhysical, tableKind, definitions, relation)
	if err != nil {
		t.Fatalf("nullable virtual reference should permit an absent link: %v", err)
	}
	nullSnapshot.Rollback()
}
