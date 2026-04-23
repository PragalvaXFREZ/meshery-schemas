# Identifier-Naming Migration — High-Level Plan: `layer5labs/meshery-extensions` (Kanvas)

> Handoff artifact for the downstream detailed-plan agent. Contains the known scope, concrete findings, and dependencies — not the final runbook. A subsequent agent will expand this into per-file / per-PR specifications.

## 1. Role in the identifier-naming migration

`layer5labs/meshery-extensions` houses Kanvas (the Meshery UI extension — React/TypeScript) plus a Go GraphQL plugin. It is a **consumer only**: it does not own any wire contract but consumes both `@meshery/schemas/mesheryApi` and `@meshery/schemas/cloudApi`, emits RTK requests to Meshery Server, and crosses an injection boundary (via `mesherySdk.ts`) to Meshery UI. Its job in the migration: stop the client-side case-flip transformations, align its request-body wrappers with what Meshery Server expects post-migration, resolve same-file casing contradictions, and displace its hand-rolled RTK endpoints where schemas equivalents exist.

## 2. The canonical contract — recap

- **Wire (JSON tags, URL query/path params, TS properties, OpenAPI properties):** camelCase, `Id` suffix.
- **DB column / `db:` tag:** snake_case (unchanged).
- **Go field names:** PascalCase with Go-idiomatic initialisms.
- **No same-resource partial migrations.** Version-bump if wire changes.

## 3. Dependencies on other repos

| Upstream | Blocks this repo's |
|---|---|
| `meshery/schemas` Phase 1 (governance + validator hardening + package publish) | All non-trivial identifier-naming work |
| `meshery/meshery` Phase 2.A (handler query-param alignment) | `getWorkspaceForCatalog` case-flip removal (no harm in leading; no harm in trailing) |
| `meshery/meshery` Phase 2.B (outbound URL alignment) | Server-side; unrelated here |
| `meshery/schemas` Phase 3.Design / Phase 3.Workspace | Any Kanvas endpoint that references those resources' types |

## 4. Known divergences (audit findings, concrete)

### 4.1 Kanvas frontend RTK (`meshmap/src/rtk-query/`)

**Client-side case-flip transformation (sends `orgID` to match Cloud legacy wire):**
- `meshmap/src/rtk-query/catalog.ts:110` — `getWorkspaceForCatalog` endpoint maps input `queryArg.orgId` → URL param `orgID: queryArg.orgId`. Must remove once backend canonical is `orgId`.
- `meshmap/src/rtk-query/catalog.ts:58-59` — `getPatternsPerUser` uses mixed `orgID` (caps) and `user_id` (snake) params in the same endpoint. Align to canonical.

**Mutation body wrapper-key drift** (snake wrappers with camelCase inner fields — partial-migration violation):
- `meshmap/src/rtk-query/designs.ts:308` — `uploadPatternBySourceType` body is `{ pattern_data: { name, patternFile }, save }`. Wrapper snake, inner camel. Should be `{ patternData: { ... } }` to align with the precedent set by PR #18856 (`SaveMesheryPattern` wire contract).
- `meshmap/src/rtk-query/designs.ts:336-342` — `patternFiletoCytoJson` body uses `pattern_data: { id, pattern_file, catalog_data, name }` — all snake inner. Should be all camel.
- `meshmap/src/rtk-query/designs.ts:188` — `k8sYamlToPattern` body wrapper `{ k8s_manifest: k8sManifest }` — wrapper snake, variable camel. Align to `{ k8sManifest }`.

**Locally-declared RTK endpoints that duplicate schemas-provided ones:**
- `meshmap/src/rtk-query/catalog.ts:90-100` — `getOrgsForCatalog` (wraps `/api/extensions/api/identity/orgs` with catalog-specific pagination). Evaluate against `@meshery/schemas/mesheryApi.getOrganizations`.
- `meshmap/src/rtk-query/catalog.ts:102-114` — `getWorkspaceForCatalog` (wraps `/api/extensions/api/workspaces` with orgId filter). Evaluate against `@meshery/schemas/mesheryApi.getWorkspaces` (once the workspace v1beta3 schema declares `orgIdQuery`).
- `meshmap/src/rtk-query/catalog.ts:48-62` — `getPatternsPerUser`. Evaluate against `@meshery/schemas/mesheryApi.getPatterns` plus user-id filter.
- `meshmap/src/rtk-query/designs.ts` — `saveDesign`, `getDesign`, `deleteDesign`, `deployPattern`, `undeployPattern`, `cloneDesign`, `uploadPatternBySourceType`, `uploadPatternJson`, `patternToCytoscape`, `k8sYamlToPattern`, `evaluateRelationships`, `meshmodelValidate`, `getPatterns`. Audit each against schemas.
- `meshmap/src/rtk-query/user.ts` — `getUserProfile`, `getUserProfileSummaryById`, `notifyMentionedUsers`, `getAccessToken`, `getPerformanceProfiles`, `getAllUsers`. Audit each against schemas.
- `meshmap/src/rtk-query/views.ts` — several. Audit.

### 4.2 Cross-boundary SDK (`meshmap/src/globals/mesherySdk.ts`)

**Same-file casing contradiction (event types ↔ dispatcher signatures):**
- Line 30 — event type `OpenViewScopedToDesign` uses `design_id` (snake).
- Line 38 — event type `OpenDesignInKanvas` uses `design_id` (snake).
- Line 46 — event type `OpenViewInKanvas` uses `view_id` (snake).
- Line 141-142 — `SetCurrentLoadedResourceInOrgWorkspaceSession` dispatcher signature uses `workspaceId`, `orgId` (camel).
- **Resolution:** Convert event-type fields to camelCase to match dispatcher signatures.

**Boundary mapping at response ingest:**
- `meshmap/src/modules/editor/modes/designer/designActor.ts:308-311` — reads `event.output.workspace_id` (snake — from HTTP response) and maps to `workspaceId` (camel — dispatcher). The snake input comes from the wire response body of `getDesign`. Once Phase 3.Design lands a camelCase wire, this mapping collapses to pass-through. Document the intentional mapping as a helper function in the interim.

### 4.3 Collaboration module

**User-ID field duality (ambiguous canonical):**
- `meshmap/src/modules/editor/collab/config.ts:32-40` — `getNameAndAvatar` interface declares **both** `user_id` and `id` fields on the user type. Canonical choice needed. Per AGENTS.md, `id` is the property default for identity.

### 4.4 Injected-prop shapes (from Meshery UI)

**`meshmap/src/App.tsx:85-89`** declares injected-prop type `SetCurrentLoadedResourceInOrgWorkspaceSession: (params: { id, workspaceId, orgId }) => void`. Consistent camelCase on Kanvas's side. The receiving end is Meshery UI's `WorkspaceModalContextProvider.tsx:51` `onLoadResource({ id, workspaceId, orgId })` — also camelCase. **Correct; no fix needed.** Document as a contract that must remain camelCase.

### 4.5 GraphQL plugin (`meshmap/graphql/`)

**Minimal divergence:**
- `graphql/schema/schema.graphql` — uses UPPERCASE composite identifiers: `designID`, `k8sclusterIDs`, `connectionID`.
- Under the canonical contract, GraphQL identifier fields should align with the canonical wire form (camelCase + `Id`), same as REST.
- `graphql/model/models_gen.go` — generated types; will follow schema changes.

**Candidate changes:**
- `designID` → `designId`, `connectionID` → `connectionId`, `k8sclusterIDs` → `k8sclusterIds`.
- Requires schema file edits, `make graphql` regeneration, and go.mod sync with `meshery/meshery` (per the project CLAUDE.md invariant).

## 5. Expected deliverables (rough)

| Deliverable | Phase | Est. PRs |
|---|---|---|
| `catalog.ts` case-flip removal (`getWorkspaceForCatalog`, `getPatternsPerUser`) | Phase 2 | 1 |
| `designs.ts` body wrapper unification (`pattern_data` → `patternData`, `k8s_manifest` → `k8sManifest`) | Phase 2 | 1 (coordinated with receiving handler verification) |
| `mesherySdk.ts` event-type camelCase alignment | Phase 2 | 1 |
| `collab/config.ts` user-ID duality resolution | Phase 2 | 1 |
| Kanvas RTK endpoint dedup vs. schemas (catalog, designs, user, views) | Phase 2–3 | 2–4 (by group) |
| GraphQL identifier alignment (`designID` → `designId`, etc.) | Phase 3 | 1 |
| Per-resource consumer updates when schemas publishes new versions | Phase 3 | ~5 (not all 22 resources touch Kanvas) |

## 6. Testing requirements (non-negotiable per agent)

- `cd meshmap && npm run build` clean before every commit.
- `npm test` green; Jest unit tests under `meshmap/src/__tests__/`.
- E2E tests under `tests/` (playwright) — at minimum the save-design flow and catalog-modal flow after `designs.ts` and `catalog.ts` changes.
- Lint: `make kanvas-lint` (ESLint + Prettier) clean.
- Kanvas dev server (`make kanvas`) loads against a live Meshery server; manually verify save, load, delete design flows.
- `cd graphql && make graphql-lint && make graphql` clean for any GraphQL changes.
- Meshery Server + Kanvas integration smoke: design save succeeds end-to-end after canonical-casing wrapper changes.

## 7. Documentation requirements (every PR)

- `CLAUDE.md` at repo root — updated per §14 of the detailed-plan draft.
- Kanvas Developer Guide under `docs/Kanvas Developer Guide/` — touched sections updated when the feature's wire shape changes.
- `meshmap/src/rtk-query/` inline docstrings referencing the canonical rule.
- `CHANGELOG.md` entry per PR.

## 8. Sequencing notes for the detailed-plan agent

- **`catalog.ts` case-flip removal can land standalone** — the receiving handler (`meshery-cloud/server/handlers/workspaces.go` accessed via Meshery Server's extension proxy) already tolerates `orgId` via `utils.QueryParam`. No server-side coordination required for this specific PR.
- **`designs.ts:308` wrapper-key change IS coordinated** — the receiving handler in `meshery/server/handlers/meshery_pattern_handler.go` must accept `patternData`. Verify by reading the handler's unmarshal target struct's JSON tags before merging; add a companion Meshery Server PR if the handler currently only accepts `pattern_data`.
- **RTK endpoint dedup is best tackled by group:** catalog first (small, isolated), then designs (large, coupled to save-design flow), then user (small), then views (small).
- **GraphQL identifier rename is Phase 3 territory** — it's a schema-driven change that requires the schemas repo to publish an updated GraphQL SDL first, then Kanvas regenerates.
- **`mesherySdk.ts` event-type alignment is Kanvas-internal** — no cross-repo coordination; can ship standalone as soon as Phase 1 schemas landing closes the governance doc.

## 9. Known open questions for the detailed-plan agent

1. For the `designs.ts:308` `pattern_data` → `patternData` wrapper change: does the meshery server `meshery_pattern_handler.go` unmarshal target currently read `pattern_data` from the request body, or already `patternData`? Concrete answer determines whether a companion meshery server PR is needed.
2. Which of the RTK endpoints in `catalog.ts` and `designs.ts` have genuine catalog-specific customizations vs. bare duplications of `@meshery/schemas/mesheryApi.*` hooks?
3. Is the GraphQL schema's UPPERCASE ID convention (`designID`, `connectionID`) authored here, or inherited from a schema SDL elsewhere? Source-of-truth question before any rename.
4. Does the Kanvas `kanvas/.meshery/provider/Layer5/capabilities.json` file declare endpoint paths that would be affected by Phase 3 URL changes? Audit.
5. What is Kanvas's minimum-supported Meshery Server version? RTK endpoint changes must be compatible with that version's handler contract.

## 10. Reference

- Full detailed plan draft (for reference; not operative): `schemas/docs/identifier-naming-migration.md`
- Source of truth: `schemas/AGENTS.md § Casing rules at a glance` (post Phase 1.A amendment)
- Kanvas Redux store setup (correctly uses schemas slices): `meshmap/src/redux/store.ts`
- Prior PRs: #18856 (patternData wrapper precedent — sets the `designs.ts` target shape), #18857 (merged k8s panic fix — unrelated), #18858 (JSON error body — Meshery Server side).
- Related Kanvas-specific automations: `meshery-extensions/CLAUDE.md § Claude Code Automation Setup`.
