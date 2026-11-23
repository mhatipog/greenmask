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
	"fmt"
	"strings"
)

// quoteIdentifier quotes a single SQL identifier component, never a raw SQL
// expression. Configured names are matched against actual columns before use.
func quoteIdentifier(dialect Dialect, identifier string) string {
	switch dialect {
	case DialectPostgres:
		return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
	case DialectMySQL:
		return "`" + strings.ReplaceAll(identifier, "`", "``") + "`"
	default:
		panic(fmt.Sprintf("unsupported dialect %d", dialect))
	}
}
