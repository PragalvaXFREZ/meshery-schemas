# Identifier Naming — Contributor Guide & Migration Summary

> An announcement-grade summary for **all Meshery contributors** — community and staff — explaining the identifier-naming migration (code-named "Option B") that shipped between **2026-04-22 and 2026-04-24** across every repository in the Layer5 / Meshery ecosystem.
>
> **Audience:** anyone who writes Go, TypeScript, OpenAPI, or SQL in a Layer5 / Meshery repo.
> **Prerequisite knowledge:** none — the document is self-contained.

---

## TL;DR

- We standardized **one** naming convention across every software element in every repo. The wire is **camelCase everywhere**; the database is **snake_case**; Go fields follow **Go idiom**; the ORM is the **only translation layer**.
- **91 pull requests** landed across **6 repositories** in three days. **7 npm packages** were published (4 of `@meshery/schemas`, 3 of `@sistent/sistent`). Both packages are live on npm at `v1.2.0` and `v0.20.0` respectively.
- The schemas validator now enforces the contract at three CI gates: **blocking schema validation**, advisory schema audit, and **blocking consumer-audit** that scans `meshery`, `meshery-cloud`, and `meshery-extensions` on every PR.
- If you're writing new code, the single most important thing to know is in the **[Canonical Naming Directory](#canonical-naming-directory)** below — the headline table of this document.

---

## Canonical Naming Directory

This is the authoritative, per-element, per-layer naming convention. Use this table as your reference any time you introduce a new schema property, Go struct, TypeScript type, URL, query parameter, operation ID, file name, enum value, or error code.

### Code elements

| # | Element | Convention | Example | Counter-example |
|---|---|---|---|---|
| 1 | Database column name | `snake_case` | `user_id`, `created_at`, `pattern_file`, `view_count` | ~~`userId`~~, ~~`orgID`~~ |
| 2 | Go `db:` struct tag | `snake_case` (matches column) | `` `db:"user_id"` ``, `` `db:"created_at"` `` | ~~`` `db:"userId"` ``~~ |
| 3 | Go struct field (exported) | `PascalCase` + Go-idiomatic initialisms | `UserID`, `OrgID`, `WorkspaceID`, `CreatedAt` | ~~`User_id`~~, ~~`UserIdentifier`~~ |
| 4 | Go struct field (unexported) | `camelCase` + Go-idiomatic initialisms | `userID`, `orgID`, `createdAt` | ~~`user_id`~~, ~~`userId`~~ |
| 5 | JSON tag on Go field | `camelCase` (regardless of DB backing) | `` `json:"userId"` ``, `` `json:"createdAt"` `` | ~~`` `json:"user_id"` ``~~, ~~`` `json:"orgID"` ``~~ |
| 6 | OpenAPI schema property name | `camelCase` | `userId:`, `patternFile:`, `createdAt:` | ~~`user_id:`~~, ~~`pattern_file:`~~ |
| 7 | TypeScript property / RTK query arg | `camelCase` | `response.userId`, `queryArg.orgId` | ~~`response.user_id`~~, ~~`queryArg.orgID`~~ |
| 8 | Enum value (new) | `lowercase` words | `enabled`, `ignored`, `duplicate` | ~~`Enabled`~~, ~~`ENABLED`~~ |
| 9 | Go type name (generated or hand-written) | `PascalCase` | `Workspace`, `KeychainPayload`, `MesheryPattern` | ~~`workspacePayload`~~ |
| 10 | TypeScript type name (generated or hand-written) | `PascalCase` | `Workspace`, `KeychainPayload` | ~~`workspacePayload`~~ |
| 11 | OpenAPI `components/schemas` entry | `PascalCase` nouns | `Model`, `Component`, `KeychainPayload` | ~~`keychainPayload`~~ |
| 12 | OpenAPI `operationId` | `camelCase` verbNoun | `createKeychain`, `getWorkspaces`, `updateEnvironment` | ~~`CreateKeychain`~~, ~~`get_all_roles`~~ |
| 13 | OpenAPI tag name | Single lowercase words where possible, else kebab | `connection`, `workspace`, `role-holder` | ~~`Connection`~~, ~~`roleHolder`~~ |
| 14 | URL path segment | `kebab-case`, plural nouns | `/api/workspaces`, `/api/role-holders`, `/api/environments` | ~~`/api/roleHolders`~~, ~~`/api/workspace`~~ |
| 15 | URL path parameter | `camelCase` with `Id` suffix (lowercase `d`) | `{workspaceId}`, `{orgId}`, `{roleId}` | ~~`{orgID}`~~, ~~`{org_id}`~~, ~~`{orgid}`~~ |
| 16 | URL query parameter | `camelCase` | `?userId=…`, `?orgId=…`, `?pageSize=25`, `?order=updatedAt%20desc` | ~~`?user_id=…`~~, ~~`?orgID=…`~~, ~~`?page_size=…`~~ |
| 17 | Pagination envelope fields | `camelCase` on new versions: `page`, `pageSize`, `totalCount` | `{ "page": 1, "pageSize": 25, "totalCount": 237 }` | ~~`page_size`~~, ~~`total_count`~~ (legacy, deprecated) |
| 18 | HTTP response status for create | `201 Created` | `POST /api/workspaces` → 201 + new resource in body | ~~`POST` → 200~~ |
| 19 | HTTP response status for upsert | `200 OK` | `POST /api/keys` (upsert) → 200 | — |
| 20 | HTTP response status for single delete | `204 No Content` | `DELETE /api/keys/{keyId}` → 204, no body | ~~200 with echoed body~~ |
| 21 | HTTP method for bulk delete | `POST .../delete`, never `DELETE` with body | `POST /api/designs/delete` with JSON body listing IDs | ~~`DELETE /api/designs` with body~~ |
| 22 | Error code (mesheryctl) | `mesheryctl-NNNN` format, each code unique | `mesheryctl-1232` | two `ErrXxxCode` constants with the same `NNNN` |
| 23 | File name | `lowercase`, descriptive | `api.yml`, `keychain.yaml`, `sql-utils.go`, `context_helpers.go` | ~~`Keychain.yaml`~~, ~~`SqlUtils.go`~~ |
| 24 | Folder name | `lowercase`, singular | `schemas/constructs/v1beta3/keychain/`, `ui/components/identity/` | ~~`Schemas/Constructs/`~~ |
| 25 | Template file | `<construct>_template.json` / `.yaml`, inside `templates/` | `templates/keychain_template.json` | template alongside the schema file |
| 26 | RTK Query generated endpoint name | `camelCase` verbNoun, matches `operationId` | `useGetWorkspacesQuery`, `useCreateKeychainMutation` | hand-written `useGetMyCustomQuery` paralleling a generated one |

### Wire-field casing — a closer look

On newly authored (canonical-casing) API versions, **every JSON tag is camelCase, regardless of whether the field is DB-backed.** The snake-case DB column name lives exclusively in `x-oapi-codegen-extra-tags.db` on the OpenAPI side and in the `db:` Go struct tag on the generated code. On DB-backed fields, the `json:` and `db:` tags differ by design:

```yaml
# OpenAPI — canonical form for a DB-backed field
patternFile:
  type: string
  description: Stored as a JSON blob.
  x-oapi-codegen-extra-tags:
    db: "pattern_file"
```

```go
// Generated Go
PatternFile string `json:"patternFile" db:"pattern_file"`
```

```typescript
// Generated TypeScript
patternFile: string;
```

This is a **retirement** of the older rule that said "when a field is DB-backed, its JSON tag should match its DB column name." That rule is gone. Wire is camelCase; DB is snake_case; the ORM layer is the only translation.

### Pagination envelope — the legacy exception

Pagination envelope fields historically used `snake_case` on the wire (`page_size`, `total_count`). On **newly authored API versions**, use `pageSize` and `totalCount`. On legacy resources that still publish the snake form, do not recase them in-place — that is a partial-casing migration and is forbidden by validator Rule 45. The snake forms attrite as resources migrate to their next canonical-casing version bump.

The field `page` is already a single-word identifier and stays `page` in both legacy and canonical forms.

---

## Before / after — concrete examples

| | Before | After |
|---|---|---|
| Workspace request body | `{ "organization_id": "..." }` | `{ "organizationId": "..." }` |
| Design response field | `{ "view_count": 42, "design_type": "Helm Chart" }` | `{ "viewCount": 42, "designType": "Helm Chart" }` |
| List query parameter | `GET /api/workspaces?organization_id=X&page_size=25` | `GET /api/workspaces?orgId=X&pageSize=25` |
| Path parameter | `GET /api/workspaces/{workspaceID}` | `GET /api/workspaces/{workspaceId}` |
| OpenAPI `operationId` | `GetAllRoles`, `get_workspaces` | `getAllRoles`, `getWorkspaces` |
| Go struct JSON tag | `LastRun *time.Time \`json:"last_run,omitempty"\`` | `LastRun *time.Time \`json:"lastRun,omitempty"\`` |
| OpenAPI response property | `user_id: { $ref: "..." }` | `userId: { $ref: "..." }` |
| Sort parameter | `?order=view_count%20desc` | `?order=viewCount%20desc` |
| RTK Query hook argument | `useGetUserTokensQuery({ isOAuth: true })` | `useGetUserTokensQuery({ isOauth: true })` |
| Error code constant | two `Err…Code` values pointing at `mesheryctl-1231` | each `Err…Code` has a unique `mesheryctl-NNNN` |

---

## Overlap-window guarantee (dual-accept)

While this migration was landing, server-side handlers on resources that touched the wire added **`UnmarshalJSON` dual-accept shims** or **`utils.QueryParam` dual-read helpers** so that requests carrying the old snake_case wire form continue to work for one deprecation cycle. If you are writing a server handler that consumes a field we just migrated, this means you can trust that **both** `viewCount` and `view_count` parse to the same Go field for the duration of the overlap. The snake path will be retired per-resource at each resource's next canonical-casing version bump.

New handlers on newly authored versions do **not** carry dual-accept shims — they accept only the canonical camelCase form.

---

## By the numbers

### Pull requests merged

**91 merged PRs** across 6 repositories in the 2026-04-22 — 2026-04-24 window:

| Repository | Merged PRs |
|---|---:|
| `meshery/schemas` | **51** |
| `meshery/meshery` | **13** |
| `layer5io/meshery-cloud` | **13** |
| `layer5labs/meshery-extensions` | **8** |
| `layer5io/sistent` | **5** |
| `meshery/meshkit` | **1** |
| **Total** | **91** |

### Releases cut

**15 release tags** published across the 6 repositories; **7 of them are npm packages** consumed across every Layer5 / Meshery UI and server:

| Repository | Releases (in window) |
|---|---|
| `@meshery/schemas` (npm) | v1.1.0, v1.1.1, v1.1.2, **v1.2.0** (current) |
| `@sistent/sistent` (npm) | v0.19.0, v0.19.1, **v0.20.0** (current) |
| `meshery/meshkit` (Go module) | v1.0.5 |
| `meshery/meshery` (server / CLI) | v1.0.10, v1.0.11 |
| `layer5io/meshery-cloud` (server / UI) | v1.0.18, v1.0.19, v1.0.20 |
| `layer5labs/meshery-extensions` | v1.0.10-1, v1.0.11-1 |

---

## Contributors

| Contributor | PRs | Primary contributions |
|---|---:|---|
| **[@leecalcote](https://github.com/leecalcote)** — Lee Calcote | **58** | Authored and merged all 22 Phase 3 per-resource canonical-casing schema version bumps (workspace, environment, organization, user, design, connection, team, role, credential, event, view, key, keychain, invitation, plan, subscription, token, badge, schedule, model, component, relationship). Drove every downstream Phase 3 consumer repoint across meshery, meshery-cloud, and meshery-extensions. Authored the Phase 4.A administrative-close decision to retain legacy directories instead of deleting them. Authored the `identifier-naming mandate` doc adoption in all four repo `AGENTS.md` files (Phase 4.C). |
| **[@jamieplu](https://github.com/jamieplu)** | **16** | Authored the entire Phase 0 (baseline artifacts) and Phase 1 (governance + validator hardening) block on `meshery/schemas`: the identifier-naming migration plan (`docs/identifier-naming-migration.md`), the `AGENTS.md` contract amendment, Rule 6 inversion, Rule 32 retirement, Rule 45 (partial casing forbidden), Rule 46 (sibling-endpoint parity), Rule 4 extension to query parameters, the TypeScript consumer auditor (`validation/consumer_ts.go`), the advisory baseline, the consumer-audit CI job, and the `@meshery/schemas` v1.1.0 release bump. |
| **[@miacycle](https://github.com/miacycle)** — Mia Grenell | **13** | Authored Phase 2 tail (final handler dual-accept + UI flips on meshery and meshery-cloud), Phase 2.K Sistent library alignment (re-exports repointed v1beta1 → canonical v1beta3/v1beta2; ~150 wire-key flips across CustomCard, CatalogCard, MetricsDisplay, PerformersSection, CatalogDesignTable, Workspaces, UsersTable; Sistent v0.17.0 → v0.19.1), the Phase 4.D validator pruning PR, the Phase 4.E impact report rewrites, the `mesheryctl-1231` master-CI unblocker, and the Sistent release workflow hygiene PRs (commit-back + npm-version idempotence + SSR hotfix). |
| **[@l5io](https://github.com/l5io)** (automated) | **3** | Automated cross-repo `@sistent/sistent` version-bump PRs across meshery, meshery-cloud, and meshery-extensions. Fired by Sistent's `notify-dependents.yml` workflow after each Sistent npm publish. |
| **[@PragalvaXFREZ](https://github.com/PragalvaXFREZ)** | **1** | Authored consumer-audit tooling improvements that shipped as Phase 0 input (better schema-driven logic, delta-from-previous-run summaries, new-schema-version detection in the audit sheet update). |

---

## If you're contributing new code

### Do

- Read the [Canonical Naming Directory](#canonical-naming-directory) above. It's the authoritative reference.
- Default your new JSON tags to **camelCase**.
- Put new DB-backed fields' `db:` tag in `x-oapi-codegen-extra-tags` on the OpenAPI side. The Go generator handles the rest.
- Name URL path parameters `{workspaceId}` etc. — not `{workspaceID}`, not `{workspace_id}`.
- Run `make validate-schemas` before opening a schemas PR — Rule 6, Rule 45, Rule 46, Rule 4 all block non-compliant changes.
- Consult the per-repo `AGENTS.md` files for repo-specific conventions — all four consumer repos adopted the identifier-naming mandate in Phase 4.C.

### Don't

- Don't re-case fields in-place on an already-published API version. That is a **partial-casing migration** and is forbidden by validator Rule 45. If the wire must change, introduce a new API version and migrate the resource consistently there.
- Don't copy an existing legacy schema as a starting template if you can help it — prefer canonical-version files (anything under `v1beta3/` or the canonical-target `v1beta2/` directories named in `docs/identifier-naming-migration.md §9.1`).
- Don't add a `DELETE` endpoint with a request body for bulk operations. REST clients and proxies silently strip `DELETE` bodies. Use `POST /api/{resources}/delete` (HTTP method cell #21 in the directory).
- Don't return HTTP `200` from a `POST` that exclusively creates a new resource — use `201 Created`.
- Don't allocate a `mesheryctl-NNNN` error code without confirming the number is free. The `MeshKit Error Codes Utility Runner` CI check will catch the collision, but it's cleaner to check `mesheryctl/internal/cli/.../error.go` files yourself first.

### If you find a violation

- Fix it in the same PR that touches the code if possible.
- If the violation predates this migration and living in retained-legacy code, it's **expected debt** — the `v1beta1/` and `v1beta2/` directories retain their historical wire form under `info.x-deprecated: true`. External consumers pinning legacy versions depend on those markers being stable.

---

## References

| Document | Purpose |
|---|---|
| [`docs/identifier-naming-migration.md`](identifier-naming-migration.md) | The canonical migration plan (authored Phase 0; current state field: **Complete**) |
| [`docs/identifier-naming-impact-report.md`](identifier-naming-impact-report.md) | Measurements-focused before/after impact report (governance artifact for Agent 4.E) |
| [`CLAUDE.md`](../CLAUDE.md) | Repo-specific conventions reference; Naming-conventions + Casing-rules-at-a-glance sections mirror this document |
| [`validation/`](../validation) | Rule implementations and advisory baseline |
| [`validation/baseline/`](../validation/baseline/) | Phase 0 baseline artifacts that anchored the migration |

---

*Document version: 2026-04-24. Source of truth for the naming contract is `AGENTS.md` / `CLAUDE.md` in `meshery/schemas`. If this document falls out of sync with the enforced contract, the enforced contract wins and this document should be corrected.*
