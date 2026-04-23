# Identifier-Naming Migration — Interim Impact Report

> **Status:** INTERIM snapshot. Phase 3 (per-resource versioned migrations) is still in flight. "After" figures for metrics that depend on per-resource migrations are projected, not measured; each such cell is explicitly labelled. "After" figures for metrics that depend only on Phase 1 (governance / validator / CI) and Phase 4.B (consumer-audit blocking) are measured from the current `master`.
>
> This document is the deliverable for **Agent 4.E** of [`docs/identifier-naming-migration.md`](identifier-naming-migration.md) §10. It is intended to be refreshed once Phase 3 completes (all 22 resources version-bumped) and again after Phase 4.A (deprecated-version sunset). Each refresh should bump the revision-history entry at the bottom.

## 1. Scope and methodology

The metrics below are derived from:

- **Phase 0 baseline artifacts** in [`validation/baseline/`](../validation/baseline/): `field-count.json`, `tag-divergence.json`, `consumer-graph.json`, `consumer-audit.txt`. These capture the repository state at the start of the migration (Phase 0.A–0.D, circa 2026-04-22).
- **Current `master` state**, regenerated on demand via `make baseline-field-count`. When the current artifact is identical to the committed baseline (i.e., Phase 3 has not yet landed any resource migrations), we note it and do not re-commit.
- **Validator rule surface** counted directly from `grep RuleNumber validation/*.go | sort -u`.
- **CI workflow state** from `.github/workflows/schema-audit.yml`.
- **Cross-repo counts** (drift-masking sites, duplicate types, hand-rolled RTK endpoints, same-file contradictions) are carried forward from the migration plan's Phase 0 observations. These cannot be re-measured inside this repository; they will be refreshed by subsequent cross-repo audits as the Phase 2 and Phase 3 downstream PRs land.

## 2. Before / after / delta

The table below mirrors the structure of [`identifier-naming-migration.md §15.1`](identifier-naming-migration.md#151-metrics-table) and populates the "after (current)" column with measured values. Italic entries are projections where no direct measurement is yet possible.

| Metric | Before (Phase 0) | After (current `master`) | Delta | Status |
|---|---|---|---|---|
| Distinct JSON-tag conventions in schemas wire models | 3 (snake, camel, lowercase single-word) | 3 (unchanged — Phase 3 not yet migrating) | 0 | Pending Phase 3 |
| Total JSON tags in `schemas/constructs/v*/**` | 1727 (Phase 0.A commit) | 2001 (post-0.A walker fixes 9f2e11af / e8ca7594) | +274 walker fidelity, not new tags | Baseline instrumentation |
| JSON tags camelCase | 352 (20.4 %) | 430 (21.5 %) | +78 | Baseline instrumentation |
| JSON tags snake_case | 462 (26.8 %) | 470 (23.5 %) | +8 | Baseline instrumentation |
| JSON tags all-lowercase single word | 907 (52.5 %) | 1095 (54.7 %) | +188 | Baseline instrumentation |
| Per-resource snake-tag remainder (sum across v* versions) | ~462 | 357 snake_tags_to_migrate across 39 versions | −105 | Artefact of fuller walker (not Phase 3 work) |
| Resources with zero snake_tags_to_migrate | — | 15 of 54 versions (28 %) | +15 | Baseline artefact |
| Validator rules total (encoded) | ~20 | 45 discrete rule numbers in use (1–46, Rule 32 retired) | +25 | Phase 1.B–1.F complete |
| Rules added in Phase 1 | — | Rule 45 (partial-casing-migrations), Rule 46 (sibling-endpoint parity), Rule 4 extended to query params, consumer_ts.go (advisory) | +4 | Phase 1.D–1.F complete |
| Rules retired in Phase 1 | — | Rule 32 (DB-backed property names must match db tags) retired to stub | −1 | Phase 1.B complete |
| Rule 6 semantics | conditional: DB-backed fields exempt from camelCase | unconditional: camelCase required regardless of DB backing | Inverted | Phase 1.B complete |
| CI gates on schema conventions | 2 (blocking-validation, advisory-audit) | 3 (blocking-validation, advisory-audit, **consumer-audit blocking**) | +1 | Phase 4.B complete |
| `consumer-audit` CI status | did not exist | **blocking** | added + promoted | Phase 1.H + 4.B complete |
| Baselined advisory violations | — (baseline introduced Phase 1.G) | 1773 entries in `build/validate-schemas.advisory-baseline.txt` | — | Phase 1.G complete |
| Drift-masking sites (`utils.QueryParam` dual-accept in `meshery-cloud`) | ≥6 sites *(plan §15)* | *unchanged, Phase 2.C not yet complete* | *pending* | Phase 2.C |
| Locally-declared Go duplicates of schemas | 5+ (MesheryPattern ×2, MesheryPatternRequestBody, MesheryFilter, MesheryApplication) *(plan §15)* | *unchanged, Phase 2.D/2.F not yet complete* | *pending* | Phase 2.D / 2.F |
| Cloud UI `api.ts` hand-rolled endpoints | 30+ *(plan §15)* | *unchanged, Phase 2.F not yet complete* | *pending* | Phase 2.F |
| Same-file casing contradictions | ≥8 *(plan §15)* | *unchanged, Phase 2.G/2.H/2.I not yet complete* | *pending* | Phase 2.G/2.H/2.I |
| ALL-CAPS `ID` on wire (schemas repo) | 0 `screaming` json tags in schemas baseline | 0 (unchanged — schemas was already clean) | 0 | Already clean |
| ALL-CAPS `ID` on wire (Go structs, meshery + cloud) | 22 `SCREAMING` json tags *(tag-divergence.json)* | *unchanged, Phase 2.A–2.E not yet complete* | *pending* | Phase 2.A–2.E |
| API versions with snake-case-on-wire JSON tags | ~20 (v1alpha1, v1alpha2, v1alpha3, v1beta1, v1beta2 resources) | 39 version×resource pairs still carry ≥1 snake tag | unchanged | Phase 3 |

### 2.1 Validator rule count — detail

Rules 1–46 are in active use, minus retired Rule 32 ⇒ **45 discrete rules**. Phase 1 delta:

| Phase | Rule change |
|---|---|
| 1.B | Rule 32 retired (`rules_property.go:checkRule32ForAPI` returns `nil`) — DB-backing no longer exempts from camelCase |
| 1.B | Rule 6 inverted to unconditional camelCase on wire (`rules_naming.go:checkRule6ForAPI`) |
| 1.C | Rule 45 added — "Partial casing migrations forbidden" (`rules_naming.go:checkRule45ForAPI`) |
| 1.D | Rule 46 added — "Sibling-endpoint parameter parity" (`rules_parity.go`) |
| 1.E | Rule 4 extended to cover query parameters (previously path-params only) |
| 1.F | `validation/consumer_ts.go` added — TypeScript RTK Query auditor (regex-based) |
| 1.G | Advisory baseline file `build/validate-schemas.advisory-baseline.txt` introduced with 1773 pre-canonical entries |
| 1.H | `consumer-audit` CI job wired into `schema-audit.yml` (advisory) |
| 1.I | `@meshery/schemas` version bumped (v1.1.0) |
| 4.B | `consumer-audit` CI job promoted to blocking; PR-comment step moved to `if: always()` |

### 2.2 Per-version snake-tag distribution (current)

From `validation/baseline/field-count.json` → `per_resource`:

| Version | Resources | Total tags | Snake tags | Camel tags |
|---|---|---|---|---|
| v1alpha1 | 6 | 144 | 11 | 130 |
| v1alpha2 | 3 | 27 | 2 | 25 |
| v1alpha3 | 2 | 87 | 1 | 85 |
| v1beta1 | 30 | 1125 | 248 | 794 |
| v1beta2 | 13 | 618 | 95 | 491 |
| **Total** | **54** | **2001** | **357** | **1525** |

The 1095 `lowercase` tags (single-word identifiers like `name`, `type`, `id`) are already camelCase-compatible and are not part of the migration surface.

## 3. Phase 3 progress tracker

Phase 3 migrates each of the 22 resources to a new canonical-casing API version (typically `v1beta3` for resources that already have a `v1beta2`, or their "first bump" otherwise). A resource is "complete" when: (a) the new-version directory exists under `schemas/constructs/`, (b) its validators pass, (c) every downstream consumer has migrated, and (d) the previous version is annotated deprecated per the template in `docs/identifier-naming-migration.md` §9.2.

Consumer-count column is the `complexity_score` from `validation/baseline/consumer-graph.json`.

| Priority | Resource | Current version(s) | Target version | Consumer count | Status |
|---|---|---|---|---|---|
| 1 | workspace | v1beta1 | v1beta3 | 11 | [ ] not started |
| 2 | environment | v1beta1 | v1beta3 | 15 | [ ] not started |
| 3 | organization | v1beta1 | v1beta2 (first bump) | 4 | [ ] not started |
| 4 | user | v1beta1 | v1beta2 (first bump) | 7 | [ ] not started |
| 5 | design / pattern | v1beta1, v1beta2 | v1beta3 | 31 (pattern) + 2 (design) | [ ] not started |
| 6 | connection | v1beta1, v1beta2 | v1beta3 | 27 | [ ] not started |
| 7 | team | v1beta1 | v1beta2 (first bump) | — | [ ] not started |
| 8 | role | v1beta1 | v1beta2 (first bump) | — | [ ] not started |
| 9 | credential | v1beta1 | v1beta2 (first bump) | 1 | [ ] not started |
| 10 | event | v1beta1, v1beta2 | v1beta3 | 1 | [ ] not started |
| 11 | view | v1beta1 | v1beta2 (first bump) | 1 | [ ] not started |
| 12 | key | v1beta1 | v1beta2 (first bump) | 2 | [ ] not started |
| 13 | keychain | v1beta1 | v1beta2 (first bump) | 1 | [ ] not started |
| 14 | invitation | v1beta1, v1beta2 | v1beta3 | 5 | [ ] not started |
| 15 | plan | v1beta2 | v1beta3 | 8 | [ ] not started |
| 16 | subscription | v1beta1, v1beta2 | v1beta3 | 6 | [ ] not started |
| 17 | token | v1beta1, v1beta2 | v1beta3 | — | [ ] not started |
| 18 | badge | v1beta1 | v1beta2 (first bump) | 5 | [ ] not started |
| 19 | schedule | v1beta1 | v1beta2 (first bump) | 1 | [ ] not started |
| 20 | model | v1beta1 | v1beta2 (first bump) | 13 | [ ] not started |
| 21 | component | v1beta2 | v1beta3 | 35 | [ ] not started |
| 22 | relationship | v1beta1, v1beta2, v1alpha3 | v1beta3 | 21 | [ ] not started |

**Overall progress: 0 / 22 resources complete.** No canonical-casing target-version directories exist in `schemas/constructs/` yet (no `v1beta3/` tree).

## 4. What actually changed in the schemas repo (Phases 1–4)

### 4.1 Governance (Phase 1.A)

- `AGENTS.md` rewritten around "wire is camelCase everywhere; DB is snake_case; Go fields follow Go idiom; the ORM layer is the sole translation boundary."
- `AGENTS.md § Identifier-naming migration (in flight)` added, linking to the master plan.
- §Casing rules at a glance table rewritten to show the canonical target state; legacy exemptions called out explicitly as deprecation-path entries.

### 4.2 Validator (Phase 1.B–1.F)

Changes described in §2.1 above. Net effect: `make validate-schemas` (blocking) enforces the canonical contract on every new field authored in a canonical-casing API version. Legacy fields in already-published versions are routed through `--style-debt` advisory severity and suppressed by the baseline until their resource's Phase 3 version bump lands. `make audit-schemas-full` exits 0 on the current `master`.

### 4.3 Consumer audit (Phase 1.F, 1.H, 4.B)

- `validation/consumer_ts.go` added — scans RTK Query hooks across meshery/meshery, meshery-cloud, meshery-extensions, classifies drift into `case-flip`, `snake-case-wrapper`, `snake-case-param`.
- `make consumer-audit` integrated into CI via `.github/workflows/schema-audit.yml`.
- **Phase 4.B (this PR):** consumer-audit job promoted from advisory (`set +e` + trailing `exit 0`) to blocking. The PR-comment step is pinned to `if: always()` so reviewers still see the divergence summary on red builds.

### 4.4 Package release (Phase 1.I)

- `@meshery/schemas` version bumped to `v1.1.0`, signalling the new validator surface and giving downstream repos a stable pin point to coordinate Phase 2 and Phase 3 consumer PRs against.

### 4.5 Validator rule pruning (Phase 4.D — deferred)

Phase 4.D's pruning of `knownLowercaseSuffixViolations` in `validation/casing.go` is explicitly deferred. Individual entries in that map become dead weight only once the last resource that still carries the named property has been Phase 3-migrated **and** its legacy version sunset by Phase 4.A. Because Phase 3 is in flight, a defensive documentation comment has been added to `casing.go` (this PR) recording the deferral and the per-entry prune cadence; no entries are removed. `dbMirroredFields` was re-verified — its docstring (updated in Phase 1.B) still accurately reflects the post-inversion semantics.

## 5. Narrative impact

The changes to date are **validator and CI changes only** — the wire format has not yet moved for any resource (no `v1beta3` directory exists). What the schemas repo has achieved so far:

1. **The canonical contract is now documentable in one sentence** and present as MUST / MUST NOT text in `AGENTS.md`.
2. **The validator and the doc now agree.** Before Phase 1.B, Rule 32 enforced "DB-backed ⇒ snake on wire" in direct contradiction to the doc's camelCase-on-wire direction. A reviewer citing the doc would be fighting the validator. That conflict is gone.
3. **Drift cannot silently accumulate.** `consumer-audit` is now a blocking CI check. Any regression that causes the Go tool itself to error (missing endpoint index, malformed schema) blocks the merge. Pure data divergence continues to surface through the PR comment, providing ongoing Phase 3 pressure without being a gate.
4. **New snake_case wire tags cannot enter master.** The `advisory-baseline.txt` is subtractive — any new violation that is *not* already baselined fails the advisory-audit workflow. This holds the line while Phase 3 migrates each resource.
5. **The migration is sized.** 357 snake tags remaining across 39 version×resource pairs; 22 resources to version-bump; 26 distinct resources represented across the three consumer repos (total complexity score 226 — weighted toward `component`, `pattern`, `connection`, `relationship`).

What the schemas repo has **not** yet achieved (scope of Phase 2 + Phase 3):

1. Any field is actually camelCase on the wire where it wasn't before. (Phase 3.)
2. Any downstream duplicate Go type or hand-rolled RTK endpoint is displaced. (Phase 2.)
3. Any drift-masking `utils.QueryParam` fallback is retired. (Phase 2.C.)

## 6. Acceptance criteria for this report

Per [`identifier-naming-migration.md §10 Agent 4.E`](identifier-naming-migration.md#agent-4e--beforeafter-impact-report-publication):

- [x] Baseline agents re-run (`make baseline-field-count` executed; no changes required because Phase 3 has not yet landed any resource migrations).
- [x] Before / after / delta table populated (§2 above).
- [x] Narrative impact section (§5 above).
- [x] Phase 3 progress tracker (§3 above).
- [x] Explicit INTERIM labelling where figures depend on Phase 2 / Phase 3 work that has not yet landed.
- [x] Committed to `meshery/schemas/docs/`.

## 7. Revision history

| Date | Phase | Summary |
|---|---|---|
| 2026-04-23 | Phase 4.B / 4.D / 4.E | Initial interim report. Phase 1 complete, Phase 4.B (consumer-audit blocking) landed this PR, Phase 4.D deferred per per-entry pruning cadence, Phases 2 and 3 in flight. |
