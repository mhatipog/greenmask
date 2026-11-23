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

package subsetbuilder

import (
	"testing"

	core "github.com/greenmaskio/greenmask/pkg/common/core"
	"github.com/stretchr/testify/require"
)

func TestCycleSubsetBuilderReturnsError(t *testing.T) {
	tests := []struct {
		name    string
		members []core.ObjectID
	}{
		{"self referencing table", []core.ObjectID{1}},
		{"multi-table cycle", []core.ObjectID{1, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sg := newSCCSubgraph(7)
			sg.addVertex(7)
			dg := core.DependencyGraphResult{
				CondensedGraph: core.CondensedGraph{
					Nodes: map[core.SCCID]core.SCCNode{
						7: {ID: 7, Members: tt.members, Cycles: &core.CycleGraph{}},
					},
				},
			}
			_, err := buildSubsetQuery(sg, dg, nil, DialectMySQL)
			require.ErrorContains(t, err, "cyclic SCC 7 is not supported")
		})
	}
}
