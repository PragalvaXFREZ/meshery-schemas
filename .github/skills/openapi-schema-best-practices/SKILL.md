---
name: openapi-schema-best-practices
description: 'Create, audit, and maintain OpenAPI schemas in meshery/schemas following established conventions. Use this skill whenever working with OpenAPI YAML files, adding new API constructs, reviewing schema consistency, checking naming conventions, validating $ref patterns, or understanding how schemas flow through the Go and TypeScript code generators. Also trigger when the user mentions schema audits, API consistency checks, schema linting, construct definitions, or asks about the build pipeline (bundle-openapi, generate-golang, generate-typescript). If the user is creating a schema from a Golang model, prefer the create-openapi-schemas-from-golang-models skill instead.'
---

# OpenAPI Schema Best Practices

You are an expert in Meshery's Schema-Driven Development (SDD) system. Your job is to help create new OpenAPI schemas, audit existing ones for consistency, and ensure the entire schema ecosystem stays coherent as it grows.

Before doing any schema work, read `.github/agents/schemas-code-contributor.md` and `AGENTS.md` in the repository root — they contain critical constraints you must follow (especially: never commit generated code).

## How this repository works

Meshery defines its data model as OpenAPI 3.0 YAML schemas under `schemas/constructs/`. A build pipeline then generates Go structs and TypeScript types from those schemas automatically. The schemas are the single source of truth — never hand-edit generated files.

### Build pipeline overview

```
schemas/constructs/**/*.yaml  (you write these)
        │
        ▼
  bundle-openapi.js           (bundles + dereferences via swagger-cli)
        │
        ▼
  _openapi_build/**/*.json    (intermediate bundled JSON)
        │
        ├──▶ generate-golang.js    → models/**/*.go         (oapi-codegen)
        ├──▶ generate-typescript.js → typescript/generated/  (openapi-typescript)
        └──▶ generate-rtk.js       → typescript/rtk/        (RTK Query hooks)
```

Understanding this pipeline matters because schema design decisions directly affect the quality of generated code. A poorly structured schema produces awkward Go structs and confusing TypeScript types.

### How code generators consume schemas

**Go generator** (`build/generate-golang.js`):
- Reads bundled JSON from `_openapi_build/constructs/`
- Generates Go structs with JSON + YAML struct tags
- Collects `x-oapi-codegen-extra-tags` to add custom struct tags (GORM, db, etc.)
- Builds import mappings from external `$ref` targets so cross-package types resolve correctly
- Uses `oapi-codegen` v2.5.1 under the hood

**TypeScript generator** (`build/generate-typescript.js`):
- Reads the same bundled JSON
- Produces TypeScript type definitions via `openapi-typescript`
- Also exports JSON schema as TypeScript const objects (`*Schema.ts`)

**RTK Query generator** (`build/generate-rtk.js`):
- Reads filtered merged specs (`cloud_openapi.yml`, `meshery_openapi.yml`)
- Generates RTK Query hooks from the `paths` defined in `api.yml` files
- Tags endpoints using `x-internal` to split between cloud and meshery APIs

### Package discovery

The build system discovers packages dynamically by walking `schemas/constructs/<version>/` and finding directories that contain an `api.yml` file. This means:
- Every new construct needs an `api.yml` to be picked up by the build
- The directory name becomes the package name (with some overrides, e.g. `design` → `pattern`)
- Some packages are excluded from merging: `v1alpha1/core` and `v1alpha1/capability`

## Directory structure for a construct

```
schemas/constructs/<version>/<construct>/
├── api.yml                          # Index file (required): aggregates subschemas + defines API paths
├── <construct>.yaml                 # Subschema: main data definitions
├── <construct>_core.yml             # Subschema: core definitions (optional)
└── templates/
    ├── <construct>_template.json    # Default-value template
    ├── <construct>_template.yaml    # YAML variant (optional)
    └── <construct>_minimal_template.json  # Minimal variant (optional)
```

The `api.yml` is the entry point. It references subschemas via `$ref` and defines REST endpoints under `paths:`. Code generators only read `api.yml` — subschemas are pulled in through references.

## Naming conventions

These conventions are non-negotiable for consistency across all APIs:

| Element | Convention | Examples |
|---------|-----------|----------|
| Schema property names | camelCase | `schemaVersion`, `displayName`, `componentsCount` |
| Identifier fields | camelCase + "Id" suffix | `modelId`, `registrantId`, `categoryId` |
| Enum values | lowercase | `enabled`, `ignored`, `duplicate` |
| Schema component names | PascalCase | `ModelDefinition`, `ComponentDefinition` |
| File/folder names | lowercase, underscores OK | `model.yaml`, `model_core.yml`, `api.yml` |
| API paths | `/api` prefix, kebab-case, plural nouns | `/api/workspaces`, `/api/environments` |
| Path parameters | camelCase | `{subscriptionId}`, `{connectionId}` |
| operationId | camelCase VerbNoun | `registerMeshmodels`, `getAllRoles` |
| Version strings | k8s-style | `v1alpha1`, `v1beta1` |
| schemaVersion | group/version | `models.meshery.io/v1beta1` |

## Common schema references

The `v1alpha1/core/api.yml` file defines reusable building blocks. Always reference these instead of redefining them:

```yaml
# Timestamps — use the core refs, not inline definitions
created_at:
  $ref: ../../v1alpha1/core/api.yml#/components/schemas/created_at
  x-order: 14
updated_at:
  $ref: ../../v1alpha1/core/api.yml#/components/schemas/updated_at
  x-order: 15

# UUIDs
id:
  $ref: ../../v1alpha1/core/api.yml#/components/schemas/uuid

# Version strings
version:
  $ref: ../../v1alpha1/core/api.yml#/components/schemas/versionString

# Semver
version:
  $ref: ../../v1alpha1/core/api.yml#/components/schemas/semverString
```

When using a `$ref` to a core schema that already defines `x-oapi-codegen-extra-tags`, do NOT add redundant tags — they're already in the core definition.

For schemas in `v1alpha3`, the relative path is shorter: `../v1alpha1/core/api.yml#/...`

## Schema design patterns

### Pagination response

Every list endpoint should return a paginated wrapper:

```yaml
<Construct>Page:
  type: object
  properties:
    page:
      type: integer
    data:
      type: array
      items:
        $ref: '#/components/schemas/<Construct>'
    totalCount:
      type: integer
    pageSize:
      type: integer
```

### Nullable time fields

```yaml
deletedAt:
  x-go-type: "core.NullTime"
  $ref: "../../v1alpha1/core/api.yml#/components/schemas/deleted_at"
  x-oapi-codegen-extra-tags:
    db: "deleted_at"
```

### Cross-construct references

When referencing types from another construct, use both `$ref` and Go type hints so the generated code imports correctly:

```yaml
Invitation:
  $ref: "../invitation/api.yml#/components/schemas/Invitation"
  x-go-type: "invitation.Invitation"
  x-go-type-import:
    path: "github.com/meshery/schemas/models/v1beta1/invitation"
```

### Custom Go types for complex fields

```yaml
metadata:
  type: object
  additionalProperties: true
  x-go-type: "core.Map"
  x-go-type-skip-optional-pointer: true
  x-oapi-codegen-extra-tags:
    db: "metadata"
```

### String arrays (pq.StringArray for PostgreSQL)

```yaml
roleNames:
  type: array
  items:
    type: string
  x-go-type: "pq.StringArray"
  x-go-import-path: "github.com/lib/pq"
```

### Internal API marking

Use `x-internal` to scope endpoints to specific deployments:

```yaml
x-internal: ["cloud"]    # Cloud-only endpoint
x-internal: ["meshery"]  # Meshery OSS-only endpoint
```

The build pipeline uses these tags to produce separate merged specs (`cloud_openapi.yml` vs `meshery_openapi.yml`).

## Creating a new schema

### Step 1: Create the construct directory

```bash
mkdir -p schemas/constructs/v1beta1/<construct>/templates
```

### Step 2: Write the subschema (`<construct>.yaml`)

Map each field to its OpenAPI type. Include `x-oapi-codegen-extra-tags` for fields that need custom Go struct tags (json, yaml, db, gorm):

```yaml
type: object
properties:
  id:
    $ref: "../../v1alpha1/core/api.yml#/components/schemas/uuid"
    x-oapi-codegen-extra-tags:
      json: "id,omitempty"
      yaml: "id,omitempty"
      db: "id"
  name:
    type: string
    description: Human-readable name
    x-oapi-codegen-extra-tags:
      json: "name,omitempty"
      yaml: "name,omitempty"
      db: "name"
  created_at:
    $ref: ../../v1alpha1/core/api.yml#/components/schemas/created_at
    x-order: 14
  updated_at:
    $ref: ../../v1alpha1/core/api.yml#/components/schemas/updated_at
    x-order: 15
```

### Step 3: Write the API index (`api.yml`)

```yaml
openapi: 3.0.0
info:
  title: <Construct> API
  version: v1beta1

paths:
  /api/<constructs>:
    get:
      operationId: getAll<Constructs>
      # ... parameters, responses
    post:
      operationId: create<Construct>
      # ... requestBody, responses

components:
  schemas:
    <Construct>:
      $ref: './<construct>.yaml'
    <Construct>Page:
      # ... pagination wrapper
```

### Step 4: Create template files

Templates provide default values for programmatic creation:

```json
{
  "id": "00000000-0000-0000-0000-000000000000",
  "name": "",
  "created_at": "0001-01-01T00:00:00Z",
  "updated_at": "0001-01-01T00:00:00Z"
}
```

### Step 5: Build and verify

```bash
make build          # Bundles schemas and generates all code
go test ./...       # Run Go tests
npm run build       # Build TypeScript package
```

Only commit the schema YAML files and template files. The generated code is produced by CI.

## Auditing schemas for consistency

When reviewing or auditing schemas, check every item on this list:

### Naming audit

- [ ] All property names are camelCase (not snake_case, not PascalCase)
- [ ] Identifier fields end with "Id" suffix (e.g., `modelId` not `model_id` or `modelID`)
- [ ] Enum values are lowercase
- [ ] Schema component names under `components/schemas` are PascalCase
- [ ] API paths use kebab-case with plural nouns under `/api`
- [ ] Path parameters are camelCase
- [ ] `operationId` values follow camelCase VerbNoun pattern

### Reference audit

- [ ] No references to deprecated `core.json` — all use `v1alpha1/core/api.yml`
- [ ] Timestamps use `$ref` to core schemas (not inline type definitions)
- [ ] UUIDs use `$ref` to core `uuid` schema
- [ ] No redundant `x-oapi-codegen-extra-tags` on fields that already have them in the referenced schema
- [ ] Cross-construct refs include `x-go-type` and `x-go-type-import` for proper Go imports

### Structure audit

- [ ] Every construct directory has an `api.yml` index file
- [ ] Subschemas are referenced from `api.yml` via `$ref`
- [ ] Template files exist in `templates/` subdirectory with sensible defaults
- [ ] `openapi: 3.0.0` version is declared (not 3.1.0 — `oapi-codegen` requires 3.0.x)
- [ ] Each construct defines `info.title` and `info.version`

### Consistency across constructs

- [ ] Pagination responses follow the standard `<Construct>Page` pattern
- [ ] Similar fields across constructs use the same types (don't define `status` as string in one and enum in another without reason)
- [ ] `x-internal` tags are applied consistently for cloud vs meshery endpoints
- [ ] `x-order` values don't collide within the same schema

### Code generation readiness

- [ ] Schema will produce clean Go structs (check for ambiguous `oneOf`/`anyOf` that create unwieldy union types)
- [ ] Fields that store JSON blobs in the database use `x-go-type: "core.Map"` with `x-go-type-skip-optional-pointer: true`
- [ ] Array fields backed by PostgreSQL use `x-go-type: "pq.StringArray"` where appropriate
- [ ] Nullable database fields use proper nullable markers

## What NOT to do

These are the most common mistakes. If you catch yourself doing any of them, stop:

1. **Committing generated code** — files in `models/`, `typescript/generated/`, `dist/`, or `_openapi_build/` are auto-generated. Only commit schema YAML and template files.
2. **Using deprecated core.json references** — always use `v1alpha1/core/api.yml`.
3. **Defining timestamps inline** — use `$ref` to core schema timestamps.
4. **Adding redundant extra tags** — if the `$ref` target already has `x-oapi-codegen-extra-tags`, don't duplicate them.
5. **Using OpenAPI 3.1.0** — the code generators require 3.0.x.
6. **Placing templates outside `templates/`** — they belong in the `templates/` subdirectory.
7. **Using `.d.ts` extension in TypeScript imports** — use extensionless paths.
8. **Forgetting to update `typescript/index.ts`** — when adding a new construct, add the import and type export to this manually-maintained file.

## Validation commands

```bash
# Lint a specific schema
npx @redocly/cli lint schemas/constructs/v1beta1/<construct>/api.yml

# Full build (validates + generates everything)
make build

# Run Go tests
go test ./...

# Run TypeScript build
npm run build

# See all available make targets
make
```

## Related resources

- **Agent guidelines**: `.github/agents/schemas-code-contributor.md` — detailed contributor rules
- **Repository guidelines**: `AGENTS.md` — complete checklist for schema changes
- **Existing skill**: `.github/skills/create-openapi-schemas-from-golang-models/` — specialized workflow for creating schemas from Go models in `layer5io/meshery-cloud`
- **Build scripts**: `build/` directory — the bundler and all code generators
- **Core schemas**: `schemas/constructs/v1alpha1/core/api.yml` — reusable building blocks
- **Example constructs**: `schemas/constructs/v1beta1/model/`, `schemas/constructs/v1beta1/environment/` — well-established patterns to follow
