# Option B Migration — High-Level Plan: `layer5io/meshery-cloud`

> Handoff artifact for the downstream detailed-plan agent. Contains the known scope, concrete findings, and dependencies — not the final runbook. A subsequent agent will expand this into per-file / per-PR specifications.

## 1. Role in the Option B migration

`layer5io/meshery-cloud` is the Go remote provider (Echo-based server) plus the Cloud Next.js UI. It is the receiving end of every Meshery-Server-proxied request and the largest surface of locally-declared RTK endpoints (~30+ in `ui/api/api.ts`). Its job in the migration: accept the Option B canonical query/body shapes on the wire, stop masking drift via `utils.QueryParam` fallbacks, align all 9 mapping-model JSON tags, and displace duplicated RTK endpoints with schemas-generated equivalents.

## 2. The Option B contract — recap

- **Wire (JSON tags, URL query/path params, TS properties, OpenAPI properties):** camelCase, `Id` suffix.
- **DB column / `db:` tag:** snake_case (unchanged).
- **Go field names:** PascalCase with Go-idiomatic initialisms.
- **No same-resource partial migrations.** Version-bump if wire changes.

## 3. Dependencies on other repos

| Upstream | Blocks this repo's |
|---|---|
| `meshery/schemas` Phase 1 (governance + validator hardening + package publish) | All non-trivial Option B work |
| `meshery/schemas` Phase 3 per-resource versioned schemas | Any handler / model / hook that imports a migrated-resource type |
| `meshery/meshery` Phase 2.A and 2.B (server query-param + outbound URL alignment) | Retirement of `utils.QueryParam` dual-accept (cannot retire until server emits single form) |

## 4. Known divergences (audit findings, concrete)

### 4.1 Go backend

**Query-parameter constant pinned to wrong form:**
- `server/utils/constants.go:138` — `QueryParamOrganizationID = "orgID"` (ALL CAPS). Should be `"orgId"`. Cascades to every consumer of the constant across middleware and handlers.

**Dual-accept pattern (masks drift):**
- `server/handlers/meshery_patterns.go:542` — `utils.QueryParam(q, "orgId", "orgID")`.
- `server/handlers/meshery_patterns.go:515` — `utils.QueryParam(q, "userId", "user_id")`.
- `server/handlers/meshery_views.go:123` — same `userId`/`user_id` dual-accept.
- `server/handlers/flow_emails.go:245` — `orgId`/`orgID` dual-accept.
- `server/handlers/middlewares_authz_scope.go:241` — `orgId`/`orgID` dual-accept.
- Additional sites to be located via `grep -rn 'QueryParam.*"[a-z]*Id".*"[a-z_]*id"' server/`.
- **Outlier with no fallback:** `server/handlers/meshery_filters.go:212` — reads only `q.Get("user_id")` (snake), silently drops `userId` if received. Either add fallback or align to Option B canonical.

**Mapping-model JSON tags — `json:"ID"` ALL CAPS across 9 files** (AGENTS.md requires lowercase `"id"`):
- `server/models/model_environment_connection_mapping.go:13` — `EnvironmentConnectionMapping.ID`.
- `server/models/model_workspaces_teams_mapping.go:13` — `WorkspacesTeamsMapping.ID`.
- `server/models/model_users_organizations_mapping.go:14` — `UsersOrganizationsMapping.ID`.
- `server/models/model_workspaces_designs_mapping.go:13` — `WorkspacesDesignsMapping.ID`.
- `server/models/model_resource_access_mapping.go:11` — `ResourceAccessMapping.ID`.
- `server/models/model_workspaces_environments_mapping.go:13` — `WorkspacesEnvironmentsMapping.ID`.
- `server/models/model_users_teams_mapping.go:14` — `UsersTeamsMapping.ID`.
- `server/models/model_workspaces_views_mapping.go:13` — `WorkspacesViewsMapping.ID`.
- `server/models/model_keychain_filter.go:10` — `KeychainFilter` — `roleID json:"roleID"` (ALL CAPS) with `db:"role_id"`.

**Go field / JSON tag / DB tag three-way splits:**
- `server/models/users.go:246` — `CatalogRequest.ContentID`: Go `ContentID`, `json:"contentId"`, `db:"content_id"`. **Actually correct under Option B** — confirm and mark as resolved rather than fix.
- `server/models/users.go:332` — `Messages.UserId`: field `UserId` with `json:"userId"`. No DB tag (in-memory). Consistent within the struct; **correct under Option B** (Go field PascalCase + camelCase JSON on non-DB-backed).
- `server/models/meshery_patterns.go:62` — `MesheryPattern.OrgID` with `json:"orgId" db:"-"`. Correct under Option B on JSON; `db:"-"` omits it (computed / joined field), so no DB conflict. **Verify and mark resolved.**

**Locally-declared request body duplicating schemas:**
- `server/handlers/meshery_patterns.go:140` — `MesheryPatternRequestBody` with a `TODO(local-struct migration)` comment referencing `github.com/meshery/schemas/models/v1beta2/design.MesheryPatternRequestBody`. Awaiting schemas issue #5063. When schemas resolves, displace.

**Other locally-declared types to evaluate for schemas displacement:**
- `server/models/meshery_patterns.go` — full `MesheryPattern` struct. Is Cloud's version divergent from Meshery Server's? Audit and unify.
- `server/models/meshery_filters.go` — `MesheryFilter`.
- `server/models/catalog_request.go` (if present) — `CatalogRequest`.

### 4.2 TypeScript frontend (`ui/`)

**~30+ hand-rolled RTK endpoints in `ui/api/api.ts`** — unknown proportion duplicate what `@meshery/schemas/cloudApi` provides. Requires systematic audit:
- `ui/api/api.ts:1255-1266` — `getWorkspaces`.
- `ui/api/api.ts:1269-1280` — `getWorkspaceById`.
- `ui/api/api.ts:~1200` — `getTeams`.
- `ui/api/api.ts:~90-96` — `getUsersForOrg`.
- Many more to be enumerated.

**Parameter case-flip transformations (frontend emits `orgID` to match Cloud's current wire form):**
- `ui/api/api.ts:1264,1272,1201` — input `orgId` → URL `orgID`. Must remove once backend `QueryParamOrganizationID` flips to `"orgId"`.
- `ui/api/api.ts:~96` — input `teamId` → URL `teamID`.

**Consumer-side casing consistency across Cloud UI components:**
- `ui/types/user.ts` — type field `user_id` (snake) while component props use `userId` (camel). Adapter layer required or normalize once.
- `ui/components/workspaces/*.js` — 9 endpoints consuming the hand-rolled Cloud UI RTK; migrations fan out across these consumers.

### 4.3 Server router surface (consumer-audit scope)

- `server/router/router.go` — parsed by `schemas/validation/consumer_echo.go` during consumer-audit. Every registered Echo route's path params and query params are validated. No local findings needed here beyond what Phase 1.H's CI job will surface.

## 5. Expected deliverables (rough)

| Deliverable | Phase | Est. PRs |
|---|---|---|
| `QueryParamOrganizationID` constant flip | Phase 2 | 1 |
| `utils.QueryParam` dual-accept removal (after one deprecation release) | Phase 2 (second release) | 1 |
| 9 mapping-model `json:"ID"` → `json:"id"` | Phase 2 | 1 (potentially with MarshalJSON shim if API-breaking) |
| `meshery_filters.go:212` user-id extraction alignment | Phase 2 | 1 (trivial) |
| Confirm-and-close for `CatalogRequest.ContentID`, `Messages.UserId`, `MesheryPattern.OrgID` | Phase 2 | 1 (docs-only or baseline update) |
| `ui/api/api.ts` endpoint audit + displacement | Phase 2–3 (spans both) | 3–5 (batched by component group) |
| Frontend case-flip removal | Phase 2 | 1 (coupled with backend constant flip) |
| `MesheryPatternRequestBody` displacement | Phase 3 | 1 (after schemas issue #5063 lands) |
| Per-resource migrations consuming new schemas versions | Phase 3 | ~22 |

## 6. Testing requirements (non-negotiable per agent)

- `go build ./...` clean before every commit.
- `go test ./...` green; new Pop ORM DAO tests for any model-tag change (round-trip Marshal/Unmarshal against fixture DB rows).
- Allure-Go test reports updated for handler changes.
- Cloud UI: `npm run build`, `npm test`, ESLint + Prettier clean.
- Jest regression tests already exist under `ui/__tests__/components/workspaces/*regression*.test.tsx` — extend these with Option B assertions.
- Integration test against staging Cloud endpoint after backend changes.

## 7. Documentation requirements (every PR)

- `AGENTS.md` / `CLAUDE.md` at repo root — reflect new conventions.
- `server/docs/` — API reference updates for any endpoint shape change.
- `ui/CONTRIBUTING.md` — new paragraph when endpoint-dedup policy changes.
- `CHANGELOG.md` entry per PR.
- Inline comments on public Go symbols referencing `schemas/AGENTS.md § Casing rules at a glance`.

## 8. Sequencing notes for the detailed-plan agent

- **Do not remove `utils.QueryParam` dual-accept (Phase 2.C PR 2) until all Meshery Server outbound URLs and all Cloud UI RTK URLs emit `orgId`.** Dual-accept is the compatibility layer during the migration; premature removal breaks in-flight requests.
- **`QueryParamOrganizationID` constant flip (Phase 2.C PR 1) is non-breaking** — backend accepts `orgId` via the fallback already; the constant flip just makes `orgId` the canonical form in error messages and logs.
- **9 mapping-model JSON tag fix is API-breaking** for any consumer reading the `ID` field by name. Survey consumers across all three downstream repos before merging; add a `MarshalJSON` shim emitting both `ID` and `id` for one release if needed.
- **`ui/api/api.ts` dedup is the largest Cloud-specific workstream.** Best tackled by component group (workspaces endpoints first, then events, then catalog, etc.) rather than as one mega-PR.
- **Cloud UI already has regression tests guarding orgID behavior** (`workspace-widget-orgid.regression.test.tsx`, etc.) — extend these to guard Option B canonicality as well.

## 9. Known open questions for the detailed-plan agent

1. Which of the 30+ `ui/api/api.ts` endpoints have genuine customizations that cannot be served by the schemas-generated `cloudApi`? Generate the exhaustive list via the consumer-audit TypeScript consumer (Phase 1.F).
2. For the 9 mapping-model `json:"ID"` fix: is the breaking-change blast radius small enough to ship without a `MarshalJSON` shim, or must we stage?
3. Does Cloud's `meshery_patterns.go MesheryPattern` diverge from Meshery Server's in ways that block a single schemas-sourced struct? Or is it field-identical modulo JSON tags?
4. Is there a Cloud-specific identifier (e.g., `PlanId`, `SubscriptionId`, `BadgeId`) whose current wire format conflicts with the schemas canonical? Enumerate before Phase 3.
5. What is Cloud's stance on supporting Meshery Server versions that predate the Option B migration? The `utils.QueryParam` removal window needs to be coordinated with the oldest supported Meshery Server release.

## 10. Reference

- Full detailed plan draft (for reference; not operative): `schemas/docs/identifier-naming-migration.md`
- Source of truth: `schemas/AGENTS.md § Casing rules at a glance` (post Phase 1.A amendment)
- Cloud consumer-audit input: `schemas/validation/consumer_echo.go` (parses `server/router/router.go`)
- Related prior PRs: #18856 (patternData wrapper), #18858 (JSON error body — sets precedent for wire-error-shape normalization).
