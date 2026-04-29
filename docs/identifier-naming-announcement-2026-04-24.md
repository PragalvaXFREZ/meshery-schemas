# Identifier-Naming Migration — Shipped 2026-04-24

> A one-time announcement for **all Meshery contributors** — community and staff — summarizing the identifier-naming migration ("Option B") that landed across the Layer5 / Meshery ecosystem on 2026-04-22 through 2026-04-24.
>
> This document is a **snapshot** of the migration outcome at the time of its completion. Its contents will not be kept in sync with future changes.
>
> **For the living reference** — the canonical naming directory you should read before writing new code — see [`identifier-naming-contributor-guide.md`](identifier-naming-contributor-guide.md).

---

## What shipped

We standardized **one** naming convention across every software element in every repo. The wire is **camelCase** everywhere; the database is **snake_case**; Go fields follow **Go idiom** (`PascalCase` with initialisms like `ID`, `URL`, `API`); the ORM is the **only** translation layer.

Every active resource, every consumer UI, every shared library, and every CI gate that enforces the contract is now aligned.

The contract is enforced at three CI gates:

- **Blocking schema validation** on every `meshery/schemas` PR (Rules 4, 6, 45, 46, SCREAMING-ID detector, full property-constraint rules).
- **Advisory schema audit** with a baseline file of pre-canonical deferred violations.
- **Blocking consumer-audit** that scans `meshery/meshery`, `layer5io/meshery-cloud`, and `layer5labs/meshery-extensions` on every PR for new TypeScript snake_case wire-key usage.

---

## By the numbers

### 91 pull requests merged across 6 repositories

| Repository | Merged PRs |
|---|---:|
| `meshery/schemas` | **51** |
| `meshery/meshery` | **13** |
| `layer5io/meshery-cloud` | **13** |
| `layer5labs/meshery-extensions` | **8** |
| `layer5io/sistent` | **5** |
| `meshery/meshkit` | **1** |
| **Total** | **91** |

### 15 release tags cut; 7 npm packages published

| Repository | Releases in window |
|---|---|
| `@meshery/schemas` (npm) | v1.1.0, v1.1.1, v1.1.2, **v1.2.0** (current) |
| `@sistent/sistent` (npm) | v0.19.0, v0.19.1, **v0.20.0** (current) |
| `meshery/meshkit` (Go module) | v1.0.5 |
| `meshery/meshery` (server / CLI) | v1.0.10, v1.0.11 |
| `layer5io/meshery-cloud` (server / UI) | v1.0.18, v1.0.19, v1.0.20 |
| `layer5labs/meshery-extensions` | v1.0.10-1, v1.0.11-1 |

### Consumer-audit findings: 23 → 0

The live `make consumer-audit` TypeScript scanner across the three downstream consumer UIs reported 23 snake_case wire-key findings at the start of the migration (17 in meshery-cloud, 6 in meshery, 0 in meshery-extensions) and reports **0** findings on all three today.

### Canonical target-version directories created: 22

All 22 resources in the §9.1 inventory of [`identifier-naming-migration.md`](identifier-naming-migration.md) landed new canonical-casing API versions — 14 in `v1beta3/` (workspace, relationship, design, connection, component, event, invitation, plan, subscription, token, environment, credential, user, organization … *correction: user and organization live in v1beta2*; the precise split is 14 v1beta3 + 8 v1beta2 per the plan), 8 in `v1beta2/` (user, organization, credential, view, key, role, model, keychain, schedule, badge). Every new version publishes **zero** snake_case wire tags; the legacy versions remain on `master` under `info.x-deprecated: true` + `info.x-superseded-by:` markers for external consumers pinning the older form (Phase 4.A — administratively closed without physical deletion).

---

## Contributors

| Contributor | PRs | Primary contributions |
|---|---:|---|
| **[@leecalcote](https://github.com/leecalcote)** — Lee Calcote | **58** | Authored and merged all 22 Phase 3 per-resource canonical-casing schema version bumps (workspace, environment, organization, user, design, connection, team, role, credential, event, view, key, keychain, invitation, plan, subscription, token, badge, schedule, model, component, relationship). Drove every downstream Phase 3 consumer repoint across meshery, meshery-cloud, and meshery-extensions. Authored the Phase 4.A administrative-close decision to retain legacy directories instead of deleting them. Authored the `identifier-naming mandate` doc adoption in all four repo `AGENTS.md` files (Phase 4.C). |
| **[@jamieplu](https://github.com/jamieplu)** | **16** | Authored the entire Phase 0 (baseline artifacts) and Phase 1 (governance + validator hardening) block on `meshery/schemas`: the identifier-naming migration plan (`docs/identifier-naming-migration.md`), the `AGENTS.md` contract amendment, Rule 6 inversion, Rule 32 retirement, Rule 45 (partial casing forbidden), Rule 46 (sibling-endpoint parity), Rule 4 extension to query parameters, the TypeScript consumer auditor (`validation/consumer_ts.go`), the advisory baseline, the consumer-audit CI job, and the `@meshery/schemas` v1.1.0 release bump. |
| **[@miacycle](https://github.com/miacycle)** — Mia Grenell | **13** | Authored Phase 2 tail (final handler dual-accept + UI flips on meshery and meshery-cloud), Phase 2.K Sistent library alignment (re-exports repointed v1beta1 → canonical v1beta3/v1beta2; ~150 wire-key flips across CustomCard, CatalogCard, MetricsDisplay, PerformersSection, CatalogDesignTable, Workspaces, UsersTable; Sistent v0.17.0 → v0.19.1 → v0.20.0), the Phase 4.D validator pruning PR, the Phase 4.E impact report rewrites, the `mesheryctl-1231` master-CI unblocker, and the Sistent release workflow hygiene PRs (commit-back + npm-version idempotence + SSR hotfix). |
| **[@l5io](https://github.com/l5io)** (automated) | **3** | Automated cross-repo `@sistent/sistent` version-bump PRs across meshery, meshery-cloud, and meshery-extensions. Fired by Sistent's `notify-dependents.yml` workflow after each Sistent npm publish. |
| **[@PragalvaXFREZ](https://github.com/PragalvaXFREZ)** | **1** | Authored consumer-audit tooling improvements that shipped as Phase 0 input (better schema-driven logic, delta-from-previous-run summaries, new-schema-version detection in the audit sheet update). |

---

## Timeline

| Date | Milestone |
|---|---|
| 2026-04-22 | Phase 0 baseline artifacts land (`validation/baseline/`). Migration plan merged as `docs/identifier-naming-migration.md`. |
| 2026-04-23 (morning) | Phase 1 governance + validator: Rule 6 inversion, Rule 45, Rule 46, Rule 4 extension, consumer_ts.go, consumer-audit CI (advisory), advisory baseline. `@meshery/schemas` v1.1.0. |
| 2026-04-23 (afternoon) | Phase 3 per-resource canonical version bumps (22 PRs). |
| 2026-04-23 (evening) | Phase 4.A administratively closed (deprecated directories retained). Phase 4.B promoted `consumer-audit` to blocking. Phase 4.D validator pruning. `@meshery/schemas` v1.1.1, v1.1.2. |
| 2026-04-24 (morning) | Phase 2 tail (meshery + meshery-cloud handler dual-accept + UI flips). |
| 2026-04-24 (afternoon) | Phase 2.K Sistent library alignment. Sistent v0.19.0 (regressed), v0.19.1 (hotfix). Phase 2.K cascade for 10 deferred keys. `@meshery/schemas` v1.2.0. `@sistent/sistent` v0.20.0. |
| 2026-04-24 (evening) | Option B administratively complete. |

---

## Where to go next

- **Writing new code?** Start with [`identifier-naming-contributor-guide.md`](identifier-naming-contributor-guide.md) — the canonical naming directory + do/don't.
- **Reviewing a PR?** The validator enforces the contract automatically on `meshery/schemas`. For the other repos, the consumer-audit CI check surfaces any snake-case TypeScript wire-key regressions.
- **Porting an external integration?** Check `docs/identifier-naming-migration.md §9.1` for the resource-by-resource version-bump inventory (what moved from where to where) and the retained-legacy §7/§8 sections of the impact report for what you can still pin to.
- **Studying the outcome as governance?** See [`identifier-naming-impact-report.md`](identifier-naming-impact-report.md) for the before/after metrics table and the validator rule-surface delta.

---

*Published 2026-04-24 by the Option B team. Archived as a snapshot of the migration outcome.*
