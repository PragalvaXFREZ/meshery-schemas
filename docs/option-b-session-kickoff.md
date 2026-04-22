# Option B — Session Kickoff

**Read this first. This document is the entry point for every execution session of the Option B identifier-naming migration.**

## 0. What this session is doing

You are one execution session in a multi-session migration that standardizes identifier naming across `meshery/schemas`, `meshery/meshery`, `layer5io/meshery-cloud`, and `layer5labs/meshery-extensions`. The migration direction is **Option B**: camelCase on wire, snake_case only at the DB/ORM boundary.

You are **not** a design session. You do not re-audit, re-plan, re-argue the contract, or introduce new conventions. You read the plan and execute.

## 1. Working directory

```
/Users/l/code/schemas
```

Sibling repos (for read/write during agent execution):
- `/Users/l/code/meshery`
- `/Users/l/code/meshery-cloud`
- `/Users/l/code/meshery-extensions`

## 2. Preconditions (verify before starting)

- `gh auth status -a` — authenticated for `github.com`.
- `git config user.email` — resolves to a committer identity authorized for sign-offs.
- Go 1.25+, Node 20+, the repo Makefile targets runnable.
- `uv` installed for `iterate-pr` skill scripts.
- You can push to `layer5io/meshery-cloud`, `layer5labs/meshery-extensions`, and `meshery/meshery` (or to a fork with auto-PR flow configured).

## 3. Required reading order

Read these in order. Do not skip. Do not skim.

1. `docs/identifier-naming-option-b-migration.md` — master plan. §1 defines the contract; §5 defines the Common Agent Protocol every sub-agent follows; §11 is the orchestration DAG; §14 is the per-repo AGENTS.md boilerplate to paste; §17 is the sign-off gate you check your work against.
2. `docs/option-b-plan-meshery.md` — high-level handoff scoped to `meshery/meshery`.
3. `docs/option-b-plan-meshery-cloud.md` — high-level handoff scoped to `layer5io/meshery-cloud`.
4. `docs/option-b-plan-meshery-extensions.md` — high-level handoff scoped to `layer5labs/meshery-extensions`.

After reading, you understand: the contract, the phase DAG, the Common Agent Protocol, the divergences in each downstream repo, the dependency ordering.

## 4. State of record (cross-session)

Persistent execution state lives in GitHub issues on `meshery/schemas`, labeled `option-b-migration`. Each issue is a phase with a checklist of sub-agents. Issue URLs:

- Phase 0 — Baseline metrics: https://github.com/meshery/schemas/issues/776
- Phase 1 — Schemas governance + validator hardening: https://github.com/meshery/schemas/issues/777
- Phase 2 — Non-breaking downstream alignment: https://github.com/meshery/schemas/issues/778
- Phase 3 — Per-resource versioned migration: https://github.com/meshery/schemas/issues/779
- Phase 4 — Deprecation sunset + enforcement finalization: https://github.com/meshery/schemas/issues/780

Governance PR carrying this plan: https://github.com/meshery/schemas/pull/781

Use the issues as the authoritative state:
- Claim a sub-agent by checking its checklist item with your session ID and start time.
- Post progress as issue comments; link each merged PR by SHA + URL.
- If blocked, post a `BLOCKED:` comment (see §7) and move on.

Do not rely on Claude Code's in-session `TaskList` for cross-session state. The in-session tasks are per-session scratch space only.

## 5. Bootstrap action (what to do after you've read §3)

1. Run `gh issue list --repo meshery/schemas --label option-b-migration --state open`.
2. Identify the lowest-priority-number phase that is not yet `completed`.
3. Within that phase issue, identify the first unchecked sub-agent whose dependencies (per §11 of master plan's DAG) are satisfied.
4. Claim it by posting a comment: `Claimed by session <short-session-id> at <ISO timestamp>.`
5. Execute per the Common Agent Protocol (master plan §5). Specifically:
   - Cut a fresh branch from the target repo's default branch.
   - Implement.
   - Build locally.
   - Test locally (new tests for new behavior; updated tests for modified behavior).
   - Update documentation in the same PR (master plan §12 mandates this).
   - Commit with sign-off.
   - Push, open PR with the §5.6 standard body template.
   - Wait for Copilot + Gemini automated review; address or refute each thread.
   - Arm `gh pr merge --auto` as soon as CI passes.
   - When merged, post the merge SHA + URL as a comment on the phase issue, and check the sub-agent's box in the checklist.
6. Claim the next unblocked sub-agent. Repeat until you hit a checkpoint or stop condition (§7).

## 6. Orchestration model

This session is **the orchestrator**. You spawn sub-agents via the Claude Code Agent tool when:
- The task is clearly parallelizable (e.g., Phase 0 baseline agents, Phase 3 per-resource migrations).
- The task benefits from isolated context (e.g., a read-heavy audit scan of one repo).

You do the task yourself when:
- It's small (single-file fix, doc tweak).
- It requires cross-referencing multiple in-flight PRs.
- It's an orchestration step (claiming issues, updating checkpoints).

Sub-agents you spawn follow the same Common Agent Protocol. You give them the exact charter from the master plan (e.g., "Execute Agent 2.A as defined in §8 of `/Users/l/code/schemas/docs/identifier-naming-option-b-migration.md`") plus the acceptance criteria. You do not re-author their prompts from scratch.

## 7. Stop conditions — when to checkpoint and exit

Exit cleanly at any of these, do not try to power through:

- **Phase boundary complete** — all sub-agents in a phase checked off, sign-off criteria (§17) met. Post a summary comment on the phase issue.
- **2 hours continuous work** — time-box so another session can pick up fresh.
- **Blocked agent** — a sub-agent hit a failure you cannot resolve (§5.7 of master plan). Post a `BLOCKED: <reason>` comment on the phase issue and on the sub-agent's PR if open. Do not force through.
- **Review-thread requires human judgment** — a Copilot or Gemini thread requests a design decision outside the master plan's scope. Post the thread link on the phase issue and stop.
- **CI flaking** — same CI failure after 3 push attempts. Stop and escalate.
- **Repo disagrees with plan** — you find a repo-level concern that contradicts the master plan (e.g., a handler whose existence the plan doesn't anticipate). Do not silently extend the plan. Post a `PLAN GAP:` comment and stop.

Before exit, every in-flight PR must be pushed with current state. Leave nothing uncommitted locally.

## 8. Session-to-session checkpoint protocol

Before exiting any session:

1. All local changes pushed; no uncommitted files except session scratch (`state.json`, `.claude/`).
2. Every open PR labeled `option-b-migration` and with the phase label.
3. Progress comment posted on the active phase issue: what landed (SHAs), what's in flight, what the next session should claim first.
4. If a new risk or gap surfaced, update the relevant per-repo handoff doc (`docs/option-b-plan-*.md`) in a follow-up commit.

## 9. Do-not-do list

- Do NOT re-run the identifier-naming audit. The findings are in the per-repo plans; they are the input, not something to reproduce.
- Do NOT change the contract defined in master plan §1. If you think it should change, post a `PLAN GAP:` comment and stop.
- Do NOT combine sub-agents' PRs. Each sub-agent ships its own PR.
- Do NOT skip review-bot feedback. Every Copilot and Gemini thread gets a fix or a documented refutation.
- Do NOT force push to any branch other than your own throwaway branches.
- Do NOT merge your own governance PR without the human-review gate being satisfied (master plan §5.3).
- Do NOT modify the master plan or this kickoff doc to match what you happen to have implemented. The plan is the contract.
- Do NOT create new identifier conventions, new casing rules, or new validator approaches. All governance decisions are fixed.

## 10. Bootstrap checklist for the very first session (after plan is merged)

The first session after the governance PR merges has a one-time setup:

1. Confirm the governance PR is merged on `meshery/schemas`.
2. Confirm the 5 phase-tracking issues exist on `meshery/schemas` with the `option-b-migration` label.
3. Back-fill the governance PR URL in §4 above (this doc) via a follow-up commit.
4. Then proceed per §5 — starting with Phase 0 baseline agents.

## 11. Session-start command (for every subsequent session)

In a fresh Claude Code session opened at `/Users/l/code/schemas`, paste:

```
Read docs/option-b-session-kickoff.md and execute per §5 of the master plan it references.
```

No additional context required. The kickoff doc and master plan carry everything.

## 12. Escalation channel

Post `PLAN GAP:` or `BLOCKED:` comments on the active phase issue. Mention @leecalcote for any decision outside the plan's scope. Do not proceed on governance questions without explicit human sign-off.

---

**End of kickoff. Proceed to §3.**
