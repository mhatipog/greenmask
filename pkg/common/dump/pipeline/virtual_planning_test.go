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

package pipeline

import (
	"context"
	"errors"
	"testing"

	core "github.com/greenmaskio/greenmask/pkg/common/core"
	"github.com/greenmaskio/greenmask/pkg/config"
	"github.com/stretchr/testify/require"
)

type virtualStageSpy struct {
	called  int
	gotKeys []core.VirtualPrimaryKey
	gotRefs []core.VirtualReference
	input   core.IntrospectionResult
	result  core.IntrospectionResult
	err     error
}

func (s *virtualStageSpy) Build(_ context.Context, _ core.DatabaseSession, physical core.IntrospectionResult, keys []core.VirtualPrimaryKey, refs []core.VirtualReference) (core.IntrospectionResult, error) {
	s.called++
	s.input = physical
	s.gotKeys = keys
	s.gotRefs = refs
	return s.result, s.err
}

func TestDiscoverVirtualPlanningOverlay(t *testing.T) {
	stubs := newStubSet()
	original := core.IntrospectionResult{KindsMap: map[core.ObjectKind][]core.Object{
		"mysql.table": {{ID: 3, Kind: "mysql.table", Name: "legacy", Payload: core.Table{ID: 3, Schema: "s", Name: "legacy"}}},
	}}
	stubs.introspector.result = original
	spy := &virtualStageSpy{result: core.IntrospectionResult{KindsMap: map[core.ObjectKind][]core.Object{
		"mysql.table": {{ID: 3, Kind: "mysql.table", Name: "legacy", Payload: core.Table{ID: 3, Schema: "s", Name: "legacy", References: []core.Reference{{ReferencedSchema: "s", ReferencedName: "parent"}}}}},
	}}}
	p := stubs.pipeline()
	p.Stages.VirtualPlanningBuilder = spy
	cfg := config.Config{}
	cfg.Dump.VirtualPrimaryKeys = []config.VirtualPrimaryKey{{Schema: "s", Name: "parent", Columns: []string{"id"}}}
	cfg.Dump.VirtualReferences = []config.VirtualReference{{Schema: "s", Name: "legacy", References: []config.Reference{{
		Schema: "s", Name: "parent", Columns: []config.ReferencedColumn{{Name: "parent_id"}},
	}}}}
	state := p.NewRun(cfg)
	require.NoError(t, p.Discover(context.Background(), runtimeOf(stubs), state))
	require.Equal(t, 1, spy.called)
	require.Equal(t, "id", spy.gotKeys[0].Columns[0])
	require.Equal(t, "parent_id", spy.gotRefs[0].Columns[0])
	require.Empty(t, state.Discovery.Introspection.KindsMap["mysql.table"][0].Payload.(core.Table).References)
	require.NotEmpty(t, spy.result.KindsMap["mysql.table"][0].Payload.(core.Table).References)
	require.NotEmpty(t, stubs.graph.input.KindsMap["mysql.table"][0].Payload.(core.Table).References)
	require.True(t, state.ExecutedStages[StageNameDiscovery])
}

func TestDiscoverVirtualPlanningFailsClosed(t *testing.T) {
	keys := []config.VirtualPrimaryKey{{Schema: "s", Name: "missing", Columns: []string{"id"}}}
	t.Run("missing graph planning support", func(t *testing.T) {
		stubs := newStubSet()
		cfg := config.Config{}
		cfg.Dump.VirtualPrimaryKeys = keys
		state := stubs.pipeline().NewRun(cfg)
		err := stubs.pipeline().Discover(context.Background(), runtimeOf(stubs), state)
		require.ErrorContains(t, err, "not supported")
		require.Zero(t, stubs.graph.calls)
	})
	t.Run("verification errors prevent graph construction", func(t *testing.T) {
		stubs := newStubSet()
		spy := &virtualStageSpy{err: errors.New("duplicate key values")}
		p := stubs.pipeline()
		p.Stages.VirtualPlanningBuilder = spy
		cfg := config.Config{}
		cfg.Dump.VirtualPrimaryKeys = keys
		state := p.NewRun(cfg)
		err := p.Discover(context.Background(), runtimeOf(stubs), state)
		require.ErrorContains(t, err, "duplicate key values")
		require.Zero(t, stubs.graph.calls)
		require.False(t, state.ExecutedStages[StageNameDiscovery])
	})
	t.Run("unsupported expression is rejected before planning", func(t *testing.T) {
		stubs := newStubSet()
		spy := &virtualStageSpy{}
		p := stubs.pipeline()
		p.Stages.VirtualPlanningBuilder = spy
		cfg := config.Config{}
		cfg.Dump.VirtualReferences = []config.VirtualReference{{Schema: "s", Name: "child", References: []config.Reference{{
			Schema: "s", Name: "parent", Columns: []config.ReferencedColumn{{Expression: "json_value()"}},
		}}}}
		state := p.NewRun(cfg)
		err := p.Discover(context.Background(), runtimeOf(stubs), state)
		require.ErrorContains(t, err, "expression columns not supported")
		require.Zero(t, spy.called)
		require.Zero(t, stubs.graph.calls)
	})
}
