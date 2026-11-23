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

package core

import (
	"reflect"
	"strings"
	"testing"
)

func TestPlanVirtualReferences(t *testing.T) {
	existing := Reference{ReferencedSchema: "billing", ReferencedName: "account", Keys: []string{"account_id"}}
	child := Table{
		Schema: "billing", Name: "invoice",
		Columns:    []Column{{Name: "tenant_id", NotNull: true}, {Name: "customer_code", NotNull: true}, {Name: "account_id", NotNull: true}},
		References: make([]Reference, 1, 3),
	}
	child.References[0] = existing
	parent := Table{Schema: "billing", Name: "customer", Columns: []Column{{Name: "tenant_id"}, {Name: "code"}}}
	tables := []Table{parent, child}
	keys, err := ResolveRowIdentities(tables, []VirtualPrimaryKey{
		{Schema: "billing", Name: "customer", Columns: []string{"tenant_id", "code"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ref := VirtualReference{
		FromSchema: "billing", FromTable: "invoice", ToSchema: "billing", ToTable: "customer",
		Columns: []string{"tenant_id", "customer_code"},
	}
	planned, err := PlanVirtualReferences(tables, keys, []VirtualReference{ref})
	if err != nil {
		t.Fatal(err)
	}
	if len(planned[1].References) != 2 || len(tables[1].References) != 1 {
		t.Fatalf("unexpected references: %+v", planned[1].References)
	}
	got := planned[1].References[1]
	if !reflect.DeepEqual(got.Keys, []string{"tenant_id", "customer_code"}) ||
		!reflect.DeepEqual(got.ReferencedKeys, []string{"tenant_id", "code"}) || got.IsNullable {
		t.Fatalf("incorrect relationship: %+v", got)
	}
	if len(parent.PrimaryKey) != 0 || len(planned[0].PrimaryKey) != 0 {
		t.Fatal("physical key mutated")
	}

	// The cloned source must include nested slices, not just the Reference structs.
	planned[1].References[0].Keys[0] = "changed"
	planned[1].References[1].Keys[0] = "changed"
	if child.References[0].Keys[0] != "account_id" || ref.Columns[0] != "tenant_id" {
		t.Fatal("planning aliased the original")
	}

	tables[1].Columns[0].NotNull = false
	planned, err = PlanVirtualReferences(tables, keys, []VirtualReference{ref})
	if err != nil {
		t.Fatal(err)
	}
	if !planned[1].References[1].IsNullable {
		t.Fatal("nullable child column ignored")
	}
}

func TestPlanVirtualReferencesValidation(t *testing.T) {
	tables := []Table{
		{Schema: "s", Name: "parent", Columns: []Column{{Name: "a"}, {Name: "b"}}},
		{Schema: "s", Name: "child", Columns: []Column{{Name: "x"}, {Name: "y"}}},
		{Schema: "s", Name: "without_key", Columns: []Column{{Name: "id"}}},
	}
	keys, err := ResolveRowIdentities(tables, []VirtualPrimaryKey{{Schema: "s", Name: "parent", Columns: []string{"a", "b"}}})
	if err != nil {
		t.Fatal(err)
	}
	good := VirtualReference{FromSchema: "s", FromTable: "child", ToSchema: "s", ToTable: "parent", Columns: []string{"x", "y"}}
	cases := []struct {
		name string
		refs []VirtualReference
		want string
	}{
		{"missing child", []VirtualReference{{FromSchema: "s", FromTable: "missing", ToSchema: "s", ToTable: "parent", Columns: []string{"x", "y"}}}, "source table"},
		{"missing parent", []VirtualReference{{FromSchema: "s", FromTable: "child", ToSchema: "s", ToTable: "missing", Columns: []string{"x", "y"}}}, "target table"},
		{"missing key", []VirtualReference{{FromSchema: "s", FromTable: "child", ToSchema: "s", ToTable: "without_key", Columns: []string{"x"}}}, "no resolved row identity"},
		{"wrong arity", []VirtualReference{{FromSchema: "s", FromTable: "child", ToSchema: "s", ToTable: "parent", Columns: []string{"x"}}}, "source columns for"},
		{"unknown column", []VirtualReference{{FromSchema: "s", FromTable: "child", ToSchema: "s", ToTable: "parent", Columns: []string{"x", "missing"}}}, "unknown column"},
		{"duplicate column", []VirtualReference{{FromSchema: "s", FromTable: "child", ToSchema: "s", ToTable: "parent", Columns: []string{"x", "x"}}}, "duplicate column"},
		{"duplicate relation", []VirtualReference{good, good}, "duplicate declaration"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := PlanVirtualReferences(tables, keys, tt.refs)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("wanted %q, got %v", tt.want, err)
			}
		})
	}
}
