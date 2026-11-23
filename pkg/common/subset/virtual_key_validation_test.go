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
	"errors"
	"strings"
	"testing"

	core "github.com/greenmaskio/greenmask/pkg/common/core"
)

func TestVirtualKeyValidationQueries(t *testing.T) {
	def := core.VirtualPrimaryKey{
		Schema: "tenant.x", Name: "odd`customers",
		Columns: []string{"tenant`id", "code"},
	}
	nulls, duplicates := virtualKeyValidationQueries(DialectMySQL, def)
	if want := "SELECT 1 FROM `tenant.x`.`odd``customers` WHERE `tenant``id` IS NULL OR `code` IS NULL LIMIT 1"; nulls != want {
		t.Fatalf("null query:\nwant %s\ngot  %s", want, nulls)
	}
	if want := "SELECT 1 FROM `tenant.x`.`odd``customers` GROUP BY `tenant``id`, `code` HAVING COUNT(*) > 1 LIMIT 1"; duplicates != want {
		t.Fatalf("duplicate query:\nwant %s\ngot  %s", want, duplicates)
	}

	def = core.VirtualPrimaryKey{
		Schema: "public", Name: `odd"customers`,
		Columns: []string{"tenant", `c"ode`},
	}
	nulls, duplicates = virtualKeyValidationQueries(DialectPostgres, def)
	if want := `SELECT 1 FROM "public"."odd""customers" WHERE "tenant" IS NULL OR "c""ode" IS NULL LIMIT 1`; nulls != want {
		t.Fatalf("postgres null query:\nwant %s\ngot  %s", want, nulls)
	}
	if want := `SELECT 1 FROM "public"."odd""customers" GROUP BY "tenant", "c""ode" HAVING COUNT(*) > 1 LIMIT 1`; duplicates != want {
		t.Fatalf("postgres duplicate query:\nwant %s\ngot  %s", want, duplicates)
	}
}

func TestVerifyVirtualPrimaryKeysPreconditions(t *testing.T) {
	table := core.Table{Schema: "s", Name: "legacy", Columns: []core.Column{{Name: "code"}}}
	key := core.VirtualPrimaryKey{Schema: "s", Name: "legacy", Columns: []string{"code"}}

	ids, err := VerifyVirtualPrimaryKeys(context.Background(), nil, DialectMySQL, []core.Table{table}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := ids.Lookup("s", "legacy"); found {
		t.Fatal("unexpected key")
	}

	_, err = VerifyVirtualPrimaryKeys(context.Background(), nil, DialectMySQL, []core.Table{table}, []core.VirtualPrimaryKey{key})
	if err == nil || !strings.Contains(err.Error(), "database transaction is required") {
		t.Fatalf("expected missing transaction, got %v", err)
	}

	invalid := key
	invalid.Columns = []string{"missing"}
	_, err = VerifyVirtualPrimaryKeys(context.Background(), nil, DialectMySQL, []core.Table{table}, []core.VirtualPrimaryKey{invalid})
	if err == nil || !strings.Contains(err.Error(), "unknown column") {
		t.Fatalf("expected schema error, got %v", err)
	}

	// Validation fails on cancellation before making any SQL request.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Any non-nil DB suffices: cancellation is checked before query execution.
	db := &sql.DB{}
	_, err = VerifyVirtualPrimaryKeys(ctx, db, DialectMySQL, []core.Table{table}, []core.VirtualPrimaryKey{key})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}

	_, err = VerifyVirtualPrimaryKeys(context.Background(), db, Dialect(-99), []core.Table{table}, []core.VirtualPrimaryKey{key})
	if err == nil || !strings.Contains(err.Error(), "unsupported SQL dialect") {
		t.Fatalf("expected dialect error, got %v", err)
	}
}
