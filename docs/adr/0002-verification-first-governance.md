# ADR 0002: Verification-first project governance

- Status: Accepted
- Date: 2026-11-10

## Context

The project needs isolated feature development, reproducible verification, and a clear decision trail before changes reach `main`. A generic checkpoint-commit workflow can create commits before relevant validation has passed.

## Decision

`AGENTS.md` defines project-specific verification-first delivery rules:

- start defects on a fixing branch before investigation;
- start features in a dedicated worktree and feature branch;
- run all available relevant verification successfully before committing or pushing;
- deliver through a verified pull request rather than directly to `main`;
- create ADR and BDR records for every defect fix or feature change;
- require explicit user approval and records before departing from a binding decision.

## Consequences

Parallel features remain isolated. Known-bad intermediate commits and direct `main` merges are prohibited. Environmental verification blockers stop delivery rather than being bypassed.
