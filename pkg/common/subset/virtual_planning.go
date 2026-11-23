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

package subset

import (
	"context"
	"fmt"
	"maps"
	"slices"

	core "github.com/greenmaskio/greenmask/pkg/common/core"
)

// VerifiedPlanningIntrospection constructs an effective, in-memory graph input
// from a real source introspection. Only the returned copy contains configured
// virtual relationships. The original must remain the durable schema-drift and
// dump-metadata source.
//
// The database session is borrowed for validation only. It must represent the
// same snapshot that workers will read later, for the entire dump lifetime.
func VerifiedPlanningIntrospection(
	ctx context.Context,
	session core.DatabaseSession,
	dialect Dialect,
	physical core.IntrospectionResult,
	tableKind core.ObjectKind,
	keys []core.VirtualPrimaryKey,
	refs []core.VirtualReference,
) (core.IntrospectionResult, error) {
	if len(keys) == 0 && len(refs) == 0 {
		return physical, nil
	}
	objects := physical.KindsMap[tableKind]
	tables := make([]core.Table, len(objects))
	for i, obj := range objects {
		switch payload := obj.Payload.(type) {
		case core.Table:
			tables[i] = payload
		case *core.Table:
			if payload == nil {
				return core.IntrospectionResult{}, fmt.Errorf("planning table %q: nil table payload", obj.Name)
			}
			tables[i] = *payload
		case interface{ ToCommonTable() core.Table }:
			tables[i] = payload.ToCommonTable()
		default:
			return core.IntrospectionResult{}, fmt.Errorf("planning table %q: unsupported payload type %T", obj.Name, obj.Payload)
		}
	}

	if session == nil {
		return core.IntrospectionResult{}, fmt.Errorf("verified planning requires a live database session")
	}
	var planned []core.Table
	err := session.RunWithOperationalDB(ctx, func(ctx context.Context, db core.DB) error {
		identities, err := VerifyVirtualPrimaryKeys(ctx, db, dialect, tables, keys)
		if err != nil {
			return err
		}
		planned, err = VerifyVirtualReferences(ctx, db, dialect, tables, identities, refs)
		return err
	})
	if err != nil {
		return core.IntrospectionResult{}, fmt.Errorf("verify virtual planning relationships: %w", err)
	}
	result := physical
	result.KindsMap = maps.Clone(physical.KindsMap)
	copied := slices.Clone(objects)
	for i := range copied {
		// Object IDs and kinds stay exactly as introspected. Conversion only
		// changes this graph-planning payload, not source schema metadata.
		copied[i].Payload = planned[i]
	}
	result.KindsMap[tableKind] = copied
	return result, nil
}
