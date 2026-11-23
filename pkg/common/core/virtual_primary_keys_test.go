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

func TestResolveRowIdentities(t *testing.T) {
	tables := []Table{
		{Schema: "a", Name: "legacy", Columns: []Column{{Name: "tenant"}, {Name: "code"}}},
		{Schema: "b", Name: "legacy", Columns: []Column{{Name: "code", NotNull: true}}},
		{Schema: "a", Name: "modern", PrimaryKey: []string{"id"}, Columns: []Column{{Name: "id", NotNull: true}}},
	}
	definitions := []VirtualPrimaryKey{{Schema: "a", Name: "legacy", Columns: []string{"tenant", "code"}}}
	ids, err := ResolveRowIdentities(tables, definitions)
	if err != nil {
		t.Fatal(err)
	}
	logical, ok := ids.Lookup("a", "legacy")
	if !ok || !logical.Virtual || !reflect.DeepEqual(logical.Columns, []string{"tenant", "code"}) {
		t.Fatalf("incorrect virtual identity: %+v", logical)
	}
	if _, ok := ids.Lookup("b", "legacy"); ok {
		t.Fatal("table matched across schemas")
	}
	physical, ok := ids.Lookup("a", "modern")
	if !ok || physical.Virtual || !reflect.DeepEqual(physical.Columns, []string{"id"}) {
		t.Fatalf("incorrect physical key: %+v", physical)
	}
	if len(tables[0].PrimaryKey) != 0 {
		t.Fatal("modified physical introspection")
	}

	logical.Columns[0] = "modified"
	physical.Columns[0] = "modified"
	unchanged, _ := ids.Lookup("a", "legacy")
	original, _ := ids.Lookup("a", "modern")
	if unchanged.Columns[0] != "tenant" || original.Columns[0] != "id" ||
		definitions[0].Columns[0] != "tenant" || tables[2].PrimaryKey[0] != "id" {
		t.Fatal("resolved identity aliases its caller")
	}

	ids, err = ResolveRowIdentities(tables, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ids.Lookup("a", "legacy"); ok {
		t.Fatal("unconfigured identity appeared")
	}
	if k, ok := ids.Lookup("a", "modern"); !ok || k.Virtual {
		t.Fatal("physical key disappeared")
	}
}

func TestResolveRowIdentitiesValidation(t *testing.T) {
	tables := []Table{
		{Schema: "a", Name: "legacy", Columns: []Column{{Name: "tenant"}, {Name: "code"}}},
		{Schema: "a", Name: "modern", PrimaryKey: []string{"id"}, Columns: []Column{{Name: "id"}}},
	}
	key := VirtualPrimaryKey{Schema: "a", Name: "legacy", Columns: []string{"code"}}
	tests := []struct {
		name        string
		tables      []Table
		definitions []VirtualPrimaryKey
		want        string
	}{
		{"missing schema", tables, []VirtualPrimaryKey{{Name: "legacy", Columns: []string{"code"}}}, "schema and table"},
		{"unknown table", tables, []VirtualPrimaryKey{{Schema: "a", Name: "missing", Columns: []string{"code"}}}, "table not found"},
		{"physical conflict", tables, []VirtualPrimaryKey{{Schema: "a", Name: "modern", Columns: []string{"id"}}}, "physical primary key already exists"},
		{"empty columns", tables, []VirtualPrimaryKey{{Schema: "a", Name: "legacy"}}, "columns must be nonempty"},
		{"blank column", tables, []VirtualPrimaryKey{{Schema: "a", Name: "legacy", Columns: []string{" "}}}, "blank column"},
		{"duplicate column", tables, []VirtualPrimaryKey{{Schema: "a", Name: "legacy", Columns: []string{"code", "code"}}}, "duplicate column"},
		{"missing column", tables, []VirtualPrimaryKey{{Schema: "a", Name: "legacy", Columns: []string{"absent"}}}, "unknown column"},
		{"duplicate declaration", tables, []VirtualPrimaryKey{key, key}, "duplicate declaration"},
		{"duplicate introspection", append(append([]Table{}, tables...), tables[0]), nil, "duplicate introspected table"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResolveRowIdentities(tt.tables, tt.definitions)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("wanted %q, got %v", tt.want, err)
			}
		})
	}
}

func TestResolveRowIdentitiesExactNames(t *testing.T) {
	table := Table{Schema: "a.b", Name: `odd"table`, Columns: []Column{{Name: `x.y"z`}}}
	ids, err := ResolveRowIdentities([]Table{table}, []VirtualPrimaryKey{
		{Schema: table.Schema, Name: table.Name, Columns: []string{`x.y"z`}},
	})
	if err != nil {
		t.Fatal(err)
	}
	key, ok := ids.Lookup("a.b", `odd"table`)
	if !ok || !reflect.DeepEqual(key.Columns, []string{`x.y"z`}) {
		t.Fatalf("incorrect identifiers: %+v", key)
	}
}
