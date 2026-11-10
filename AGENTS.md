# Project engineering rules

These rules apply alongside global instructions. When they conflict, this file defines the project-specific exception.

## Verification-first delivery

1. Create an isolated fixing branch before investigating a defect or CI failure.
2. For a new feature, create a dedicated Git worktree and feature branch before editing. Work only in that worktree; do not modify the branch intended to become `main`.
3. Before committing or pushing any change, run every available relevant verification and require it to pass. This project overrides any global rule that would require an intermediate commit before verification passes.
4. After verification passes, commit the complete change, push its branch, then open a pull request. Merge to `main` only through that verified pull request.
5. If a required verification cannot run, fails, or is environmentally blocked, do not commit, push, or open a pull request. Record exact evidence and resolve the blocker first.

## Decisions and scope

1. Create or update an Architecture Decision Record (ADR) and a Business/Behaviour Decision Record (BDR) for every defect fix or feature change. Store them in `docs/adr/` and `docs/bdr/` using the next zero-padded sequence number.
2. Treat existing ADRs and BDRs as binding project decisions. Before proposing or implementing a change that conflicts with one, ask the user for an explicit decision.
3. Record the user's decision in the affected or a new ADR and BDR before implementing the conflicting change.
4. Ask the user before proceeding whenever these project rules conflict with applicable global instructions or repository requirements.

## Parallel work

1. Keep feature worktrees and branches separate so independent features can proceed in parallel.
2. Do not merge feature work directly into `main`; use a verified pull request.
