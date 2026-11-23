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

package tablegraph

import (
	"strings"
	"testing"

	core "github.com/greenmaskio/greenmask/pkg/common/core"
)

func TestGraphExplicitReferencedColumns(t *testing.T) {
	// The physical PK is id, but the FK points to a composite UNIQUE key.
	parent := core.Table{
		Schema: "s", Name: "customer", PrimaryKey: []string{"id"},
		Columns: []core.Column{{Name: "id"}, {Name: "tenant"}, {Name: "code"}},
	}
	child := core.Table{
		Schema: "s", Name: "invoice",
		References: []core.Reference{{
			ReferencedSchema: "s", ReferencedName: "customer",
			Keys:           []string{"tenant_id", "customer_code"},
			ReferencedKeys: []string{"tenant", "code"},
		}},
	}
	graph, err := NewGraph([]core.Table{parent, child})
	if err != nil {
		t.Fatal(err)
	}
	edge := graph.Graph[1][0]
	if got := edge.From().Keys(); len(got) != 2 || got[0].Name != "tenant_id" || got[1].Name != "customer_code" {
		t.Fatalf("incorrect child columns: %+v", got)
	}
	if got := edge.To().Keys(); len(got) != 2 || got[0].Name != "tenant" || got[1].Name != "code" {
		t.Fatalf("incorrect parent columns: %+v", got)
	}
	if got := graph.TransposedGraph[0][0].From().Keys(); got[0].Name != "tenant" || got[1].Name != "code" {
		t.Fatalf("transposed graph did not preserve mapping: %+v", got)
	}
	// Legacy reference metadata still falls back to the actual physical PK.
	child.References[0].ReferencedKeys = nil
	child.References[0].Keys = []string{"customer_id"}
	graph, err = NewGraph([]core.Table{parent, child})
	if err != nil {
		t.Fatal(err)
	}
	if got := graph.Graph[1][0].To().Keys(); len(got) != 1 || got[0].Name != "id" {
		t.Fatalf("physical key fallback changed: %+v", got)
	}
}

func TestGraphRejectsInvalidJoinArity(t *testing.T) {
	parent := core.Table{Schema: "s", Name: "parent", PrimaryKey: []string{"id"}, Columns: []core.Column{{Name: "id"}}}
	child := core.Table{Schema: "s", Name: "child", References: []core.Reference{{
		ReferencedSchema: "s", ReferencedName: "parent", Keys: []string{"a", "b"},
	}}}
	_, err := NewGraph([]core.Table{parent, child})
	if err == nil || !strings.Contains(err.Error(), "invalid key mapping") {
		t.Fatalf("expected arity error, got %v", err)
	}

	child.References[0].Keys = []string{"a"}
	child.References[0].ReferencedKeys = []string{"missing"}
	_, err = NewGraph([]core.Table{parent, child})
	if err == nil || !strings.Contains(err.Error(), "unknown target column") {
		t.Fatalf("expected target error, got %v", err)
	}

	parent.PrimaryKey = nil
	child.References[0].ReferencedKeys = nil
	_, err = NewGraph([]core.Table{parent, child})
	if err == nil || !strings.Contains(err.Error(), "invalid key mapping") {
		t.Fatalf("expected missing-key error, got %v", err)
	}
}
