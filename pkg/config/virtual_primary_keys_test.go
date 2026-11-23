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

package config

import (
	"strings"
	"testing"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

// Real production YAML -> Viper -> mapstructure path, with unused-key checks.
func TestVirtualKeysConfig(t *testing.T) {
	v := viper.New()
	v.SetConfigType("yaml")
	require.NoError(t, v.ReadConfig(strings.NewReader(`
dump:
  virtual_primary_keys:
    - schema: legacy
      name: customers
      columns: [tenant_id, customer_code]
  virtual_references:
    - schema: legacy
      name: invoices
      references:
        - schema: legacy
          name: customers
          not_null: true
          columns:
            - name: tenant_id
            - name: customer_code
`)))
	var cfg Config
	require.NoError(t, v.Unmarshal(&cfg, func(c *mapstructure.DecoderConfig) { c.ErrorUnused = true }))
	keys := cfg.Dump.ToCoreVirtualPrimaryKeys()
	require.Len(t, keys, 1)
	require.Equal(t, []string{"tenant_id", "customer_code"}, keys[0].Columns)
	links, err := cfg.Dump.ToCoreVirtualReferences()
	require.NoError(t, err)
	require.Len(t, links, 1)
	require.True(t, links[0].NotNull)
	require.Equal(t, "invoices", links[0].FromTable)
	require.Equal(t, "customers", links[0].ToTable)
	require.Equal(t, []string{"tenant_id", "customer_code"}, links[0].Columns)
	keys[0].Columns[0] = "mutated"
	require.Equal(t, "tenant_id", cfg.Dump.VirtualPrimaryKeys[0].Columns[0])
}

func TestVirtualReferencesUnsupportedShapesAreRejected(t *testing.T) {
	cases := []struct {
		name string
		refs []VirtualReference
		want string
	}{
		{"expression", []VirtualReference{{Schema: "s", Name: "log", References: []Reference{{
			Schema: "s", Name: "target", Columns: []ReferencedColumn{{Expression: "json->'id'"}},
		}}}}, "expression columns not supported"},
		{"polymorphic", []VirtualReference{{Schema: "s", Name: "comment", References: []Reference{{
			Schema: "s", Name: "post", PolymorphicExprs: []string{"kind='post'"}, Columns: []ReferencedColumn{{Name: "id"}},
		}}}}, "polymorphic expressions not supported"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := (Dump{VirtualReferences: tt.refs}).ToCoreVirtualReferences()
			require.ErrorContains(t, err, tt.want)
		})
	}
}
