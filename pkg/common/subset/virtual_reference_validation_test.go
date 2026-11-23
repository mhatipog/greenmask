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
	"strings"
	"testing"

	core "github.com/greenmaskio/greenmask/pkg/common/core"
)

func TestVirtualReferenceValidationQuery(t *testing.T) {
	ref := core.VirtualReference{
		FromSchema: "legacy", FromTable: "invoice",
		ToSchema: "legacy", ToTable: "customer",
		Columns: []string{"tenant_id", "customer_code"},
	}
	identity := core.RowIdentity{Columns: []string{"tenant_id", "code"}, Virtual: true}
	query := virtualReferenceValidationQuery(DialectMySQL, ref, identity)

	for _, part := range []string{
		"`child`.`tenant_id` = `parent`.`tenant_id`",
		"`child`.`customer_code` = `parent`.`code`",
		"`child`.`tenant_id` IS NOT NULL",
		"`child`.`customer_code` IS NOT NULL",
		"`parent`.`tenant_id` IS NULL",
	} {
		if !strings.Contains(query, part) {
			t.Fatalf("missing predicate %q in %s", part, query)
		}
	}
	// The same query builder must quote unusual PostgreSQL identifiers.
	ref.FromTable = `odd"child`
	ref.Columns = []string{`tenant"id`, "customer_code"}
	query = virtualReferenceValidationQuery(DialectPostgres, ref, identity)
	for _, part := range []string{`"legacy"."odd""child"`, `"child"."tenant""id" = "parent"."tenant_id"`} {
		if !strings.Contains(query, part) {
			t.Fatalf("bad quoted identifier in %s", query)
		}
	}
}

func TestVerifyVirtualReferencesRejectsInvalidPlan(t *testing.T) {
	child := core.Table{Schema: "s", Name: "child", Columns: []core.Column{{Name: "key"}}}
	parent := core.Table{Schema: "s", Name: "parent", Columns: []core.Column{{Name: "id"}}}
	ids, err := core.ResolveRowIdentities([]core.Table{parent, child}, []core.VirtualPrimaryKey{{
		Schema: "s", Name: "parent", Columns: []string{"id"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	reference := core.VirtualReference{
		FromSchema: "s", FromTable: "child", ToSchema: "s", ToTable: "parent", Columns: []string{"missing"},
	}
	_, err = VerifyVirtualReferences(context.Background(), nil, DialectMySQL, []core.Table{parent, child}, ids, []core.VirtualReference{reference})
	if err == nil || !strings.Contains(err.Error(), "unknown column") {
		t.Fatalf("expected schema rejection before SQL, got %v", err)
	}
	reference.Columns = []string{"key"}
	_, err = VerifyVirtualReferences(context.Background(), nil, DialectMySQL, []core.Table{parent, child}, ids, []core.VirtualReference{reference})
	if err == nil || !strings.Contains(err.Error(), "database transaction is required") {
		t.Fatalf("expected missing snapshot, got %v", err)
	}
}

func TestVirtualReferenceNotNullQuery(t *testing.T) {
	ref := core.VirtualReference{
		FromSchema: "s", FromTable: "child",
		ToSchema: "s", ToTable: "parent",
		Columns: []string{"tenant_id", "code"},
		NotNull: true,
	}
	got := virtualReferenceNullQuery(DialectMySQL, ref)
	want := "SELECT 1 FROM `s`.`child` WHERE `tenant_id` IS NULL OR `code` IS NULL LIMIT 1"
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}
