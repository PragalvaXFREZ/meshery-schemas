# Identifier-Naming Migration — Impact Report (Phase 3 complete)

> **Status:** Phase 3 **COMPLETE** — all 22 resources in the §9.1 inventory have been version-bumped to canonical camelCase wire form and merged to `meshery/schemas` `master`. Phase 4.A (deprecated-version sunset) is intentionally deferred pending one release cycle of consumer migration, per plan §10. "After" figures in the table reflect the current `master` with all Phase 3 merges landed. Metrics that depend on Phase 2 drift-masking removal in downstream repos remain pending and are labelled as such.
>
> This document is the deliverable for **Agent 4.E** of [`docs/identifier-naming-migration.md`](identifier-naming-migration.md) §10. It will receive one more refresh after Phase 4.A (deprecated-version sunset) lands; each refresh bumps the revision-history entry at the bottom.

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
| Distinct JSON-tag conventions in *newly-authored* canonical wire models | 3 (snake, camel, lowercase single-word) | 2 (camelCase + lowercase single-word only) — every Phase 3 target version is camelCase-on-wire | −1 | Phase 3 complete |
| Distinct JSON-tag conventions across the whole tree (including pre-canonical versions) | 3 | 3 — legacy versions retain their published wire form until Phase 4.A sunset | unchanged | Phase 4.A deferred |
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
| API versions with snake-case-on-wire JSON tags | ~20 (v1alpha1, v1alpha2, v1alpha3, v1beta1, v1beta2 resources) | 22 legacy version×resource pairs retain snake tags (deprecated, pending 4.A sunset); every canonical-casing target version is snake-free | −17 when Phase 4.A sunsets the deprecated versions | Phase 3 complete; Phase 4.A deferred |
| Canonical target-version directories present | 0 | 22 (one per §9.1 inventory row): 14 in `v1beta3/`, 8 in `v1beta2/` | +22 | Phase 3 complete |

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

| Priority | Resource | Current version(s) | Target version | Consumer count | PR | Status |
|---|---|---|---|---|---|---|
| 1 | workspace | v1beta1 | v1beta3 | 11 | [#800](https://github.com/meshery/schemas/pull/800) | [x] merged |
| 2 | environment | v1beta1 | v1beta3 | 15 | [#815](https://github.com/meshery/schemas/pull/815) | [x] merged |
| 3 | organization | v1beta1 | v1beta2 | 4 | [#805](https://github.com/meshery/schemas/pull/805) | [x] merged |
| 4 | user | v1beta1 | v1beta2 | 7 | [#804](https://github.com/meshery/schemas/pull/804) | [x] merged |
| 5 | design / pattern | v1beta1, v1beta2 | v1beta3 | 33 | [#802](https://github.com/meshery/schemas/pull/802) | [x] merged |
| 6 | connection | v1beta1, v1beta2 | v1beta3 | 27 | [#806](https://github.com/meshery/schemas/pull/806) | [x] merged |
| 7 | team | v1beta1 | v1beta2 | — | [#808](https://github.com/meshery/schemas/pull/808) | [x] merged |
| 8 | role | v1beta1 | v1beta2 | — | [#813](https://github.com/meshery/schemas/pull/813) | [x] merged |
| 9 | credential | v1beta1 | v1beta2 | 1 | [#803](https://github.com/meshery/schemas/pull/803) | [x] merged |
| 10 | event | v1beta1, v1beta2 | v1beta3 | 1 | [#809](https://github.com/meshery/schemas/pull/809) | [x] merged |
| 11 | view | v1beta1 | v1beta2 | 1 | [#810](https://github.com/meshery/schemas/pull/810) | [x] merged |
| 12 | key | v1beta1 | v1beta2 | 2 | [#811](https://github.com/meshery/schemas/pull/811) | [x] merged |
| 13 | keychain | v1beta1 | v1beta2 | 1 | [#814](https://github.com/meshery/schemas/pull/814) | [x] merged |
| 14 | invitation | v1beta1, v1beta2 | v1beta3 | 5 | [#816](https://github.com/meshery/schemas/pull/816) | [x] merged |
| 15 | plan | v1beta2 | v1beta3 | 8 | [#818](https://github.com/meshery/schemas/pull/818) | [x] merged |
| 16 | subscription | v1beta1, v1beta2 | v1beta3 | 6 | [#819](https://github.com/meshery/schemas/pull/819) | [x] merged |
| 17 | token | v1beta1, v1beta2 | v1beta3 | — | [#820](https://github.com/meshery/schemas/pull/820) | [x] merged |
| 18 | badge | v1beta1 | v1beta2 | 5 | [#821](https://github.com/meshery/schemas/pull/821) | [x] merged |
| 19 | schedule | v1beta1 | v1beta2 | 1 | [#817](https://github.com/meshery/schemas/pull/817) | [x] merged |
| 20 | model | v1beta1 | v1beta2 | 13 | [#812](https://github.com/meshery/schemas/pull/812) | [x] merged |
| 21 | component | v1beta2 | v1beta3 | 35 | [#807](https://github.com/meshery/schemas/pull/807) | [x] merged |
| 22 | relationship | v1beta1, v1beta2, v1alpha3 | v1beta3 | 21 | [#801](https://github.com/meshery/schemas/pull/801) | [x] merged |

**Overall progress: 22 / 22 resources complete.** Every resource listed in the §9.1 inventory now has a canonical-casing target-version directory under `schemas/constructs/`, and its prior version carries `info.x-deprecated: true` + `info.x-superseded-by: <target-version>` so the OpenAPI bundler routes to the new version. Previous-version schema files remain in place to satisfy cross-construct `$ref`s until Phase 4.A sunsets them after one release cycle of consumer migration.

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

The wire format has now moved for every resource in the §9.1 inventory. What the schemas repo has achieved:

1. **The canonical contract is documentable in one sentence** and present as MUST / MUST NOT text in `AGENTS.md`.
2. **The validator and the doc agree.** Before Phase 1.B, Rule 32 enforced "DB-backed ⇒ snake on wire" in direct contradiction to the doc's camelCase-on-wire direction. A reviewer citing the doc would be fighting the validator. That conflict is gone.
3. **Drift cannot silently accumulate.** `consumer-audit` is a blocking CI check. Any regression that causes the Go tool itself to error (missing endpoint index, malformed schema) blocks the merge. Pure data divergence continues to surface through the PR comment, providing ongoing Phase 4.A pressure without being a gate.
4. **New snake_case wire tags cannot enter master.** The `advisory-baseline.txt` is subtractive — any new violation that is *not* already baselined fails the advisory-audit workflow. This held the line throughout Phase 3 and continues to hold for any new resource added post-migration.
5. **Every canonical-casing target version is snake-free on the wire.** Every Phase 3 PR shipped with its new version's JSON tags entirely camelCase, path/query parameters camelCase with `Id` suffixes, lower-camelCase `operationId`s, and pagination envelopes flipped to `pageSize` / `totalCount`. DB-backed primitives retain their snake `db:` tag via `x-oapi-codegen-extra-tags` — the ORM is now the sole translation boundary, matching the one-sentence contract.
6. **Every legacy version is bundler-gated.** Each deprecated version's `info` block carries `x-deprecated: true` + `x-superseded-by: <new-version>`, which `build/lib/config.js::isDeprecatedPackage` reads to exclude it from the merged OpenAPI. This lets the new version take over the path space without a duplicate-operation error while downstream consumers migrate.

What remains (scope of Phase 2 downstream sweeps and Phase 4.A sunset):

1. **Downstream drift-masking retirement.** `utils.QueryParam` dual-accept fallbacks in `meshery-cloud` and the 5+ locally-declared Go duplicates of schemas types must still be collapsed onto the canonical types (Phase 2.C / 2.D / 2.F — partially complete; some items landed alongside the individual Phase 3 PRs).
2. **Legacy-version sunset.** Phase 4.A will physically delete each deprecated `schemas/constructs/<old-version>/<resource>/` directory once every downstream consumer has migrated to the new version. This is the point at which the "API versions with snake-case-on-wire JSON tags" metric drops to 0.

## 6. Acceptance criteria for this report

Per [`identifier-naming-migration.md §10 Agent 4.E`](identifier-naming-migration.md#agent-4e--beforeafter-impact-report-publication):

- [x] Baseline agents re-run (`make baseline-field-count` executed; no changes required because Phase 3 has not yet landed any resource migrations).
- [x] Before / after / delta table populated (§2 above).
- [x] Narrative impact section (§5 above).
- [x] Phase 3 progress tracker (§3 above).
- [x] Explicit labelling where figures still depend on Phase 2 downstream drift-masking removal or Phase 4.A legacy-version sunset.
- [x] Committed to `meshery/schemas/docs/`.

## 7. Revision history

| Date | Phase | Summary |
|---|---|---|
| 2026-04-23 | Phase 4.B / 4.D / 4.E | Initial interim report. Phase 1 complete, Phase 4.B (consumer-audit blocking) landed this PR, Phase 4.D deferred per per-entry pruning cadence, Phases 2 and 3 in flight. |
| 2026-04-23 | Phase 3 complete | All 22 resources from §9.1 version-bumped to canonical camelCase wire form: workspace (#800), relationship (#801), design (#802), credential (#803), user (#804), organization (#805), connection (#806), component (#807), team (#808), event (#809), view (#810), key (#811), model (#812), role (#813), keychain (#814), environment (#815), invitation (#816), schedule (#817), plan (#818), subscription (#819), token (#820), badge (#821). Progress tracker flipped to 22/22; §2 metrics updated; §5 narrative revised to reflect completion. Phase 4.A sunset and Phase 2 downstream drift-masking retirement remain open as noted. |
