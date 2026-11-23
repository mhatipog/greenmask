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
	"strings"

	core "github.com/greenmaskio/greenmask/pkg/common/core"
)

// VerifyVirtualReferences checks that every non-NULL child key points to a
// parent in the same snapshot. A child tuple containing a NULL is treated as
// having no reference (SQL MATCH SIMPLE semantics). The referenced key must
// already have passed VerifyVirtualPrimaryKeys.
func VerifyVirtualReferences(
	ctx context.Context,
	db core.DB,
	dialect Dialect,
	tables []core.Table,
	identities core.RowIdentities,
	references []core.VirtualReference,
) ([]core.Table, error) {
	planned, err := core.PlanVirtualReferences(tables, identities, references)
	if err != nil {
		return nil, err
	}
	if len(references) == 0 {
		return planned, nil
	}
	if db == nil {
		return nil, fmt.Errorf("verify virtual references: database transaction is required")
	}
	if dialect != DialectMySQL && dialect != DialectPostgres {
		return nil, fmt.Errorf("verify virtual references: unsupported SQL dialect %d", dialect)
	}
	for _, ref := range references {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		identity, ok := identities.Lookup(ref.ToSchema, ref.ToTable)
		if !ok {
			// PlanVirtualReferences should have already caught this.
			return nil, fmt.Errorf("virtual reference %q.%q: target identity not resolved", ref.ToSchema, ref.ToTable)
		}
		if ref.NotNull {
			nulls, err := existsRow(ctx, db, virtualReferenceNullQuery(dialect, ref))
			if err != nil {
				return nil, fmt.Errorf("verify virtual reference %q.%q -> %q.%q for NULL values: %w",
					ref.FromSchema, ref.FromTable, ref.ToSchema, ref.ToTable, err)
			}
			if nulls {
				return nil, fmt.Errorf("virtual reference %q.%q -> %q.%q: not_null asserted but child key contains NULL",
					ref.FromSchema, ref.FromTable, ref.ToSchema, ref.ToTable)
			}
		}
		query := virtualReferenceValidationQuery(dialect, ref, identity)
		orphan, err := existsRow(ctx, db, query)
		if err != nil {
			return nil, fmt.Errorf("verify virtual reference %q.%q -> %q.%q: %w",
				ref.FromSchema, ref.FromTable, ref.ToSchema, ref.ToTable, err)
		}
		if orphan {
			return nil, fmt.Errorf("virtual reference %q.%q -> %q.%q: child row has no matching parent",
				ref.FromSchema, ref.FromTable, ref.ToSchema, ref.ToTable)
		}
	}
	return planned, nil
}

// virtualReferenceNullQuery checks the not_null assertion. The table and
// column identifiers come from verified introspection, never value strings.
func virtualReferenceNullQuery(dialect Dialect, ref core.VirtualReference) string {
	table := quoteIdentifier(dialect, ref.FromSchema) + "." + quoteIdentifier(dialect, ref.FromTable)
	conds := make([]string, len(ref.Columns))
	for i, col := range ref.Columns {
		conds[i] = quoteIdentifier(dialect, col) + " IS NULL"
	}
	return "SELECT 1 FROM " + table + " WHERE " + strings.Join(conds, " OR ") + " LIMIT 1"
}

func virtualReferenceValidationQuery(dialect Dialect, ref core.VirtualReference, key core.RowIdentity) string {
	from := quoteIdentifier(dialect, ref.FromSchema) + "." + quoteIdentifier(dialect, ref.FromTable)
	to := quoteIdentifier(dialect, ref.ToSchema) + "." + quoteIdentifier(dialect, ref.ToTable)
	child := quoteIdentifier(dialect, "child")
	parent := quoteIdentifier(dialect, "parent")

	joins := make([]string, 0, len(ref.Columns))
	populated := make([]string, 0, len(ref.Columns)+1)
	for i, col := range ref.Columns {
		lhs := child + "." + quoteIdentifier(dialect, col)
		rhs := parent + "." + quoteIdentifier(dialect, key.Columns[i])
		joins = append(joins, lhs+" = "+rhs)
		populated = append(populated, lhs+" IS NOT NULL")
	}
	populated = append(populated, parent+"."+quoteIdentifier(dialect, key.Columns[0])+" IS NULL")
	return "SELECT 1 FROM " + from + " AS " + child +
		" LEFT JOIN " + to + " AS " + parent + " ON " + strings.Join(joins, " AND ") +
		" WHERE " + strings.Join(populated, " AND ") + " LIMIT 1"
}
