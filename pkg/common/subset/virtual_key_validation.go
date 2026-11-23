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
	"database/sql"
	"errors"
	"fmt"
	"strings"

	core "github.com/greenmaskio/greenmask/pkg/common/core"
)

// VerifyVirtualPrimaryKeys resolves the declared identities and checks their
// uniqueness and non-NULL values before they can be used to plan a subset.
//
// db MUST use the same consistent read snapshot as the data dump. In
// particular, callers must not pass an autocommit *sql.DB or a transaction
// whose isolation differs from the worker snapshots. This function does not
// begin or commit a transaction, and it does not verify virtual FK integrity.
// Each validation may require a full scan of the named table.
func VerifyVirtualPrimaryKeys(
	ctx context.Context,
	db core.DB,
	dialect Dialect,
	tables []core.Table,
	definitions []core.VirtualPrimaryKey,
) (core.RowIdentities, error) {
	identities, err := core.ResolveRowIdentities(tables, definitions)
	if err != nil {
		return core.RowIdentities{}, fmt.Errorf("resolve row identities: %w", err)
	}
	if len(definitions) == 0 {
		return identities, nil
	}
	if db == nil {
		return core.RowIdentities{}, fmt.Errorf("verify virtual primary keys: database transaction is required")
	}
	if dialect != DialectMySQL && dialect != DialectPostgres {
		return core.RowIdentities{}, fmt.Errorf("verify virtual primary keys: unsupported SQL dialect %d", dialect)
	}

	for _, def := range definitions {
		if err := ctx.Err(); err != nil {
			return core.RowIdentities{}, err
		}
		nullQuery, duplicateQuery := virtualKeyValidationQueries(dialect, def)
		hasNull, err := existsRow(ctx, db, nullQuery)
		if err != nil {
			return core.RowIdentities{}, fmt.Errorf("verify virtual primary key %q.%q for NULL values: %w", def.Schema, def.Name, err)
		}
		if hasNull {
			return core.RowIdentities{}, fmt.Errorf("virtual primary key %q.%q: key contains NULL values", def.Schema, def.Name)
		}

		duplicate, err := existsRow(ctx, db, duplicateQuery)
		if err != nil {
			return core.RowIdentities{}, fmt.Errorf("verify virtual primary key %q.%q for duplicate tuples: %w", def.Schema, def.Name, err)
		}
		if duplicate {
			return core.RowIdentities{}, fmt.Errorf("virtual primary key %q.%q: key contains duplicate tuples", def.Schema, def.Name)
		}
	}
	return identities, nil
}

// virtualKeyValidationQueries expects already schema-validated definitions.
// Identifiers are quoted by the SQL dialect; no key values enter query text.
func virtualKeyValidationQueries(dialect Dialect, def core.VirtualPrimaryKey) (nulls, duplicates string) {
	table := quoteIdentifier(dialect, def.Schema) + "." + quoteIdentifier(dialect, def.Name)
	keyCols := make([]string, 0, len(def.Columns))
	nullConds := make([]string, 0, len(def.Columns))
	for _, column := range def.Columns {
		col := quoteIdentifier(dialect, column)
		keyCols = append(keyCols, col)
		nullConds = append(nullConds, col+" IS NULL")
	}
	nulls = "SELECT 1 FROM " + table + " WHERE " + strings.Join(nullConds, " OR ") + " LIMIT 1"
	duplicates = "SELECT 1 FROM " + table + " GROUP BY " + strings.Join(keyCols, ", ") + " HAVING COUNT(*) > 1 LIMIT 1"
	return nulls, duplicates
}

func existsRow(ctx context.Context, db core.DB, query string) (bool, error) {
	var one int
	err := db.QueryRowContext(ctx, query).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
