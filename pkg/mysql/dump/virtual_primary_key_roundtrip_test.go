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

package dump

import (
	"context"
	"database/sql"
	"path/filepath"

	core "github.com/greenmaskio/greenmask/pkg/common/core"
	"github.com/greenmaskio/greenmask/pkg/common/utils"
	"github.com/greenmaskio/greenmask/pkg/config"
	mysqlrestore "github.com/greenmaskio/greenmask/pkg/mysql/restore"
)

// TestVirtualPrimaryKeyRoundTrip exercises the *actual* v1 dump and restore
// pipelines, including schema scripts, raw data files, persisted metadata,
// database remapping and exact row content. Only the temporary MySQL test
// container is modified.
func (s *pipelineEngineSuite) TestVirtualPrimaryKeyRoundTrip() {
	ctx := context.Background()
	const source = "testdb"
	const target = "gm_virtual_restore319"
	const customers = "gm_virtual_customers319"
	const invoices = "gm_virtual_invoices319"

	s.MigrateUp(ctx, []string{
		`CREATE TABLE testdb.gm_virtual_customers319 (
            tenant_id INT NOT NULL,
            code VARCHAR(40) NOT NULL,
            label VARCHAR(80) NULL
        ) ENGINE=InnoDB;`,
		`CREATE TABLE testdb.gm_virtual_invoices319 (
            id INT PRIMARY KEY,
            tenant_id INT NOT NULL,
            customer_code VARCHAR(40) NOT NULL,
            amount INT NOT NULL,
            memo VARCHAR(80) NULL
        ) ENGINE=InnoDB;`,
		`INSERT INTO testdb.gm_virtual_customers319 (tenant_id, code, label) VALUES
            (1, 'shared', 'Alice O''Brien'),
            (2, 'shared', 'different tenant'),
            (1, 'other', 'Jalapeño');`,
		`INSERT INTO testdb.gm_virtual_invoices319
            (id, tenant_id, customer_code, amount, memo) VALUES
            (11, 1, 'shared', 101, 'first'),
            (22, 2, 'shared', 999, 'never restore'),
            (33, 1, 'other', 202, 'café');`,
	})
	defer s.MigrateDown(ctx, []string{
		`DROP TABLE testdb.gm_virtual_invoices319;`,
		`DROP TABLE testdb.gm_virtual_customers319;`,
	})

	db, err := s.GetConnectionWithUser(ctx, engMysqlRootUser, engMysqlRootPass)
	s.Require().NoError(err)
	defer db.Close()
	defer func() {
		_, _ = db.ExecContext(context.Background(), "DROP DATABASE IF EXISTS `"+target+"`")
	}()

	storageDir := s.T().TempDir()
	cfg := s.baseConfig(ctx, storageDir)
	cfg.Dump.Options.Compression = core.CompressionNone
	cfg.Dump.VirtualPrimaryKeys = []config.VirtualPrimaryKey{{
		Schema: source, Name: customers, Columns: []string{"tenant_id", "code"},
	}}
	cfg.Dump.VirtualReferences = []config.VirtualReference{{
		Schema: source, Name: invoices,
		References: []config.Reference{{
			Schema: source, Name: customers, NotNull: true,
			Columns: []config.ReferencedColumn{
				{Name: "tenant_id"}, {Name: "customer_code"},
			},
		}},
	}}
	cfg.Dump.Transformation = config.TransformationConfig{{
		Schema: source, Name: customers,
		SubsetConds: []string{"`testdb`.`gm_virtual_customers319`.`tenant_id` = 1"},
	}}

	dumpState, err := NewDumpPipeline().RunDump(ctx, cfg)
	s.Require().NoError(err, "real MySQL dump pipeline must complete")
	s.Require().NotNil(dumpState)

	files := s.storageFiles(storageDir)
	s.Require().Contains(files, "schema_pre_"+source+".sql")
	s.Require().Contains(files, "schema_post_"+source+".sql")
	s.Require().Contains(files, source+"__"+customers+".sql")
	s.Require().Contains(files, source+"__"+invoices+".sql")
	s.Require().Contains(files, "metadata.json")

	// Source data stays intact. The generated dump must not contain tenant 2.
	customerDump := s.readFile(files[source+"__"+customers+".sql"])
	invoiceDump := s.readFile(files[source+"__"+invoices+".sql"])
	s.NotContains(customerDump, "different tenant")
	s.NotContains(invoiceDump, "never restore")

	cfg.Restore = config.NewRestore()
	cfg.Restore.Options.Jobs = 1
	cfg.Restore.Options.CreateDatabase = true
	cfg.Restore.Options.RemapDatabase = map[string]string{source: target}
	cfg.Restore.Options.DatabaseReplaceMode = core.DatabaseReplaceModeStrict
	cfg.Restore.MysqlConfig.ConnectionOpts = s.GetRootConnectionOpts(ctx)
	// The original database is still live, so the restore must remap the
	// exported schema and data to a fresh target instead of overwriting it.
	restorePipeline, err := mysqlrestore.NewRestorePipeline(utils.NewDefaultCmdProducer())
	s.Require().NoError(err)
	_, err = restorePipeline.RunRestore(ctx, cfg, dumpState.DumpID)
	s.Require().NoError(err, "real MySQL restore pipeline must complete")

	gotCustomers := virtualCustomerRows(ctx, s.T(), db, target, customers)
	s.Equal([]virtualCustomerRow{
		{TenantID: 1, Code: "other", Label: "Jalapeño"},
		{TenantID: 1, Code: "shared", Label: "Alice O'Brien"},
	}, gotCustomers)

	gotInvoices := virtualInvoiceRows(ctx, s.T(), db, target, invoices)
	s.Equal([]virtualInvoiceRow{
		{ID: 11, TenantID: 1, Code: "shared", Amount: 101, Memo: "first"},
		{ID: 33, TenantID: 1, Code: "other", Amount: 202, Memo: "café"},
	}, gotInvoices)

	// Original schema is still available and includes the excluded tenant.
	s.Len(virtualCustomerRows(ctx, s.T(), db, source, customers), 3)
	s.Len(virtualInvoiceRows(ctx, s.T(), db, source, invoices), 3)
	s.FileExists(filepath.Clean(files["metadata.json"]))
}

type virtualCustomerRow struct {
	TenantID int
	Code     string
	Label    string
}

type virtualInvoiceRow struct {
	ID       int
	TenantID int
	Code     string
	Amount   int
	Memo     string
}

func virtualCustomerRows(ctx context.Context, t interface {
	Helper()
	Fatalf(format string, args ...any)
}, db *sql.DB, schema, name string) []virtualCustomerRow {
	t.Helper()
	rows, err := db.QueryContext(ctx, "SELECT tenant_id, code, label FROM `"+schema+"`.`"+name+"` ORDER BY code")
	if err != nil {
		t.Fatalf("read %s.%s: %v", schema, name, err)
	}
	defer rows.Close()
	var result []virtualCustomerRow
	for rows.Next() {
		var r virtualCustomerRow
		if err := rows.Scan(&r.TenantID, &r.Code, &r.Label); err != nil {
			t.Fatalf("scan customers: %v", err)
		}
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read customers: %v", err)
	}
	return result
}

func virtualInvoiceRows(ctx context.Context, t interface {
	Helper()
	Fatalf(format string, args ...any)
}, db *sql.DB, schema, name string) []virtualInvoiceRow {
	t.Helper()
	rows, err := db.QueryContext(ctx, "SELECT id, tenant_id, customer_code, amount, memo FROM `"+schema+"`.`"+name+"` ORDER BY id")
	if err != nil {
		t.Fatalf("read %s.%s: %v", schema, name, err)
	}
	defer rows.Close()
	var result []virtualInvoiceRow
	for rows.Next() {
		var r virtualInvoiceRow
		if err := rows.Scan(&r.ID, &r.TenantID, &r.Code, &r.Amount, &r.Memo); err != nil {
			t.Fatalf("scan invoices: %v", err)
		}
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read invoices: %v", err)
	}
	return result
}
