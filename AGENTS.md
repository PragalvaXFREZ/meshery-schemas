# Meshery Schemas — Coding Agent Instructions

This is the central schema repository for the Meshery platform. Schemas here drive Go struct generation, TypeScript type generation, and RTK Query client generation. Mistakes in schema design propagate into generated code across multiple downstream repos (meshery/meshery, layer5io/meshery-cloud).

## Build

```bash
make build       # generate Go structs + TypeScript types + RTK clients
npm run build    # build TypeScript distribution (dist/)
```

Generated files (`models/`, `typescript/generated/`, `dist/`) are NOT committed. Only source schemas and manually written helper files are committed.

## The Dual-Schema Pattern (REQUIRED)

This is the most critical design rule in this repo. Every agent or contributor MUST follow it.

### Rule 1: `<construct>.yaml` = response schema only

The YAML file for an entity represents the **full server-side object** as returned in API responses. It is NOT a request body schema.

**Required properties of every entity `.yaml`:**
- `additionalProperties: false` at the top level
- All server-generated fields defined in `properties`: `id`, `created_at`, `updated_at`, `deleted_at`
- Server-generated fields that are always present belong in `required`

```yaml
# CORRECT: keychain.yaml
type: object
additionalProperties: false
required:
  - id
  - name
  - owner
  - created_at
  - updated_at
properties:
  id:
    $ref: ../../v1alpha1/core/api.yml#/components/schemas/uuid
  name:
    type: string
  owner:
    $ref: ../../v1alpha1/core/api.yml#/components/schemas/uuid
  created_at:
    $ref: ../../v1alpha1/core/api.yml#/components/schemas/created_at
  updated_at:
    $ref: ../../v1alpha1/core/api.yml#/components/schemas/updated_at
  deleted_at:
    $ref: ../../v1alpha1/core/api.yml#/components/schemas/nullTime
```

### Rule 2: Every writable entity needs a `*Payload` schema in `api.yml`

For any entity that has `POST` or `PUT` operations, define a `{Construct}Payload` schema in `api.yml` that:
- Contains **only client-settable fields** — no `created_at`, `updated_at`, `deleted_at`
- Makes `id` optional with `json:"id,omitempty"` for upsert patterns
- Is the schema referenced by all `requestBody` entries for `POST`/`PUT`

```yaml
# CORRECT: in api.yml
components:
  schemas:
    KeychainPayload:
      type: object
      description: Payload for creating or updating a keychain.
      required:
        - name
      properties:
        id:
          $ref: ../../v1alpha1/core/api.yml#/components/schemas/uuid
          description: Existing keychain ID for updates; omit on create.
          x-oapi-codegen-extra-tags:
            json: "id,omitempty"
        name:
          type: string
        owner:
          $ref: ../../v1alpha1/core/api.yml#/components/schemas/uuid
          x-oapi-codegen-extra-tags:
            json: "owner,omitempty"
```

### Rule 3: POST/PUT requestBody MUST reference `*Payload`, never the entity schema

```yaml
# WRONG — forces clients to supply server-generated fields
post:
  requestBody:
    content:
      application/json:
        schema:
          $ref: "#/components/schemas/Keychain"

# CORRECT — separate payload type for writes
post:
  requestBody:
    content:
      application/json:
        schema:
          $ref: "#/components/schemas/KeychainPayload"
```

### Canonical reference implementations

When uncertain, model new schemas on these:
- `schemas/constructs/v1beta1/connection/` — `connection.yaml` + `ConnectionPayload` in `api.yml`
- `schemas/constructs/v1beta1/key/` — `key.yaml` + `KeyPayload` in `api.yml`
- `schemas/constructs/v1beta1/team/` — `team.yaml` + `teamPayload`/`teamUpdatePayload` in `api.yml`
- `schemas/constructs/v1beta1/environment/` — `environment.yaml` + `environmentPayload` in `api.yml`

## Checklist for new entity schemas

Before opening a PR, verify:
- [ ] `<construct>.yaml` has `additionalProperties: false`
- [ ] `<construct>.yaml` lists all server-generated fields in `properties` and appropriate ones in `required`
- [ ] `api.yml` defines `{Construct}Payload` with only client-settable fields
- [ ] All `POST`/`PUT` `requestBody` entries reference `{Construct}Payload`
- [ ] `GET` responses reference the full `{Construct}` entity schema

## Naming conventions

- Property names: `camelCase` (`schemaVersion`, `displayName`)
- ID-suffix fields: `lowerCamelCase` + `Id` (`modelId`, `registrantId`)
- Enums: lowercase words (`enabled`, `ignored`, `duplicate`)
- Object names: singular nouns (`model`, `component`, `design`)
- `components/schemas` names: PascalCase nouns (`Model`, `Component`, `KeychainPayload`)
- Files/folders: lowercase (`api.yml`, `keychain.yaml`, `templates/keychain_template.json`)
- Endpoint paths: `/api` prefix, kebab-case, plural nouns (`/api/workspaces`, `/api/environments`)
- Path params: camelCase with `Id` suffix (`{subscriptionId}`, `{connectionId}`, `{orgId}` — NOT `{orgID}`, NOT `{org_id}`)
- `operationId`: lower camelCase verbNoun (`createKeychain`, `updateEnvironment` — NOT `CreateKeychain`, NOT `UpdateEnvironment`)

## Casing rules at a glance

Every element in the API has exactly one correct casing. The table below is the single authoritative reference:

| Element | Casing | Example | Counter-example |
|---|---|---|---|
| Schema property names (non-DB) | camelCase | `schemaVersion`, `displayName` | ~~`schema_version`~~, ~~`SchemaVersion`~~ |
| ID-suffix properties | camelCase + `Id` | `modelId`, `registrantId` | ~~`modelID`~~, ~~`model_id`~~ |
| DB-mirrored fields | snake_case | `created_at`, `updated_at`, `user_id` | ~~`createdAt`~~ |
| Enum values | lowercase | `enabled`, `ignored` | ~~`Enabled`~~, ~~`ENABLED`~~ |
| `components/schemas` names | PascalCase | `ModelDefinition`, `KeychainPayload` | ~~`modelDefinition`~~ |
| File and folder names | lowercase | `api.yml`, `keychain.yaml` | ~~`Keychain.yaml`~~ |
| Path segments | kebab-case, plural nouns | `/api/role-holders` | ~~`/api/roleHolders`~~ |
| Path parameters | camelCase + `Id` | `{orgId}`, `{workspaceId}` | ~~`{orgID}`~~, ~~`{org_id}`~~ |
| `operationId` | lower camelCase verbNoun | `getAllRoles`, `createWorkspace` | ~~`GetAllRoles`~~, ~~`get_all_roles`~~ |
| Go type names | PascalCase (generated) | `Connection`, `KeychainPayload` | — |
| Go field names | PascalCase (generated) | `CreatedAt`, `UpdatedAt` | — |
| TypeScript type names | PascalCase (generated) | `Connection`, `KeychainPayload` | — |

**snake\_case is only for DB-mirrored fields** — `created_at`, `updated_at`, `deleted_at`, `user_id`, and similar fields that map directly to a database column named with underscores. All other names follow the rules above.

## HTTP API Design Principles

These rules govern how endpoints are structured. They are enforced in part by `make validate-schemas`.

### HTTP method semantics

| Use case | Method | Example |
|---|---|---|
| Create a resource | `POST` | `POST /api/workspaces` → 201 |
| Upsert a resource | `POST` | `POST /api/keys` → 200 |
| Update an existing resource | `PUT` or `PATCH` | `PUT /api/workspaces/{workspaceId}` → 200 |
| Non-CRUD action on a resource | `POST` to a sub-resource path | `POST /api/invitations/{invitationId}/accept` |
| Bulk delete | `POST` to a `/delete` sub-resource | `POST /api/designs/delete` → 200 |
| Single delete | `DELETE` | `DELETE /api/keys/{keyId}` → 204 |

**Do NOT use `DELETE` with a request body for bulk operations.** REST semantics do not define a request body for `DELETE`; many HTTP clients and proxies strip it silently. Use a `POST /api/{resources}/delete` sub-resource instead:

```yaml
# WRONG — DELETE with a request body
delete:
  operationId: deletePatterns
  requestBody:
    content:
      application/json:
        schema:
          $ref: '#/components/schemas/PatternIds'

# CORRECT — POST sub-resource for bulk delete
post:
  operationId: deletePatterns
  summary: Bulk delete patterns by ID
  requestBody:
    content:
      application/json:
        schema:
          $ref: '#/components/schemas/PatternIds'
  responses:
    "200":
      description: Patterns deleted
```

### HTTP response codes

| Code | Meaning | When to use |
|---|---|---|
| 200 | OK | Request succeeded; body contains the result (queries, upserts, actions) |
| 201 | Created | A new resource was created; body contains the new resource |
| 202 | Accepted | Request received; operation will complete asynchronously |
| 204 | No Content | Request succeeded; no response body (e.g., a single-resource `DELETE`) |

Use **201** (not 200) for `POST` endpoints that exclusively create a new resource. Use **200** for upsert operations where the resource may already exist.

### Resource grouping and path structure

Endpoints are grouped into logical categories under `/api`:

| Category prefix | Domain |
|---|---|
| `/api/identity/` | Users, orgs, roles, teams, invitations |
| `/api/integrations/` | Connections, environments, credentials |
| `/api/content/` | Designs, views, components, models |
| `/api/entitlement/` | Plans, subscriptions, features |
| `/api/auth/` | Tokens, keychains, keys |

New endpoints must be placed in the appropriate category. Path segments must be kebab-case plural nouns matching the resource name.

## File structure for a construct

```
schemas/constructs/v1beta1/<construct>/
  api.yml                          # OpenAPI spec: endpoints + all schema definitions
  <construct>.yaml                 # Entity (response) schema
  templates/
    <construct>_template.json      # Example instance
    <construct>_template.yaml
```

## Go helper files

Auto-generated Go structs (`models/<version>/<construct>/<construct>.go`) are NOT committed. Manually written helpers ARE committed:

```
models/v1beta1/<construct>/
  <construct>.go          # Auto-generated — DO NOT edit
  <construct>_helper.go   # Manual — add SQL driver, Entity interface, TableName(), etc.
```

## The Dual-Schema Pattern (REQUIRED for all entity schemas)

This is the most critical API design rule in this repo. Violations produce incorrect generated Go structs and API clients downstream in `meshery/meshery` and `layer5io/meshery-cloud`.

### Rule 1 — `<construct>.yaml` is a response schema only

The YAML file for an entity represents the **full server-side object** returned in API responses. It is **not** a request body schema.

Every entity `<construct>.yaml` must have:
- `additionalProperties: false` at the top level
- All server-generated fields defined in `properties`: `id`, `created_at`, `updated_at`, `deleted_at`
- Server-generated fields that are always present in responses listed in `required`

### Rule 2 — Every writable entity needs a `*Payload` schema in `api.yml`

For any entity that supports `POST` or `PUT`, define a `{Construct}Payload` schema in `api.yml`:
- Contains **only client-settable fields** — never `created_at`, `updated_at`, `deleted_at`
- `id` is optional with `json:"id,omitempty"` for upsert patterns, or omitted entirely for create-only
- Referenced by all `requestBody` entries for `POST`/`PUT`

### Rule 3 — `POST`/`PUT` requestBody must reference `*Payload`, never the entity schema

```yaml
# WRONG — forces clients to supply server-generated fields
post:
  requestBody:
    content:
      application/json:
        schema:
          $ref: "#/components/schemas/Keychain"

# CORRECT
post:
  requestBody:
    content:
      application/json:
        schema:
          $ref: "#/components/schemas/KeychainPayload"
  responses:
    "200":
      content:
        application/json:
          schema:
            $ref: "#/components/schemas/Keychain"   # full entity in response
```

### Canonical reference implementations

Model new schemas on these:
- `schemas/constructs/v1beta1/connection/` — `connection.yaml` + `ConnectionPayload`
- `schemas/constructs/v1beta1/key/` — `key.yaml` + `KeyPayload`
- `schemas/constructs/v1beta1/team/` — `team.yaml` + `teamPayload`/`teamUpdatePayload`
- `schemas/constructs/v1beta1/environment/` — `environment.yaml` + `environmentPayload`

---

## SQL Driver (`Scan`/`Value`) Implementation Rules

When manually implementing `sql.Scanner` and `driver.Valuer` for map-like types:

### Rule: Always serialize — never return SQL NULL from `Value()`

The established pattern in this repo (`core.Map`) always marshals to a JSON string. A nil map marshals to the JSON string `"null"`, **not** SQL NULL. New implementations must match this unless the column is explicitly nullable and nil-vs-empty distinction is required and documented.

```go
// CORRECT — matches core.Map pattern
func (m MapObject) Value() (driver.Value, error) {
    b, err := json.Marshal(m)
    if err != nil {
        return nil, err
    }
    return string(b), nil
}

// WRONG — writes SQL NULL, inconsistent with core.Map
func (m MapObject) Value() (driver.Value, error) {
    if m == nil {
        return nil, nil   // ← do not do this
    }
    ...
}
```

### Rule: `Scan` must zero the receiver on nil src

When `src` is nil (SQL NULL), explicitly zero the receiver. Returning without modifying it leaves stale data if the same struct is reused across rows.

```go
// CORRECT
case nil:
    *m = nil
    return nil

// WRONG — leaves stale data
case nil:
    return nil
```

---

## Common Mistakes to Avoid

1. ❌ Committing generated Go code in `models/` directory
2. ❌ Committing generated TypeScript code in `typescript/generated/` directory
3. ❌ Committing built files in `dist/` directory
4. ❌ Using deprecated `core.json` references
5. ❌ Adding redundant `x-oapi-codegen-extra-tags` when using schema references
6. ❌ Forgetting to update template files in the `templates/` subdirectory with default values
7. ❌ Not testing the build process after schema changes
8. ❌ Placing template files outside the `templates/` subdirectory
9. ❌ Using `.d.ts` extension in TypeScript import paths
10. ❌ Assuming schema property names are PascalCase (check actual generated `.d.ts` files)
11. ❌ Adding `x-generate-db-helpers` on individual properties — it must be at the schema component level
12. ❌ Using `x-generate-db-helpers` on amorphous types without a fixed schema — use `x-go-type: "core.Map"` instead
13. ❌ Using the full entity schema as a `POST`/`PUT` `requestBody` — always use a separate `*Payload` schema
14. ❌ Omitting `additionalProperties: false` from entity `<construct>.yaml` files
15. ❌ Returning `(nil, nil)` from `Value()` in SQL driver implementations — always marshal, even for nil maps
16. ❌ Returning without zeroing the receiver in `Scan()` when `src` is nil — set `*m = nil` first
17. ❌ Using PascalCase for new `operationId` values — always lower camelCase (`getPatterns`, not `GetPatterns`)
18. ❌ Using SCREAMING\_CASE path parameters (`{orgID}`, `{roleID}`) — always camelCase with `Id` suffix (`{orgId}`, `{roleId}`)
19. ❌ Using `DELETE` with a request body for bulk operations — use `POST /api/{resources}/delete` instead
20. ❌ Returning 200 from a `POST` that exclusively creates a new resource — use 201

## Checklist for Schema Changes

- [ ] Modified only schema JSON/YAML files (not generated code)
- [ ] Updated corresponding template files in `templates/` subdirectory with default values
- [ ] Used non-deprecated `v1alpha1/core/api.yml` references
- [ ] If adding new schemas, referenced them from `api.yml` (the construct index file)
- [ ] Removed redundant tags when using schema references
- [ ] If a schema type is stored as a JSON blob in a DB column AND has a dedicated schema definition, used `x-generate-db-helpers: true` at the schema component level (not per-property)
- [ ] Ran `make build` successfully
- [ ] Ran `go test ./...` successfully
- [ ] Ran `npm run build` successfully
- [ ] Verified only schema JSON/YAML files are in the commit
- [ ] If updating `typescript/index.ts`, verified import paths are correct
- [ ] (New entity) `<construct>.yaml` has `additionalProperties: false`
- [ ] (New entity) `<construct>.yaml` includes all server-generated fields in `properties` and `required`
- [ ] (New entity with writes) `api.yml` defines a `{Construct}Payload` with only client-settable fields
- [ ] (New entity with writes) All `POST`/`PUT` `requestBody` entries reference `{Construct}Payload`, not `{Construct}`
- [ ] (New SQL driver) `Value()` always marshals — never returns `(nil, nil)`
- [ ] (New SQL driver) `Scan()` sets `*m = nil` when `src` is nil
- [ ] (New endpoint) `operationId` is lower camelCase verbNoun
- [ ] (New endpoint) Path parameters are camelCase with `Id` suffix (e.g., `{workspaceId}`, not `{workspaceID}`)
- [ ] (New endpoint) No `DELETE` operation has a `requestBody` — bulk deletes use `POST .../delete`
- [ ] (New `POST` for creation only) Response code is 201, not 200

Always add `// This is not autogenerated.` at the top of helper files.


Use `x-generate-db-helpers: true` on a schema component to auto-generate `Scan`/`Value` SQL driver methods for types stored as JSON blobs in a single DB column. Do NOT use this for types mapped to full DB tables.

## x-internal annotation

Control which bundled output includes an API path:
- `x-internal: ["cloud"]` — cloud-only (`cloud_schema.yml`)
- `x-internal: ["meshery"]` — Meshery-only (`meshery_schema.yml`)
- Omit `x-internal` — included in all bundles
