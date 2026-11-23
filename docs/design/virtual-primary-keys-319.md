# Virtual primary keys (#319) — v1 research branch

This implementation is based on the unmerged v1 framework PR #464.
It is **not ready for upstream submission or production use**.

## Configuration contract under test

The existing `dump.virtual_references` schema is reused. The only new
configuration field is `dump.virtual_primary_keys`.

```yaml
dump:
  virtual_primary_keys:
    - schema: legacy
      name: customer
      columns: [tenant_id, code]
  virtual_references:
    - schema: legacy
      name: invoice
      references:
        - schema: legacy
          name: customer
          columns:
            - name: tenant_id
            - name: customer_code
```

Source columns and target identity columns are paired **in declaration order**.
Keys are declarations over existing data; no constraints or indexes are created.
A physical primary key cannot be overridden.

Only named columns and ordinary (non-polymorphic) relationships are supported
in this increment. Existing expression/polymorphic configuration is rejected
explicitly rather than silently ignored.

## Planning boundary

`DumpPipeline.Discover` saves the original schema introspection as before.
Only when virtual identities/references are configured does it invoke the
engine-specific `VirtualPlanningBuilder` with the existing dump session.

The MySQL stage checks data using the transaction supplied by
`RunWithOperationalDB`, then returns a detached copy of table payloads
for `DependencyGraphBuilder`. The source result still goes to drift checks,
metadata and dump-context construction. The graph overlay is the only source
of virtual edges used by `subsetbuilder`.

Validation is fail-closed:

- Missing tables/columns, duplicate declarations, conflicts with real PKs,
  mismatched composite-key arities and duplicate graph table names fail.
- The virtual key must contain no NULLs or duplicate tuples in the snapshot.
- Non-NULL child tuples must resolve to a matching parent.
- A nullable virtual reference follows SQL MATCH SIMPLE semantics; a tuple with
  any NULL child column is treated as absent.
- `not_null: true` is an assertion checked against actual child rows. A
  NULL fails validation rather than silently changing the join type.
- Identifiers are quoted with the correct SQL dialect, not concatenated as
  unescaped names.

These probes may scan large tables. No claim is made that validation is cheap
without suitable indexes. The checks must observe the same consistent
read snapshot as data extraction; they are not correct on an unrelated
autocommit connection.

## Evidence

- Live MySQL 8.4 regression: composite cross-tenant values, validated keyless
  parent, new-v1 graph edges, actual generated SQL and returned rows, duplicate
  and NULL key errors, orphaned links, and concurrent changes after a pinned
  InnoDB read snapshot.
- Package race tests, strict production YAML decoding, `go vet`,
  whole-module compilation and query-identifier quoting tests.
- Independent snapshot-pool regression on MySQL 8.4.13 retained the expected
  pre-insert view across the metadata transaction and raw worker connections.
  No pool changes were justified:
  https://github.com/mhatipog/greenmask/tree/research/mysql-snapshot-innodb-view

## Outstanding release gates

1. Prove full dump/restore with this feature enabled through the v1 CLI,
   including metadata, schema extraction, filtering and exact restored rows.
2. Resolve real FK referenced-column introspection (#548). The compatibility
   fallback for old introspection assumes every physical FK targets the PK;
   it is **not correct** for references to alternate UNIQUE keys.
3. Cyclic subset SQL is not implemented in PR #464. This branch rejects
   cyclic subset requests with an error, including self-references, instead
   of panicking. Cycles require separate implementation.
4. Stress snapshot guarantees and verification costs at realistic data sizes;
   define behavior for non-transactional tables. Test PostgreSQL support
   independently before adding a PostgreSQL planning stage.
5. Coordinate the public config and mandatory validation policy with the
   maintainer before submitting the feature.

Scope is intentionally limited: this branch solves verified acyclic
virtual-key graph planning, not arbitrary expressions or general graph cycles.
