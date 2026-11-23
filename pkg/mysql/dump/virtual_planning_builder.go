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

package dump

import (
	"context"

	core "github.com/greenmaskio/greenmask/pkg/common/core"
	"github.com/greenmaskio/greenmask/pkg/common/subset"
	"github.com/greenmaskio/greenmask/pkg/mysql/kinds"
)

var _ core.VirtualPlanningBuilder = (*VirtualPlanningBuilder)(nil)

// VirtualPlanningBuilder validates user-declared keys and links against the
// MySQL dump snapshot and projects a detached view for graph construction.
type VirtualPlanningBuilder struct{}

func (*VirtualPlanningBuilder) Build(
	ctx context.Context,
	session core.DatabaseSession,
	introspection core.IntrospectionResult,
	keys []core.VirtualPrimaryKey,
	references []core.VirtualReference,
) (core.IntrospectionResult, error) {
	return subset.VerifiedPlanningIntrospection(
		ctx, session, subset.DialectMySQL, introspection,
		kinds.ObjectKindTable, keys, references,
	)
}
