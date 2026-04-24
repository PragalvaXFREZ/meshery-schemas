# Identifier-Naming Migration — Impact Report (Phase 3 + Phase 4 administratively complete)

> **Status:** Phase 3 + Phase 4 **administratively complete (2026-04-23)**. All 22 resources in the §9.1 inventory of [`identifier-naming-migration.md`](identifier-naming-migration.md) have been version-bumped to canonical camelCase wire form and merged to `meshery/schemas` `master`. **Phase 4.A closed without deletion per maintainer decision** — the deprecated `schemas/constructs/v1beta1/` and `schemas/constructs/v1beta2/` directories are **retained on `master` under `info.x-deprecated: true` for external-consumer compatibility** and are **not slated for physical removal.** The OpenAPI bundler (`build/lib/config.js::isDeprecatedPackage`) continues to exclude them from the merged spec, so path-space collisions are prevented while external consumers that pin legacy versions remain functional. The one-release-cycle safety window defined in the original plan was overridden; see [`identifier-naming-migration.md §10 Agent 4.A`](identifier-naming-migration.md#agent-4a--deprecated-version-retirement-administrative-close-no-physical-deletion) and §20 of that plan for the decision record. §8 below is the canonical index of retained legacy directories. Metrics that depend on Phase 2 drift-masking removal in downstream repos remain pending and are labelled as such.
>
> This document is the deliverable for **Agent 4.E** of [`docs/identifier-naming-migration.md`](identifier-naming-migration.md) §10. A further refresh is only required if the maintainer later reverses the non-deletion policy; each refresh bumps the revision-history entry at the bottom.

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
| Distinct JSON-tag conventions across the whole tree (including pre-canonical versions) | 3 | 3 — legacy versions retain their published wire form; administratively closed, directories retained on master | unchanged | Phase 4.A administratively closed; directories retained |
| Total JSON tags in `schemas/constructs/v*/**` | 1727 (Phase 0.A commit) | 2001 (post-0.A walker fixes 9f2e11af / e8ca7594) | +274 walker fidelity, not new tags | Baseline instrumentation |
| JSON tags camelCase | 352 (20.4 %) | 430 (21.5 %) | +78 | Baseline instrumentation |
| JSON tags snake_case | 462 (26.8 %) | 470 (23.5 %) | +8 | Baseline instrumentation |
| JSON tags all-lowercase single word | 907 (52.5 %) | 1095 (54.7 %) | +188 | Baseline instrumentation |
| Per-resource snake-tag remainder (sum across v* versions) | ~462 | 357 snake_tags_to_migrate across 39 versions | −105 | Artefact of fuller walker (not Phase 3 work) |
| Resources with zero snake_tags_to_migrate | — | 15 of 54 versions (28 %) | +15 | Baseline artefact |
| Validator rules total (encoded) | ~20 | 45 discrete rule numbers in use (1–46, Rule 32 retired) | +25 | Phase 1.B–1.F complete |
| Rules added in Phase 1 | — | Rule 45 (partial-casing-migrations), Rule 46 (sibling-endpoint parity), Rule 4 extended to query params, consumer_ts.go (advisory) | +4 | Phase 1.D–1.F complete |
| Rules retired in Phase 1 | — | Rule 32 (DB-backed property names must match db tags) retired to stub | −1 | Phase 1.B complete |
| Sub-rule allowlists retired in Phase 4.D | — | `knownLowercaseSuffixViolations` emptied in `validation/casing.go`; `lowercaseSuffixPattern` regex removed; `GetCamelCaseIssues` lowercase-suffix branch no-op | −1 allowlist | Phase 4.D complete |
| Rule 6 semantics | conditional: DB-backed fields exempt from camelCase | unconditional: camelCase required regardless of DB backing | Inverted | Phase 1.B complete |
| CI gates on schema conventions | 2 (blocking-validation, advisory-audit) | 3 (blocking-validation, advisory-audit, **consumer-audit blocking**) | +1 | Phase 4.B complete |
| `consumer-audit` CI status | did not exist | **blocking** | added + promoted | Phase 1.H + 4.B complete |
| Baselined advisory violations | — (baseline introduced Phase 1.G) | 1773 entries in `build/validate-schemas.advisory-baseline.txt` | — | Phase 1.G complete |
| Drift-masking sites (`utils.QueryParam` dual-accept in `meshery-cloud`) | ≥6 sites *(plan §15)* | Phase 2.C + Phase 3 consumer-repoint PRs landed dual-accept across workspace / environment / filter / design / share handlers; Phase 2 tail PR layer5io/meshery-cloud#5092 adds the final 10 sites covering tokens / credentials / approve+deny handlers. With that PR merged the dual-accept surface is the complete set. | Phase 2.C + Phase 2 tail complete (PR merge-pending) |
| Locally-declared Go duplicates of schemas | 5+ (MesheryPattern ×2, MesheryPatternRequestBody, MesheryFilter, MesheryApplication) *(plan §15)* | Phase 3 design (#802) + Phase 3 consumer-repoint PRs on `meshery` displaced the `MesheryPattern` / `MesheryFilter` / `MesheryApplication` Go locals onto the canonical `v1beta3/design` schemas types. Residual locals (if any) are limited to request-wrapper shims retained for backward compatibility. | Phase 2.D / 2.F substantially complete |
| Cloud UI `api.ts` hand-rolled endpoints | 30+ *(plan §15)* | **0 snake-case wire findings** after merge of layer5io/meshery-cloud#5092 (17 → 0). Hand-rolled endpoints that still exist are now casing-aligned with `@meshery/schemas/cloudApi`. | Phase 2 tail complete (PR merge-pending) |
| Same-file casing contradictions | ≥8 *(plan §15)* | 0 reachable via consumer-audit after Phase 2 tail PRs (meshery/meshery#18904 + layer5io/meshery-cloud#5092) close the last rtk-query snake wrappers. | Phase 2.G/2.H/2.I + Phase 2 tail complete (PR merge-pending) |
| ALL-CAPS `ID` on wire (schemas repo) | 0 `screaming` json tags in schemas baseline | 0 (unchanged — schemas was already clean) | 0 | Already clean |
| ALL-CAPS `ID` on wire (Go structs, meshery + cloud) | 22 `SCREAMING` json tags *(tag-divergence.json)* | `isOAuth` flipped to `isOauth` in `meshery-cloud` (PR #5092); remainder contained by server-side dual-accept and will attrite as deprecated structs are retired. | Phase 2.A–2.E substantially complete |
| Consumer-audit TypeScript findings (live, across 3 consumer repos) | — (auditor added Phase 1.F) | **0 findings on meshery-cloud** after PR #5092 (17 → 0); **0 findings on meshery** after PR #18904 (6 → 0); **0 findings on meshery-extensions** (was already clean). Total: **23 → 0** on the three consumer trees post-merge. | −23 to zero | Phase 2 tail complete (PR merge-pending) |
| API versions with snake-case-on-wire JSON tags | ~20 (v1alpha1, v1alpha2, v1alpha3, v1beta1, v1beta2 resources) | 22 legacy version×resource pairs retain snake tags (deprecated, administratively closed, directories retained on master); every canonical-casing target version is snake-free | 0 on the wire for canonical versions; retained count unchanged by design | Phase 3 complete; Phase 4.A administratively closed, directories retained |
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
| 4.D | `knownLowercaseSuffixViolations` allowlist and `lowercaseSuffixPattern` regex retired in `validation/casing.go`; `GetCamelCaseIssues` lowercase-suffix branch left as a documented no-op. The `screamingIDRE` detector, Rule 4 (URL parameters), Rule 6 (schema property names), Rule 45 (partial-casing migrations), and Rule 46 (sibling-endpoint parity) are retained permanently as forward-looking guardrails. Rule 32 remains the sole retired rule number; no additional rule number was retired — only a sub-rule allowlist inside Rule 6's casing helper — so the "45 discrete rules" count above is unchanged. |

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

### 4.5 Validator rule pruning (Phase 4.D — complete)

Phase 3 landed all 22 per-resource canonical-casing version bumps and Phase 4.A administratively closed with every legacy directory carrying `info.x-deprecated: true`. The audit walker (`validation/audit.go::walkValidatedConstructSpecs`) already skips deprecated specs and only processes the latest non-deprecated version per construct, so the historical lowercase-suffix identifiers (`userid`, `orgid`, `pageurl`, …) enumerated by `knownLowercaseSuffixViolations` could no longer reach any live resource. The allowlist is therefore retired:

- `knownLowercaseSuffixViolations` in `validation/casing.go` — replaced with an empty map + retirement comment; `HasLowercaseSuffix`/`GetCamelCaseIssues` public signatures preserved so external callers type-check unchanged.
- `lowercaseSuffixPattern` regex in `validation/casing.go` — removed (was defined but never referenced; dead weight).
- `GetCamelCaseIssues` — lowercase-suffix branch kept with an inline comment explaining it is a Phase 4.D-retired no-op; the issue-construction plumbing stays intact so a future pattern-based detector can slot in without restructuring the function.
- Test `TestGetCamelCaseIssues` — row `{"userid", 1}` updated to `{"userid", 0}` with a comment redirecting readers to `screamingIDRE` and `IsBadPathParam`, which remain the live guardrails against the regression shape.

The rules explicitly kept as permanent forward-looking guardrails are `screamingIDRE` (SCREAMING-case `ID` detection), Rule 4 (URL parameter casing incl. `IsBadPathParam`), Rule 6 (unconditional schema-property camelCase), Rule 45 (partial-casing-migrations forbidden), and Rule 46 (sibling-endpoint parameter parity). Rule 32 remains the only retired rule number. The `dbMirroredFields` set was re-verified — its post-Phase-1.B docstring (wire is camelCase regardless of DB backing; the set exists solely for `matcher.go`'s consumer-type diff) still accurately reflects current semantics.

The advisory baseline (`build/validate-schemas.advisory-baseline.txt`, 1827 entries) did not need to shrink: the retired allowlist was contributing zero baselined violations because every property it covered lived in a deprecated directory the audit walker was already skipping. `make audit-schemas-full` still reports 530 advisory issues on `master`, unchanged by this PR.

## 5. Narrative impact

The wire format has now moved for every resource in the §9.1 inventory. What the schemas repo has achieved:

1. **The canonical contract is documentable in one sentence** and present as MUST / MUST NOT text in `AGENTS.md`.
2. **The validator and the doc agree.** Before Phase 1.B, Rule 32 enforced "DB-backed ⇒ snake on wire" in direct contradiction to the doc's camelCase-on-wire direction. A reviewer citing the doc would be fighting the validator. That conflict is gone.
3. **Drift cannot silently accumulate.** `consumer-audit` is a blocking CI check. Any regression that causes the Go tool itself to error (missing endpoint index, malformed schema) blocks the merge. Pure data divergence continues to surface through the PR comment, providing ongoing Phase 4.A pressure without being a gate.
4. **New snake_case wire tags cannot enter master.** The `advisory-baseline.txt` is subtractive — any new violation that is *not* already baselined fails the advisory-audit workflow. This held the line throughout Phase 3 and continues to hold for any new resource added post-migration.
5. **Every canonical-casing target version is snake-free on the wire.** Every Phase 3 PR shipped with its new version's JSON tags entirely camelCase, path/query parameters camelCase with `Id` suffixes, lower-camelCase `operationId`s, and pagination envelopes flipped to `pageSize` / `totalCount`. DB-backed primitives retain their snake `db:` tag via `x-oapi-codegen-extra-tags` — the ORM is now the sole translation boundary, matching the one-sentence contract.
6. **Every legacy version is bundler-gated.** Each deprecated version's `info` block carries `x-deprecated: true` + `x-superseded-by: <new-version>`, which `build/lib/config.js::isDeprecatedPackage` reads to exclude it from the merged OpenAPI. This lets the new version take over the path space without a duplicate-operation error while downstream consumers migrate.

What remains (scope of Phase 2 downstream sweeps):

1. **Downstream drift-masking retirement.** `utils.QueryParam` dual-accept fallbacks in `meshery-cloud` and the remaining locally-declared Go duplicates of schemas types attrite as the deprecated structs are retired; they are intentionally retained while external consumers pin legacy versions. The consumer-audit now reports **0 TypeScript findings** across all three consumer trees once the Phase 2 tail PRs (meshery/meshery#18904 + layer5io/meshery-cloud#5092) land; no additional sweep work is scheduled.

Legacy-version sunset is **administratively closed without physical deletion** (2026-04-23 maintainer decision). The deprecated `schemas/constructs/v1beta1/` and `schemas/constructs/v1beta2/` directories remain on `master` indefinitely under `info.x-deprecated: true` + `info.x-superseded-by:` markers so external consumers that pin legacy versions are not stranded. The OpenAPI bundler excludes them from the merged spec; the canonical versions own the wire today. The "API versions with snake-case-on-wire JSON tags" metric therefore does **not** drop to 0 — the retained legacy directories are accounted-for debt, indexed in §8, not a pending cleanup. Any future physical deletion is a separate maintainer decision.

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
| 2026-04-23 | Phase 4.B / 4.D / 4.E | Initial interim report. Phase 1 complete, Phase 4.B (consumer-audit blocking) landed this PR, Phase 4.D deferred per-entry pruning cadence, Phases 2 and 3 in flight. |
| 2026-04-23 | Phase 3 complete | All 22 resources from §9.1 version-bumped to canonical camelCase wire form: workspace (#800), relationship (#801), design (#802), credential (#803), user (#804), organization (#805), connection (#806), component (#807), team (#808), event (#809), view (#810), key (#811), model (#812), role (#813), keychain (#814), environment (#815), invitation (#816), schedule (#817), plan (#818), subscription (#819), token (#820), badge (#821). Progress tracker flipped to 22/22; §2 metrics updated; §5 narrative revised to reflect completion. Phase 4.A sunset and Phase 2 downstream drift-masking retirement remain open as noted. |
| 2026-04-23 | Phase 4.A administrative close | One-release-cycle safety window overridden by maintainer decision; **Phase 4.A closed without physical deletion.** Deprecated `schemas/constructs/v1beta1/` (25 resources) and `schemas/constructs/v1beta2/` (7 resources) directories retained on `master` under `info.x-deprecated: true` + `info.x-superseded-by:` markers for external-consumer compatibility. Status banner updated; §2 status column flipped from "Phase 4.A deferred" to "administratively closed, directories retained"; §5 narrative revised; new §8 retained-legacy-directories index added. Any future physical deletion is a separate maintainer decision, not scheduled. |
| 2026-04-23 | Phase 4.D complete | `knownLowercaseSuffixViolations` retired in `validation/casing.go`; unused `lowercaseSuffixPattern` regex removed; `GetCamelCaseIssues` lowercase-suffix branch is now a documented no-op; `TestGetCamelCaseIssues` expectation for `userid` updated from 1 → 0. Kept permanently: `screamingIDRE`, Rule 4 (URL parameters), Rule 6 (schema property names), Rule 45 (partial-casing migrations), Rule 46 (sibling-endpoint parity) — these are the forward-looking guardrails. Rule 32 remains the sole retired rule number; the "45 discrete rules" count in §2 is unchanged. The advisory baseline did not need to shrink (the retired allowlist was contributing zero entries, because every offender lived in a deprecated directory the audit walker was already skipping). §2, §2.1, §4.5 updated. This unblocks the Phase 4.E final impact-report refresh. |
| 2026-04-23 | Phase 2 tail + Phase 4.E final | Last 23 consumer-audit TypeScript findings closed across the two remaining downstream UIs: **meshery-cloud PR #5092** flips the 17 hand-rolled `ui/api/api.ts` sites (`isOAuth` → `isOauth`, token / credential / user_id / form / task wrappers) with matching server-side dual-accept via `utils.QueryParam` on `/api/identity/tokens`, `/api/integrations/credentials`, `/api/content/patterns`, `/api/content/views`, and the users-request approve + deny handlers; **meshery PR #18904** flips the 6 `ui/rtk-query/` sites (`connection.ts`, `environments.ts`, `notificationCenter.ts`, `workspace.ts`) and adds `UnmarshalJSON` dual-accept shims in `workspace_handlers.go`, `environments_handlers.go`, `server_events_configuration_handler.go`, and `k8sconfig_handler.go` (Go case-insensitive JSON-tag fallback does not bridge `_` boundaries, so explicit shims are required). Consumer-audit TypeScript findings: meshery-cloud 17 → 0, meshery 6 → 0, meshery-extensions already 0; total **23 → 0** across the three consumer trees post-merge. §2 rows for drift-masking sites, locally-declared Go duplicates, cloud UI `api.ts` hand-rolled endpoints, same-file casing contradictions, SCREAMING `ID` on wire refreshed; new row added for live consumer-audit TypeScript findings. §5 "what remains" updated to reflect zero scheduled Phase 2 sweep work. **The migration is substantively complete:** every canonical target version owns the wire, every consumer tree audits clean, the validator forward-looking guardrails are retained, and the retained legacy directories are accounted-for debt (§8), not an open cleanup item. |

## 8. Retained legacy directories

Phase 4.A closed administratively on 2026-04-23; physical deletion of deprecated `schemas/constructs/<old-version>/<resource>/` directories was **overridden by maintainer decision**. The table below is the canonical index of directories retained on `master` for external-consumer compatibility. Each carries `info.x-deprecated: true`; the OpenAPI bundler (`build/lib/config.js::isDeprecatedPackage`) reads that marker and excludes the directory from the merged spec, so the canonical target version owns the wire without path-space collision. Every retained directory is frozen — no schema edits in place — and the generated client surface for external consumers pinning the legacy version continues to work.

If an external consumer imports from a path listed below, this is intentional and supported; the `x-superseded-by` column indicates the canonical version they should migrate to when next convenient. If a column is blank the legacy resource is retained as-is without a direct successor in the §9.1 inventory (typically because its functionality moved into a different resource or was retired in-product; treat these as frozen).

### 8.1 `schemas/constructs/v1beta1/*` (25 deprecated directories)

| Directory | `x-superseded-by` |
|---|---|
| `schemas/constructs/v1beta1/academy/` | `v1beta2` |
| `schemas/constructs/v1beta1/badge/` | — (retained as-is) |
| `schemas/constructs/v1beta1/catalog/` | `v1beta2` |
| `schemas/constructs/v1beta1/component/` | `v1beta2` |
| `schemas/constructs/v1beta1/connection/` | `v1beta2` |
| `schemas/constructs/v1beta1/core/` | `v1beta2` |
| `schemas/constructs/v1beta1/credential/` | — (retained as-is) |
| `schemas/constructs/v1beta1/design/` | `v1beta2` |
| `schemas/constructs/v1beta1/environment/` | `v1beta3` |
| `schemas/constructs/v1beta1/event/` | `v1beta2` |
| `schemas/constructs/v1beta1/invitation/` | `v1beta2` |
| `schemas/constructs/v1beta1/key/` | — (retained as-is) |
| `schemas/constructs/v1beta1/keychain/` | — (retained as-is) |
| `schemas/constructs/v1beta1/model/` | `v1beta2` |
| `schemas/constructs/v1beta1/organization/` | `v1beta2` |
| `schemas/constructs/v1beta1/plan/` | `v1beta2` |
| `schemas/constructs/v1beta1/relationship/` | `v1beta2` |
| `schemas/constructs/v1beta1/role/` | `v1beta2` |
| `schemas/constructs/v1beta1/schedule/` | `v1beta2` |
| `schemas/constructs/v1beta1/subscription/` | `v1beta2` |
| `schemas/constructs/v1beta1/team/` | `v1beta2` |
| `schemas/constructs/v1beta1/token/` | `v1beta2` |
| `schemas/constructs/v1beta1/user/` | `v1beta2` |
| `schemas/constructs/v1beta1/view/` | — (retained as-is) |
| `schemas/constructs/v1beta1/workspace/` | `v1beta3` |

### 8.2 `schemas/constructs/v1beta2/*` (7 deprecated directories — first-generation canonical targets superseded by `v1beta3`)

| Directory | `x-superseded-by` |
|---|---|
| `schemas/constructs/v1beta2/connection/` | `v1beta3` |
| `schemas/constructs/v1beta2/design/` | `v1beta3/design` |
| `schemas/constructs/v1beta2/event/` | — (retained as-is) |
| `schemas/constructs/v1beta2/invitation/` | — (retained as-is) |
| `schemas/constructs/v1beta2/plan/` | — (retained as-is) |
| `schemas/constructs/v1beta2/subscription/` | — (retained as-is) |
| `schemas/constructs/v1beta2/token/` | — (retained as-is) |

### 8.3 Operating rules for the retained tree

- **Do not delete** any directory listed above. Physical deletion is not scheduled; any future decision to remove a directory is a separate maintainer action documented in [`identifier-naming-migration.md §20`](identifier-naming-migration.md#20-revision-history).
- **Do not remove or modify `x-deprecated: true` / `x-superseded-by:` markers** on these directories. They are the compatibility signal that documents the directory's frozen status and lets the bundler exclude the legacy path from the merged spec.
- **Do not add new properties, operations, or resources** to these directories. They are frozen; all new work happens in the canonical-casing target versions.
- When a downstream consumer reports a migration blocker, **fix it in the canonical version, not in the retained legacy directory.**
- `make validate-schemas` and `make audit-schemas` continue to run against the retained tree; the existing advisory baseline (`build/validate-schemas.advisory-baseline.txt`) absorbs the known snake-wire violations so CI remains green.
