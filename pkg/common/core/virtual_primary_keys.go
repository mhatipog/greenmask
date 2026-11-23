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
	"fmt"
	"slices"
	"strings"
)

// VirtualPrimaryKey names existing columns asserted to identify rows in a table
// without a physical primary key. It does not create a database constraint.
type VirtualPrimaryKey struct {
	Schema  string
	Name    string
	Columns []string
}

// RowIdentity is the table's physical or user-declared row identifier.
// Virtual identities are unverified assertions until NULLs and duplicate tuples
// have been checked against the same snapshot used for the dump.
type RowIdentity struct {
	Columns []string
	Virtual bool
}

type rowIdentityTable struct {
	schema string
	name   string
}

// RowIdentities keeps logical identifiers separate from physical introspection.
// Lookup returns a copy, so the resolved definitions are read-only to callers.
type RowIdentities struct {
	byTable map[rowIdentityTable]RowIdentity
}

func (r RowIdentities) Lookup(schema, name string) (RowIdentity, bool) {
	identity, ok := r.byTable[rowIdentityTable{schema: schema, name: name}]
	identity.Columns = slices.Clone(identity.Columns)
	return identity, ok
}

// ResolveRowIdentities validates declared keys against the introspected schema.
// The result never modifies Table.PrimaryKey or database schema metadata.
// Nullable column definitions are allowed: the actual data must be checked for
// NULLs and duplicates within the dump snapshot before planning executable SQL.
func ResolveRowIdentities(tables []Table, definitions []VirtualPrimaryKey) (RowIdentities, error) {
	byName := make(map[rowIdentityTable]Table, len(tables))
	result := RowIdentities{byTable: make(map[rowIdentityTable]RowIdentity, len(tables))}
	for _, table := range tables {
		name := rowIdentityTable{schema: table.Schema, name: table.Name}
		if _, exists := byName[name]; exists {
			return RowIdentities{}, fmt.Errorf("duplicate introspected table %q.%q", table.Schema, table.Name)
		}
		byName[name] = table
		if len(table.PrimaryKey) > 0 {
			result.byTable[name] = RowIdentity{Columns: slices.Clone(table.PrimaryKey)}
		}
	}

	declared := make(map[rowIdentityTable]struct{}, len(definitions))
	for _, def := range definitions {
		name := rowIdentityTable{schema: def.Schema, name: def.Name}
		if strings.TrimSpace(name.schema) == "" || strings.TrimSpace(name.name) == "" {
			return RowIdentities{}, fmt.Errorf("virtual primary key: schema and table must be nonempty")
		}
		if _, exists := declared[name]; exists {
			return RowIdentities{}, fmt.Errorf("virtual primary key for %q.%q: duplicate declaration", name.schema, name.name)
		}
		declared[name] = struct{}{}
		table, found := byName[name]
		if !found {
			return RowIdentities{}, fmt.Errorf("virtual primary key for %q.%q: table not found", name.schema, name.name)
		}
		if len(table.PrimaryKey) > 0 {
			return RowIdentities{}, fmt.Errorf("virtual primary key for %q.%q: physical primary key already exists", name.schema, name.name)
		}
		if len(def.Columns) == 0 {
			return RowIdentities{}, fmt.Errorf("virtual primary key for %q.%q: columns must be nonempty", name.schema, name.name)
		}
		columns := make(map[string]struct{}, len(table.Columns))
		for _, column := range table.Columns {
			columns[column.Name] = struct{}{}
		}
		seen := make(map[string]struct{}, len(def.Columns))
		for _, column := range def.Columns {
			if strings.TrimSpace(column) == "" {
				return RowIdentities{}, fmt.Errorf("virtual primary key for %q.%q: blank column", name.schema, name.name)
			}
			if _, exists := seen[column]; exists {
				return RowIdentities{}, fmt.Errorf("virtual primary key for %q.%q: duplicate column %q", name.schema, name.name, column)
			}
			seen[column] = struct{}{}
			if _, exists := columns[column]; !exists {
				return RowIdentities{}, fmt.Errorf("virtual primary key for %q.%q: unknown column %q", name.schema, name.name, column)
			}
		}
		result.byTable[name] = RowIdentity{Columns: slices.Clone(def.Columns), Virtual: true}
	}
	return result, nil
}
