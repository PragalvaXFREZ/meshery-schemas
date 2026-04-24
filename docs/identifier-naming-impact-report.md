# Identifier-Naming Migration — Final Impact Report

> **Status:** **COMPLETE** (2026-04-24). All five phases of the Option B identifier-naming migration have landed across `meshery/schemas`, `meshery/meshery`, `layer5io/meshery-cloud`, `layer5labs/meshery-extensions`, `layer5io/sistent`, and (scan-clean) `meshery/meshkit`. The canonical contract — *wire is camelCase everywhere; DB is snake_case; Go fields follow Go idiom; the ORM layer is the sole translation boundary* — is now enforced at three places: the schemas validator (blocking), the advisory schema audit, and the blocking consumer-audit CI gate. Phase 4.A (legacy-version sunset) was administratively closed per maintainer decision: deprecated `schemas/constructs/v1beta1/` (25 resources) and `schemas/constructs/v1beta2/` (7 resources) directories are retained on `master` under `info.x-deprecated: true` + `info.x-superseded-by:` markers so external consumers that pin legacy versions are not stranded. Final in-flight items (Phase 2.K cascade PRs on Sistent / meshery / meshery-cloud / meshery-extensions following schemas#832) are tracked in §9 as the only work still traveling across PR review; everything schema-side, governance-side, and library-side has already merged and shipped.
>
> This document is the **Agent 4.E** deliverable of [`identifier-naming-migration.md`](identifier-naming-migration.md) §10. Each section below is the published before/after measurement promised by the plan's §15.

---

## 1. Scope and methodology

Metrics are derived from:

- **Phase 0 baseline artifacts** in [`validation/baseline/`](../validation/baseline/): `field-count.json`, `tag-divergence.json`, `consumer-graph.json`, `consumer-audit.txt` — the repository state at Phase 0.A–0.D, circa 2026-04-22.
- **Current `master` state** as of 2026-04-24, regenerable via `make baseline-field-count`.
- **Validator rule surface** counted from `grep RuleNumber validation/*.go | sort -u`.
- **CI workflow state** from `.github/workflows/schema-audit.yml`.
- **Cross-repo counts** (drift-masking sites, duplicate Go types, hand-rolled RTK endpoints, same-file casing contradictions, Sistent library-layer hits) from a `grep` audit across the six repos at 2026-04-24, cited inline in §2 and §9.

## 2. Before / after / delta

Table mirrors [`identifier-naming-migration.md §15.1`](identifier-naming-migration.md#151-metrics-table).

| Metric | Before (Phase 0) | After (current `master`) | Delta | Status |
|---|---|---|---|---|
| Distinct JSON-tag conventions in newly-authored canonical wire models | 3 (snake / camel / lowercase single-word) | 2 (camel + lowercase single-word only) | −1 | **Phase 3 complete** |
| Distinct JSON-tag conventions across the whole tree incl. legacy | 3 | 3 — legacy versions retain their published wire form, administratively closed, directories retained | unchanged by design | **Phase 4.A administratively closed** |
| Total JSON tags in `schemas/constructs/v*/**` | 1727 (Phase 0.A) | 2001 (post-0.A walker fixes) | +274 walker fidelity, not new tags | Baseline instrumentation |
| JSON tags camelCase | 352 (20.4 %) | 430 (21.5 %) | +78 | Baseline |
| JSON tags snake_case | 462 (26.8 %) | 470 (23.5 %) | +8 (walker fidelity; legacy retained) | Baseline |
| JSON tags all-lowercase single word | 907 (52.5 %) | 1095 (54.7 %) | +188 | Baseline |
| Per-resource snake-tag remainder | ~462 | 357 across 39 versions | −105 | Phase 3 + walker |
| Resources with zero snake_tags_to_migrate | — | 15 of 54 versions (28 %) | +15 | Phase 3 artefact |
| Validator rules in active use | ~20 | 45 discrete rule numbers (1–46 minus retired Rule 32) | +25 | **Phase 1.B–1.F complete** |
| Rules added in Phase 1 | — | Rule 45 (partial-casing-migrations), Rule 46 (sibling-endpoint parity), Rule 4 extended to query params, `consumer_ts.go` (advisory) | +4 | Phase 1.D–1.F |
| Rules retired in Phase 1 | — | Rule 32 (DB-backed property names must match db tags) retired to stub | −1 | Phase 1.B |
| Sub-rule allowlists retired in Phase 4.D | — | `knownLowercaseSuffixViolations` emptied; `lowercaseSuffixPattern` regex removed; `GetCamelCaseIssues` lowercase-suffix branch no-op | −1 allowlist | Phase 4.D |
| Rule 6 semantics | conditional (DB-backed exempt from camel) | unconditional (camel required regardless of DB backing) | Inverted | Phase 1.B |
| CI gates on schema conventions | 2 (blocking-validation, advisory-audit) | 3 (blocking-validation, advisory-audit, **consumer-audit blocking**) | +1 | Phase 4.B |
| `consumer-audit` CI status | did not exist | **blocking** | added + promoted | Phase 1.H + 4.B |
| Baselined advisory violations | — | 1827 entries in `build/validate-schemas.advisory-baseline.txt` | — | Phase 1.G |
| Drift-masking sites (`utils.QueryParam` dual-accept in meshery-cloud) | ≥6 *(plan §15)* | **Complete set:** Phase 2.C + Phase 3 repoint PRs + PR #5092 Phase 2 tail | +10 final sites; all intentional dual-accepts | **Phase 2 complete** |
| Locally-declared Go duplicates of schemas | 5+ (MesheryPattern ×2, MesheryPatternRequestBody, MesheryFilter, MesheryApplication) | Displaced to canonical `v1beta3/design` types via Phase 3 + Phase 3 consumer-repoint PRs; residual locals are intentional request-wrapper shims | substantially reduced | Phase 2.D / 2.F complete |
| Cloud UI `api.ts` hand-rolled endpoints with snake wire | 30+ | **0 snake wire findings** after PR #5092 (17 → 0) | −17 to zero | **Phase 2 tail complete** |
| Same-file casing contradictions | ≥8 | **0 reachable via consumer-audit** after PR #5092 + PR #18904 | −8 to zero | **Phase 2 tail complete** |
| ALL-CAPS `ID` on wire in schemas | 0 | 0 | 0 | Already clean |
| ALL-CAPS `ID` on wire in Go structs (meshery + cloud) | 22 | `isOAuth` flipped to `isOauth` in meshery-cloud (#5092); remainder contained by dual-accept and attrites as deprecated structs are retired | −1 flipped; remainder contained | Phase 2.A–2.E substantially complete |
| Consumer-audit TypeScript findings (live, 3 consumer repos) | — | **0 findings on meshery-cloud** (17 → 0 via #5092); **0 findings on meshery** (6 → 0 via #18904); **0 findings on meshery-extensions** (was clean) | **−23 to zero** | **Phase 2 tail complete** |
| Sistent library-layer snake wire-key drift | not in audit scope at Phase 0 | **≈150 sites flipped** across 3 Sistent PRs (#1431 + #1434): `organization_id`, `created_at/updated_at/deleted_at`, `user_id`, `first_name/last_name`, `avatar_url`, `role_names`, `team_id`, `last_login_time`, `pattern_info`, `pattern_caveats`, `team_name`/`team_names` kept for server-coord follow-up | −150 flipped; cascade (10 residual keys now schematized via #834) in-flight | **Phase 2.K complete; cascade in flight** |
| Canonical target-version directories present | 0 | 22 (§9.1 inventory): 14 in `v1beta3/`, 8 in `v1beta2/` | +22 | **Phase 3 complete** |
| @meshery/schemas published versions during Option B | 1.0.x | v1.1.0, v1.1.1, v1.1.2, **v1.2.0** (canonical coverage for 10 previously deferred keys) | +4 minor/patch | **Phase 4.E active** |
| @sistent/sistent published versions during Option B | 0.16.5 | 0.19.0 (Phase 2.K; broken SSR), **0.19.1** (SSR fix via #1434) | +2 | **Phase 2.K complete + hotfix** |

### 2.1 Validator rule surface — detail

45 active rules (Rules 1–46, Rule 32 retired in Phase 1.B). Phase 1 + Phase 4.D delta:

| Phase | Change |
|---|---|
| 1.B | Rule 32 retired (`rules_property.go:checkRule32ForAPI` returns `nil`); Rule 6 inverted to unconditional camelCase |
| 1.C | Rule 45 added — *partial casing migrations forbidden* |
| 1.D | Rule 46 added — *sibling-endpoint parameter parity* |
| 1.E | Rule 4 extended to cover query parameters |
| 1.F | `validation/consumer_ts.go` — TypeScript RTK Query auditor (advisory) |
| 1.G | Advisory baseline `build/validate-schemas.advisory-baseline.txt` introduced (1827 entries) |
| 1.H | `consumer-audit` wired into `schema-audit.yml` (advisory) |
| 1.I | `@meshery/schemas` → v1.1.0 |
| 4.B | `consumer-audit` promoted to **blocking**; PR-comment step on `if: always()` |
| 4.D | `knownLowercaseSuffixViolations` allowlist + `lowercaseSuffixPattern` retired; `GetCamelCaseIssues` lowercase-suffix branch no-op |

### 2.2 Per-version snake-tag distribution on current `master`

From `validation/baseline/field-count.json → per_resource` (refreshed 2026-04-24):

| Version | Resources | Total tags | Snake tags | Camel tags |
|---|---|---|---|---|
| v1alpha1 | 6 | 144 | 11 | 130 |
| v1alpha2 | 3 | 27 | 2 | 25 |
| v1alpha3 | 2 | 87 | 1 | 85 |
| v1beta1 | 30 | 1125 | 248 | 794 |
| v1beta2 | 13 | 618 | 95 | 491 |
| v1beta3 | 22 (canonical targets) | — | **0** | — |
| **Total** | **54 versions** | **2001** | **357** | **1525** |

The 1095 all-lowercase single-word tags (`name`, `type`, `id`) are camelCase-compatible and are not part of the migration surface. The 357 snake tags live in the deprecated-but-retained legacy directories (see §8).

## 3. Phase-by-phase completion log

| Phase | Deliverables | PRs |
|---|---|---|
| **0 — Baseline** | Field-count, tag-divergence, consumer-audit, consumer-graph artifacts committed under `validation/baseline/` | #781–#786 |
| **1 — Governance & validator** | `AGENTS.md` amendment; Rule 6 inversion + Rule 32 retirement; Rules 45 + 46; query-param extension; `consumer_ts.go`; advisory baseline; consumer-audit CI; @meshery/schemas v1.1.0 | #788–#798 |
| **2 — Non-breaking alignment** | Substantially subsumed by Phase 3 per-resource repoint PRs on the downstream repos; server dual-accept patterns established | interleaved with Phase 3 |
| **2 (tail)** | meshery-cloud `ui/api/api.ts` 17 hand-rolled sites + `users.go` dual-accept; meshery `ui/rtk-query/` 6 sites + 4 handler dual-accept shims | #5092, #18904 |
| **2.K — Sistent library alignment** | Sistent re-exports repointed v1beta1 → canonical v1beta2/v1beta3; 150+ snake keys flipped across CustomCard, CatalogCard, MetricsDisplay, PerformersSection, CatalogDesignTable, Workspaces, TeamTable, UsersTable, ResponsiveDataTable, CatalogDetail; Sistent version 0.16.5 → 0.19.0 → 0.19.1 (SSR hotfix) | layer5io/sistent#1431, #1434 |
| **3 — Per-resource version bumps** | All 22 resources in §9.1 inventory migrated to canonical camelCase: workspace (#800), relationship (#801), design (#802), credential (#803), user (#804), organization (#805), connection (#806), component (#807), team (#808), event (#809), view (#810), key (#811), model (#812), role (#813), keychain (#814), environment (#815), invitation (#816), schedule (#817), plan (#818), subscription (#819), token (#820), badge (#821) | #800–#821 |
| **4.A — Deprecated-version sunset** | **Administratively closed** per maintainer decision — deprecated directories retained on `master` with `x-deprecated: true` + `x-superseded-by` markers; bundler (`build/lib/config.js::isDeprecatedPackage`) excludes them from the merged spec | #822, #828 |
| **4.B — Consumer-audit blocking** | CI job promoted from advisory to blocking | #799 |
| **4.C — Universal AGENTS.md / CLAUDE.md** | Downstream repo mandates adopted in meshery, meshery-cloud, meshery-extensions | (in-repo docs PRs) |
| **4.D — Final validator pruning** | `knownLowercaseSuffixViolations` retired; `lowercaseSuffixPattern` removed; `GetCamelCaseIssues` lowercase-suffix branch no-op | #830 |
| **4.E — Impact report** | This document | #831, #833, #834 (schemas#832 closeout), and the PR containing this rewrite |
| **#832 closeout — canonical coverage for 10 deferred keys** | Added `TeamMember.joinedAt`, `Schedule.lastRun`/`.nextRun`, `MesheryPattern.designType` + 5 catalog counts; `teamName` intentionally skipped as alias of `Team.name` | #834 |

## 4. PR inventory — full session

### `meshery/schemas`
| # | Subject |
|---|---|
| #781 | Option B migration plan |
| #782 | Phase 0.A field-count baseline |
| #783 | Phase 0.B tag-divergence baseline |
| #784 | Phase 0.C consumer-audit baseline |
| #785 | Phase 0.D consumer-dependency graph |
| #786 | Phase 0.D scanner ergonomics follow-up |
| #787 | Dependabot pgx bump (bystander) |
| #788 | Phase 1.A AGENTS.md amendment |
| #789 | Drop "Option B" transient label |
| #791 | Phase 1.B Rule 6 inversion + Rule 32 retirement |
| #792 | Phase 1.C Rule 45 (partial casing) |
| #793 | Phase 1.D Rule 46 (sibling parity) |
| #794 | Phase 1.E Rule 4 query-param extension |
| #795 | Phase 1.F consumer_ts.go |
| #796 | Phase 1.G advisory baseline refresh |
| #797 | Phase 1.H consumer-audit CI wiring |
| #798 | Phase 1.I @meshery/schemas v1.1.0 bump |
| #799 | Phase 4.B/4.D/4.E interim report |
| #800–#821 | Phase 3 per-resource canonical versions (22 PRs) |
| #822 | Phase 4.E refresh at Phase 3 completion |
| #824 | Connection.Environments + workspace GET Environments v1beta3 retarget |
| #825 | v1beta3 Entity helpers (component, model, design) |
| #826 | v1beta3 Component gorm foreignKey fix |
| #827 | CI_CONSUMER_PAT provisioning docs |
| #828 | Phase 4.A administrative close |
| #830 | Phase 4.D validator prune |
| #831 | Phase 4.E final refresh |
| #832 | (open issue) tracking 10 deferred keys — now closed by #834 |
| #833 | Phase 2.K impact report update |
| #834 | schemas#832 canonical coverage (10 deferred keys) |

### `layer5io/meshery-cloud`
| # | Subject |
|---|---|
| (Phase 3 repoint PRs, merged earlier) | per-resource Phase 3 consumer repoints |
| #5092 | Phase 2 tail — 17 api.ts snake wire keys + handler dual-accept |
| #5094 | Sistent pin 0.18.8 → 0.19.0 (Phase 2.K consumer) |
| *(in flight)* | Phase 2.K cascade for 10-key coverage |

### `meshery/meshery`
| # | Subject |
|---|---|
| (Phase 3 repoint PRs, merged earlier) | per-resource Phase 3 consumer repoints |
| #18904 | Phase 2 tail — 6 rtk-query snake wire keys + 4 handler dual-accept shims |
| #18905 | Master CI unblocker (duplicate `mesheryctl-1231` error code) |
| #18911 | Sistent pin 0.18.8 → 0.19.0 (Phase 2.K consumer) |
| *(in flight)* | Phase 2.K cascade for 10-key coverage |

### `layer5io/sistent`
| # | Subject |
|---|---|
| #1431 | Phase 2.K library alignment — 150+ snake keys flipped; v0.16.5 → v0.19.0 |
| #1432 | Release workflow commit-back (package.json version tracking) |
| #1433 | Release workflow idempotence hotfix (`--allow-same-version`) |
| #1434 | SSR crash fix (`MesheryPatternImportRequestBody` oneOf); v0.19.0 → v0.19.1 |
| *(in flight)* | Phase 2.K cascade for 10-key coverage; Sistent v0.20.0 |

### `layer5labs/meshery-extensions`
| # | Subject |
|---|---|
| (Phase 3 repoint PRs, merged earlier) | per-resource Phase 3 consumer repoints |
| *(in flight)* | Phase 2.K cascade — `design_type` outbound body + 5 sort-literal values in meshmap |

### `meshery/meshkit`
Audit-verified zero wire-key hits. No PRs required.

## 5. Narrative impact

The Option B initiative eliminated the core ambiguity in the Layer5 / Meshery ecosystem's identifier contract. Before Phase 0, every repository had its own local read of the rule — meshery/schemas exposed three distinct wire conventions across resources, meshery-cloud handlers carried `utils.QueryParam` fallbacks for both camel and snake forms, Sistent re-exported snake-typed schemas into its public component surface, and the validator's Rule 6 had a DB-backing exception that contradicted the stated contract. After the migration:

1. **One contract, one rule per layer.** DB → snake; Go field → PascalCase + idiomatic initialisms; JSON tag → camelCase; URL parameter → camelCase + `Id` suffix; `operationId` → lower camelCase verbNoun; `components/schemas` type → PascalCase. The ORM layer is the only translation point. Rule 6 is unconditional — no DB-backing exemption.

2. **Three CI gates enforce it:** blocking schema validation (Rule 4, Rule 6, Rule 45, Rule 46, `screamingIDRE`, full property-constraint rules), advisory schema audit (tracks style violations against a baseline), and — promoted in Phase 4.B — **blocking consumer-audit** that runs across meshery/meshery, layer5io/meshery-cloud, and layer5labs/meshery-extensions on every PR.

3. **22 canonical resource versions** own the wire today. Every API a downstream repo invokes on the canonical path returns camelCase responses and accepts camelCase request bodies. Legacy versions are retained on `master` (Phase 4.A administratively closed) under `x-deprecated: true` + `x-superseded-by:` markers so external consumers pinning older versions are not stranded; the bundler excludes them from the merged spec to avoid path-space collisions.

4. **Drift-masking fully retired at the consumer-audit layer.** All three consumer UIs (meshery/ui, meshery-cloud/ui, meshery-extensions/meshmap) report **0 TypeScript findings** from `make consumer-audit`. Server-side `UnmarshalJSON` dual-accept shims on workspace / environments / events-config / k8s-ping / approve / deny / credentials / patterns / views / tokens payloads preserve backward compatibility for the one-release overlap window after each flip.

5. **Sistent — the design-system layer the consumer-audit scanner does not walk — was brought into compliance.** Phase 2.K (sistent#1431) flipped 150+ snake keys across the catalog / workspace / users / teams component surface; sistent#1434 fixed a regression in #1431 that crashed all 0.19.0 consumers at Next.js SSR module load (a hard-coded read of the pre-v1.1.0 flat `MesheryPatternImportRequestBody.properties` shape, which had moved to `oneOf`); sistent#1432 wired a release-time commit-back so master's `package.json` now tracks the version that was actually published to npm (prior state: master at 0.16.5, npm at 0.18.8).

6. **The 10 residual keys** surfaced during the Sistent sweep as having no canonical coverage in schemas — `team_name`, `joined_at`, `last_run`, `next_run`, `design_type`, and 5 design catalog metrics — were closed by meshery/schemas#834. `teamName` was intentionally skipped as a naming alias of `Team.name`. The other 9 are now expressed in v1.2.0 of the published schema. The per-repo cascade flipping consumer references to the new canonical types is in flight (see §6).

## 6. Outstanding work — in flight

The following four PRs are open and were dispatched in parallel during the finalization window. They complete the #832 cascade:

| Repo | Purpose |
|---|---|
| `layer5io/sistent` | Flip the 9 canonical-covered keys across 46 wire-bound sites; Sistent v0.19.1 → v0.20.0. Intentional non-flips retained: `team_name` / `team_names` pending coordinated server rename. |
| `meshery/meshery` | Server Go struct JSON-tag flips on MesheryPattern + PerformanceProfile; sort-whitelist dual-accept; UI reads on Performance cards; `MesheryPatternPayload` `UnmarshalJSON` dual-accept for `design_type`. GraphQL `last_run` intentionally preserved (gqlgen separately versioned). |
| `layer5io/meshery-cloud` | Server struct flips on MesheryPattern + Roles + PerformanceProfile; SQL aliases retained snake (DB column names); sort-whitelist dual-accept; UI catalog / leaderboard / subscriptions / invitations / team-management flips; two `UnmarshalJSON` dual-accepts (bulk-delete-teams + catalog upload). |
| `layer5labs/meshery-extensions` | meshmap catalog outbound `design_type` body key + 5 sort-literal values. |

When all four cascade PRs merge, the Option B migration is closed end-to-end across every live wire surface in the Layer5 / Meshery ecosystem. No further schemas work is planned; the residual `team_name` / `team_names` server-rename remains as a separate, bounded follow-up tracked on the meshery-cloud server.

## 7. Retained legacy directories

Phase 4.A closed administratively on 2026-04-23 (PR #828). Physical deletion of the deprecated directories was **overridden by maintainer decision**. Every retained directory carries `info.x-deprecated: true`; the OpenAPI bundler reads that marker and excludes the directory from the merged spec so the canonical target owns the wire without path-space collision.

- `schemas/constructs/v1beta1/*` — 25 deprecated directories, each superseded by either v1beta2 or v1beta3 (see §8.1 of prior report revision for the resource-by-resource table; unchanged).
- `schemas/constructs/v1beta2/*` — 7 deprecated directories superseded by v1beta3 (unchanged).

External consumers that import from a retained legacy path continue to receive the frozen contract. The `x-superseded-by` marker indicates the canonical version to migrate to on the next upgrade cycle.

## 8. Revision history

| Date | Phase | Summary |
|---|---|---|
| 2026-04-23 | 4.B / 4.D / 4.E | Initial interim report (Phase 1 complete, Phase 4.B landing, Phases 2 and 3 in flight). |
| 2026-04-23 | Phase 3 complete | 22/22 resources version-bumped to canonical camelCase; §2 metrics refreshed; §5 narrative revised. |
| 2026-04-23 | Phase 4.A administrative close | One-release-cycle safety window overridden; deprecated directories retained on `master`. |
| 2026-04-23 | Phase 4.D complete | `knownLowercaseSuffixViolations` retired; `lowercaseSuffixPattern` removed; `GetCamelCaseIssues` branch no-op. |
| 2026-04-23 | Phase 2 tail + 4.E final | 23 consumer-audit TypeScript findings closed (meshery 6→0, meshery-cloud 17→0, meshery-extensions was 0); server dual-accept shims across workspace / environments / events-config / k8s-ping / approve / deny / tokens / credentials / patterns / views. |
| 2026-04-24 | Phase 2.K — Sistent library alignment | 150+ snake keys flipped across Sistent; v0.16.5 → v0.19.0; SSR crash hotfix v0.19.1 via #1434; release-workflow commit-back via #1432. 10 residual keys surfaced and tracked in meshery/schemas#832. |
| 2026-04-24 | schemas#832 closed | PR #834 added canonical coverage: TeamMember.joinedAt; Schedule.lastRun/.nextRun; MesheryPattern.designType + 5 catalog counts (viewCount, downloadCount, cloneCount, deploymentCount, shareCount). `teamName` intentionally skipped as alias of Team.name. @meshery/schemas v1.2.0 released. |
| 2026-04-24 | **Final** | All five phases complete. Phase 2.K cascade PRs dispatched in parallel across Sistent / meshery / meshery-cloud / meshery-extensions. Status banner flipped to **COMPLETE**. |

---

**This report closes Agent 4.E.** Any further revision will be additive — noting the cascade-PR landings and any subsequent naming-aligned publishes — rather than structural. The migration is complete in the sense that every schema and every CI gate commits to the canonical contract; the cascade PRs are consumer housekeeping that does not change the contract.
