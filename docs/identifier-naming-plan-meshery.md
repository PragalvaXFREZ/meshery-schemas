# Option B Migration — High-Level Plan: `meshery/meshery`

> Handoff artifact for the downstream detailed-plan agent. Contains the known scope, concrete findings, and dependencies — not the final runbook. A subsequent agent will expand this into per-file / per-PR specifications.

## 1. Role in the Option B migration

`meshery/meshery` is the Go server + Meshery UI (Next.js) + `mesheryctl` CLI. It consumes `@meshery/schemas` as the source of truth and hosts the routing logic that the schemas consumer-audit validates against. Its job in the migration: align every identifier it emits (outbound URLs, JSON bodies), every identifier it reads (query params, response shapes), and every hook it exposes to Kanvas with the Option B canonical form.

## 2. The Option B contract — recap

- **Wire (JSON tags, URL query/path params, TS properties, OpenAPI properties):** camelCase, `Id` suffix (lowercase `d`).
- **DB column / `db:` tag:** snake_case (unchanged; the sole remaining translation boundary).
- **Go field names:** PascalCase with Go-idiomatic initialisms (`UserID`, `OrgID`, `WorkspaceID`).
- **No same-resource partial migrations.** If a resource's wire format changes, bump its API version.

## 3. Dependencies on other repos

| Upstream | Blocks this repo's |
|---|---|
| `meshery/schemas` Phase 1 (governance + validator hardening + package publish) | All non-trivial Option B work in this repo |
| `meshery/schemas` Phase 3 per-resource versioned schemas | Any handler / model / hook that imports a migrated-resource type |
| `layer5io/meshery-cloud` Phase 2.C (constant + dual-accept retirement) | Timing of outbound URL alignment — can land before or after; no hard ordering |

## 4. Known divergences (audit findings, concrete)

### 4.1 Go backend

**Query-parameter extraction drift (sibling endpoints, different keys):**
- `server/handlers/workspace_handlers.go:24,46` — reads `q.Get("orgID")` (ALL CAPS). Should be `q.Get("orgId")`. *Non-breaking if companion UI fix lands.*
- `server/handlers/meshery_pattern_handler.go:557` — `q["orgID"]`. Same fix.
- `server/handlers/environments_handlers.go:24,45` — already reads `q.Get("orgId")`. **Correct under Option B.**

**Outbound URL construction (Meshery Server → Meshery Cloud):**
- `server/models/remote_provider.go:5174` — `q.Set("orgID", orgID)` in `GetEnvironments`. Change to `"orgId"`.
- `server/models/remote_provider.go:5215` — `q.Set("orgID", orgID)` in `GetWorkspaces`. Change to `"orgId"`.
- `server/models/remote_provider.go:5607` — `q.Set("orgID", orgID)` in `GetCatalogMesheryPatterns`.
- `server/models/remote_provider.go:5671` — `q.Set("orgID", orgID)` in `GetUsersKeys`.
- Companion change: Meshery Cloud must already accept `orgId` (it does via its `utils.QueryParam` fallback today; becomes canonical after Cloud's Phase 2.C).

**JSON tag drift on locally-declared Go models (partial-casing-migration violations):**
- `server/models/meshery_pattern.go:93,109,110` — `MesheryPattern` struct mixes three JSON-tag conventions on three DB-backed ID fields: `json:"user_id"`, `json:"workspace_id"`, `json:"orgId"`. Violates AGENTS.md "Partial casing migrations forbidden". Two resolution paths:
  - **Phase 2 interim:** Normalize back to all snake (revert `orgId` → `org_id`) so the struct is consistent with DB-backing — non-breaking, internal consistency restored.
  - **Phase 3 final:** Displace the local struct with `github.com/meshery/schemas/models/v1beta3/design.MesheryPattern` once the v1beta3 design schema lands under Option B.
- `server/models/meshery_filter.go:19,36` — `MesheryFilter.UserID json:"user_id"`. Candidate for schemas export + displacement.
- `server/models/meshery_application.go:42` — same class.
- `server/models/k8s_context.go:37-38` — `MesheryInstanceID json:"meshery_instance_id"`, `KubernetesServerID json:"kubernetes_server_id"`. Meshery-local (not in schemas); wire form needs Option B camelCase (`mesheryInstanceId`, `kubernetesServerId`) once a `v1beta*` K8sContext schema is authored.

**`mesheryctl` CLI (not yet surveyed — gap):**
- Subsequent audit pass needed. Expected divergences: command-line flag casing (`--org-id` vs `--orgId`), config-file key casing.

### 4.2 TypeScript frontend (`ui/`)

**Same-file casing contradictions:**
- `ui/rtk-query/user.ts:87` — `orgID: selectedOrganization?.id` (ALL CAPS).
- `ui/rtk-query/user.ts:107` — `orgId: queryArg?.orgId` (camelCase). **Same file; two conventions.**
- `ui/rtk-query/design.ts:44` — `user_id: queryArg.user_id` (snake).
- `ui/rtk-query/design.ts:49` — `orgID: queryArg.orgID` (ALL CAPS). **Same file; two conventions.**

**Thin wrappers that exist only to alias parameter names:**
- `ui/rtk-query/environments.ts:11-21` — wraps `useSchemasGetEnvironmentsQuery` solely to pass `orgId` through. Eliminate; have callers import the generated hook directly.
- `ui/rtk-query/workspace.ts:19-80` — custom `queryFn` exists because the schemas-generated `getWorkspaces` was missing the `orgId` param. Once schemas Phase 3.Workspace authors v1beta3 with the `orgIdQuery` ref, retire the custom queryFn.

**Hand-rolled endpoints to evaluate for schemas displacement:**
- `ui/rtk-query/workspace.ts` — custom `getWorkspaces` (expand-info multi-dispatch).
- `ui/rtk-query/user.ts` — `useGetOrganizations` and others using custom `queryFn` with URL transformation.

## 5. Expected deliverables (rough)

| Deliverable | Phase | Est. PRs |
|---|---|---|
| `q.Get("orgID")` → `q.Get("orgId")` in handlers | Phase 2 | 1 |
| `q.Set("orgID", ...)` → `q.Set("orgId", ...)` in `remote_provider.go` | Phase 2 | 1 |
| Meshery UI RTK hook unification (user.ts, design.ts, environments.ts) | Phase 2 | 1–2 |
| `MesheryPattern` struct JSON tag normalization (Phase 2 interim) | Phase 2 | 1 |
| `mesheryctl` CLI flag / config audit + fix | Phase 2 | 1 |
| Per-resource migrations consuming new schemas versions | Phase 3 | ~22 (one per resource, coordinated with schemas) |
| Local Go type displacements (`MesheryPattern`, `MesheryFilter`, `MesheryApplication` → schemas imports) | Phase 3 | 3+ |
| `K8sContext` JSON tag migration (new schemas type or in-place update) | Phase 3 | 1 |

## 6. Testing requirements (non-negotiable per agent)

- `go build ./...` clean before every commit.
- `go test ./...` green for touched packages; new tests for any new behavior.
- Handler-level tests asserting new query-param names work and old names fail (post-deprecation).
- Serialization round-trip tests (`json.Marshal`/`Unmarshal`) for any struct JSON tag change.
- E2E smoke in `tests/` (playwright) for UI changes.
- `make kanvas-lint` / ESLint / Prettier green on UI changes.

## 7. Documentation requirements (every PR)

- `CLAUDE.md` at repo root — reflect new conventions as landed.
- `docs/` subpages that reference the changed endpoint, hook, or model.
- API reference (server swagger / generated docs) updated.
- `CHANGELOG.md` entry per PR.
- Comments on public Go symbols explaining rationale, referencing `schemas/AGENTS.md § Casing rules at a glance`.

## 8. Sequencing notes for the detailed-plan agent

- **Phase 2 non-breaking fixes can all ship independently** — no inter-PR ordering required. They should land BEFORE Phase 3 starts to reduce noise in per-resource diffs.
- **Phase 3 per-resource migrations wait for schemas:** each resource's Phase 3 in this repo cannot start until `meshery/schemas` has authored and published the new version of that resource.
- **Workspace is priority-1 for Phase 3** because of the known `orgId`-drop production bug (PR #18858 context). This repo's side of that migration is small once schemas publishes v1beta3.
- **`mesheryctl` audit has not been done yet** — a pre-Phase-2 audit agent is needed to inventory CLI flag and config-file identifier casing.

## 9. Known open questions for the detailed-plan agent

1. Does the `mesheryctl` config file format need its own versioning for an Option B migration, or can it migrate in place?
2. Should `K8sContext` be exported to `meshery/schemas` for reuse (it currently is not), and if so, under which version?
3. Which resources in the 22-resource inventory have meshery-server-specific shape extensions (e.g., pagination envelope on `/api/pattern`) that need schemas-side updates before this repo can consume them?
4. Are there generated GraphQL types in `server/internal/graphql/generated/generated.go` that reference snake-case identifiers inherited from a GraphQL SDL file? That file was mentioned in prior audits but not traced end-to-end.
5. Deprecation strategy for handlers that currently dual-accept casing: flip to single-accept in Phase 2, or hold until Phase 3 per-resource?

## 10. Reference

- Full detailed plan draft (for reference; not the operative document): `schemas/docs/identifier-naming-migration.md`
- Source of truth: `schemas/AGENTS.md § Casing rules at a glance` (post Phase 1.A amendment)
- Prior audit sub-reports in this session's transcript (meshery-cloud, meshery, meshery-extensions audits)
- PR #18856 (patternData wrapper precedent), PR #18857 (merged k8s panic fix — unrelated), PR #18858 (JSON error body — Option B-adjacent)
