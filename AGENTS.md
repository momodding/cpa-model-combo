# Project engineering rules

These rules apply alongside global instructions. When they conflict, this file defines the project-specific exception.

## Verification-first delivery

1. Create an isolated fixing branch before investigating a defect or CI failure.
2. For a new feature, create a dedicated Git worktree and feature branch before editing. Work only in that worktree; do not modify the branch intended to become `main`.
3. Before committing or pushing a change, run every relevant verification available locally and require it to pass. If a required local check cannot complete because of a documented environmental limitation or timeout (rather than an observed test/code failure), use the CI fallback below.
4. CI fallback: commit and push the complete change to its isolated feature branch and open a pull request so the required GitHub Actions checks can run. Record which local gates were blocked and why. Do not merge unless every required CI check passes.
5. If a local check reports an actual test/code failure, do not use CI as a substitute for resolving it; fix or investigate the failure first. If a required GitHub Actions check fails, do not merge; resolve the failure and rerun CI.
6. Merge to `main` only through a verified pull request.

## Decisions and scope

1. Create or update an Architecture Decision Record (ADR) and a Business/Behaviour Decision Record (BDR) for every defect fix or feature change. Store them in `docs/adr/` and `docs/bdr/` using the next zero-padded sequence number.
2. Treat existing ADRs and BDRs as binding project decisions. Before proposing or implementing a change that conflicts with one, ask the user for an explicit decision.
3. Record the user's decision in the affected or a new ADR and BDR before implementing the conflicting change.
4. Ask the user before proceeding whenever these project rules conflict with applicable global instructions or repository requirements.

## Parallel work

1. Keep feature worktrees and branches separate so independent features can proceed in parallel.
2. Do not merge feature work directly into `main`; use a verified pull request.
