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
)

// VirtualReference connects child columns to a parent's resolved row identity.
// Column order matters. This is a planning assertion, not a database FK.
type VirtualReference struct {
	FromSchema string
	FromTable  string
	ToSchema   string
	ToTable    string
	Columns    []string
	// NotNull asserts every child-side key column is non-NULL in the snapshot.
	NotNull bool
}

// PlanVirtualReferences makes a detached view for graph planning. Source
// introspection and physical constraint metadata remain untouched.
//
// IMPORTANT: this does not validate data. Before using the result to execute
// subset queries, verify key uniqueness, nullability and referential integrity
// in the same snapshot used for the dump.
func PlanVirtualReferences(tables []Table, identities RowIdentities, references []VirtualReference) ([]Table, error) {
	planned := slices.Clone(tables)
	if len(references) == 0 {
		return planned, nil
	}
	for i := range planned {
		planned[i].Columns = slices.Clone(tables[i].Columns)
		planned[i].PrimaryKey = slices.Clone(tables[i].PrimaryKey)
		planned[i].References = make([]Reference, len(tables[i].References))
		for j, ref := range tables[i].References {
			ref.Keys = slices.Clone(ref.Keys)
			ref.ReferencedKeys = slices.Clone(ref.ReferencedKeys)
			planned[i].References[j] = ref
		}
	}

	byName := make(map[rowIdentityTable]int, len(tables))
	for i, table := range tables {
		name := rowIdentityTable{schema: table.Schema, name: table.Name}
		if _, exists := byName[name]; exists {
			return nil, fmt.Errorf("virtual references: duplicate introspected table %q.%q", table.Schema, table.Name)
		}
		byName[name] = i
	}

	for i, relation := range references {
		from := rowIdentityTable{schema: relation.FromSchema, name: relation.FromTable}
		to := rowIdentityTable{schema: relation.ToSchema, name: relation.ToTable}
		childIdx, found := byName[from]
		if !found {
			return nil, fmt.Errorf("virtual reference: source table %q.%q not found", from.schema, from.name)
		}
		if _, found = byName[to]; !found {
			return nil, fmt.Errorf("virtual reference from %q.%q: target table %q.%q not found", from.schema, from.name, to.schema, to.name)
		}
		identity, found := identities.Lookup(to.schema, to.name)
		if !found {
			return nil, fmt.Errorf("virtual reference from %q.%q: target %q.%q has no resolved row identity", from.schema, from.name, to.schema, to.name)
		}
		if len(relation.Columns) == 0 || len(relation.Columns) != len(identity.Columns) {
			return nil, fmt.Errorf("virtual reference from %q.%q to %q.%q: %d source columns for %d target columns", from.schema, from.name, to.schema, to.name, len(relation.Columns), len(identity.Columns))
		}
		for _, previous := range references[:i] {
			if previous.FromSchema == relation.FromSchema && previous.FromTable == relation.FromTable &&
				previous.ToSchema == relation.ToSchema && previous.ToTable == relation.ToTable && slices.Equal(previous.Columns, relation.Columns) {
				return nil, fmt.Errorf("virtual reference from %q.%q to %q.%q: duplicate declaration", from.schema, from.name, to.schema, to.name)
			}
		}

		columns := make(map[string]Column, len(tables[childIdx].Columns))
		for _, column := range tables[childIdx].Columns {
			columns[column.Name] = column
		}
		seen := make(map[string]struct{}, len(relation.Columns))
		nullable := false
		for _, name := range relation.Columns {
			if _, exists := seen[name]; exists {
				return nil, fmt.Errorf("virtual reference from %q.%q: duplicate column %q", from.schema, from.name, name)
			}
			seen[name] = struct{}{}
			column, exists := columns[name]
			if !exists {
				return nil, fmt.Errorf("virtual reference from %q.%q: unknown column %q", from.schema, from.name, name)
			}
			nullable = nullable || !column.NotNull
		}
		if relation.NotNull {
			// Validation must prove this assertion before the plan is executed.
			nullable = false
		}
		planned[childIdx].References = append(planned[childIdx].References, Reference{
			ReferencedSchema: to.schema,
			ReferencedName:   to.name,
			Keys:             slices.Clone(relation.Columns),
			ReferencedKeys:   identity.Columns,
			IsNullable:       nullable,
		})
	}
	return planned, nil
}
