// Copyright 2025 Greenmask
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

package tablegraph

import (
	"errors"
	"fmt"

	core "github.com/greenmaskio/greenmask/pkg/common/core"
)

var errReferenceTableNotFound = errors.New("reference table not found")

type Graph struct {
	// Vertexes - the Vertexes that are part of the Graph. Basically they are the tables in the DB.
	//
	// The index of the table in the slice is the index of the table in the Graph.
	// This index used in TableLink to reference the table as well.
	Vertexes []core.Table
	// Graph - the oriented Graph representation of the DB Vertexes.
	Graph [][]Edge
	// TransposedGraph - the transposed Graph representation of the DB Vertexes.
	TransposedGraph [][]Edge
}

// NewGraph - creates a new Graph instance.
//
// It builds a Graph based on the introspected Vertexes and their references.
func NewGraph(tables []core.Table) (Graph, error) {
	var edgeIdSequence int
	graph := make([][]Edge, len(tables))
	transposedGraph := make([][]Edge, len(tables))
	type tableName struct{ schema, name string }
	byName := make(map[tableName]int, len(tables))
	for i, table := range tables {
		key := tableName{table.Schema, table.Name}
		if _, exists := byName[key]; exists {
			return Graph{}, fmt.Errorf("duplicate graph table %q.%q", table.Schema, table.Name)
		}
		byName[key] = i
	}
	for tableIdx, table := range tables {
		for _, reference := range table.References {
			referenceTableIdx, found := byName[tableName{reference.ReferencedSchema, reference.ReferencedName}]
			if !found {
				return Graph{}, fmt.Errorf("reference from %q.%q to %q.%q: %w",
					table.Schema, table.Name, reference.ReferencedSchema, reference.ReferencedName, errReferenceTableNotFound)
			}

			parentKeys := reference.ReferencedKeys
			if len(parentKeys) == 0 {
				// Older introspection only records the child side. Preserve its
				// primary-key fallback until each engine supplies actual FK targets.
				parentKeys = tables[referenceTableIdx].PrimaryKey
			}
			if len(reference.Keys) == 0 || len(parentKeys) == 0 || len(reference.Keys) != len(parentKeys) {
				return Graph{}, fmt.Errorf("reference from %q.%q to %q.%q: invalid key mapping (%d child, %d parent columns)",
					table.Schema, table.Name, reference.ReferencedSchema, reference.ReferencedName, len(reference.Keys), len(parentKeys))
			}
			if len(tables[referenceTableIdx].Columns) > 0 {
				for _, name := range parentKeys {
					found := false
					for _, col := range tables[referenceTableIdx].Columns {
						if col.Name == name {
							found = true
							break
						}
					}
					if !found {
						return Graph{}, fmt.Errorf("reference from %q.%q to %q.%q: unknown target column %q",
							table.Schema, table.Name, reference.ReferencedSchema, reference.ReferencedName, name)
					}
				}
			}

			from := NewTableLink(
				tableIdx,
				table,
				NewKeysByColumn(reference.Keys),
				nil,
			)
			to := NewTableLink(
				referenceTableIdx,
				tables[referenceTableIdx],
				NewKeysByColumn(parentKeys),
				nil,
			)

			edge := NewEdge(
				edgeIdSequence,
				reference.IsNullable,
				from,
				to,
			)
			graph[tableIdx] = append(
				graph[tableIdx],
				edge,
			)

			// Transpose the edge

			transposedFrom := NewTableLink(
				referenceTableIdx,
				tables[referenceTableIdx],
				NewKeysByColumn(parentKeys),
				nil,
			)

			transposedTo := NewTableLink(
				tableIdx,
				table,
				NewKeysByColumn(reference.Keys),
				nil,
			)

			transposedEdge := NewEdge(
				edgeIdSequence,
				reference.IsNullable,
				transposedFrom,
				transposedTo,
			)

			transposedGraph[referenceTableIdx] = append(
				transposedGraph[referenceTableIdx],
				transposedEdge,
			)
			edgeIdSequence++
		}
	}
	return Graph{
		Vertexes:        tables,
		Graph:           graph,
		TransposedGraph: transposedGraph,
	}, nil
}
