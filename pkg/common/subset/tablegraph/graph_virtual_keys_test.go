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
	"testing"

	core "github.com/greenmaskio/greenmask/pkg/common/core"
)

func TestGraphWithVirtualPrimaryKey(t *testing.T) {
	parent := core.Table{
		Schema: "tenant", Name: "customer",
		Columns: []core.Column{{Name: "tenant_id"}, {Name: "code"}},
	}
	child := core.Table{
		Schema: "tenant", Name: "invoice",
		Columns: []core.Column{{Name: "tenant_id", NotNull: true}, {Name: "customer_code", NotNull: true}},
	}
	tables := []core.Table{parent, child}
	identities, err := core.ResolveRowIdentities(tables, []core.VirtualPrimaryKey{{
		Schema: "tenant", Name: "customer", Columns: []string{"tenant_id", "code"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	planned, err := core.PlanVirtualReferences(tables, identities, []core.VirtualReference{{
		FromSchema: "tenant", FromTable: "invoice", ToSchema: "tenant", ToTable: "customer",
		Columns: []string{"tenant_id", "customer_code"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	graph, err := NewGraph(planned)
	if err != nil {
		t.Fatal(err)
	}
	if got := graph.Graph[1][0].To().Keys(); len(got) != 2 || got[0].Name != "tenant_id" || got[1].Name != "code" {
		t.Fatalf("unexpected logical target columns: %+v", got)
	}
	if len(tables[0].PrimaryKey) != 0 || len(tables[1].References) != 0 {
		t.Fatal("graph planning modified source schema")
	}
}
