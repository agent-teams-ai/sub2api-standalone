# Native connection declarations and admin forms

`GatewayNativeConnectionInventory` owns provider products, connection methods,
credential input grammars, configuration requirements and method revisions.
Admin forms select their existing actions from that same declaration. They do
not maintain a separate provider-to-method registry.

`GatewayConnectionMethodDefinition.AdminActions` describes the admin adapter:
category, form surface, input action, optional OAuth product, add method and
cloud credential mode. The surfaces distinguish Create, admin ReAuth, legacy
ReAuth and the shared authorization form because they implement different
handlers. A native method does not acquire an admin action merely because it
exists. A new method that uses an existing form primitive can declare that
action explicitly; a new renderer primitive still requires an implementation.

Admin configuration requirements describe existing admin capabilities. For
example, AI Studio requires the configured custom OAuth client; the optional
Grok password helper also requires its existing runtime capability check.
These requirements are distinct from protected Gateway mount requirements.

## Generated projections

The existing `BuildPlatformCatalog` generator includes two projections in
`frontend/src/constants/platformCatalog.builtin.json`:

- `admin_connection_methods` contains the choices consumed by admin forms.
- `native_connection_inventory` is the versioned
  `native-connection-inventory/v1` declaration snapshot used for review.

The inventory contains only declaration data: public identities and labels,
protocol families, input fields, requirements and semantic revisions. It does
not include credentials, runtime configuration values, internal mount endpoints
or owner identities. A declaration does not prove that an adapter is mounted or
that a provider completed qualification.

Regenerate through the existing Go test, from `backend`:

```sh
UPDATE_PLATFORM_CATALOG=1 go test -tags unit ./internal/service \
  -run '^TestFrontendBuiltinPlatformCatalogInSync$' -count=1
```

Then run the same test without `UPDATE_PLATFORM_CATALOG` to verify the checked-in
asset. No separate schema compiler or generation command is required.

The literal shared example
`backend/internal/service/testdata/native_connection_catalog_contract.json`
is consumed by Go contract tests and by actual Vue form tests. It introduces
`fixture_browser_grant` through an existing manual authorization action. The
form tests additionally use an independently maintained baseline matrix and
exercise their existing API calls under baseline and conditional configurations.

## Bounded inventory report

The existing Account gateway native qualification workflow exports the current
inventory and compares it with the generated catalog at the PR base, previous
push revision, or previous commit for a manual run. The report records added,
removed and changed products, methods, input/preflight fields and completion
grammars, security and admin requirements, and semantic revisions. Each change
includes its identity and previous/new values;
entries are sorted for reproducibility.

The first base revision without an inventory is reported as
`baseline: "not_exported"`, with current declarations listed as additions.
Malformed, oversized or unknown exported contracts fail the check; they are
never treated as an empty historical baseline.

Bounds are explicit: 2 MiB per snapshot, 128 profiles, 32 methods per profile,
16 fields per input/preflight grammar, 64 choices per field and 8 MiB per
report. Duplicate identities and out-of-bounds field grammars are rejected.

The workflow uploads the source revisions, baseline catalog, current inventory,
diff and contract-test log in its `native-connection-inventory-*` artifact. The
tests require the full export path configuration when exporting:

```sh
NATIVE_INVENTORY_BASELINE=/path/to/base-platform-catalog.json \
NATIVE_INVENTORY_EXPORT=/path/to/current-inventory.json \
NATIVE_INVENTORY_REPORT=/path/to/inventory-diff.json \
NATIVE_INVENTORY_BASE_REVISION="$BASE_REVISION" \
NATIVE_INVENTORY_CURRENT_REVISION="$CURRENT_REVISION" \
go test -tags unit ./internal/service \
  -run 'TestBuildPlatformCatalog|TestFrontendBuiltinPlatformCatalogInSync|TestGatewayConnectionInventory' \
  -count=1 -v
```

The report describes declaration changes. It cannot detect code-only semantic
changes, establish protocol compatibility, or replace provider regressions and
review. Revisions and retirement rules still apply to semantic changes even
when the serialized declaration is unchanged.
