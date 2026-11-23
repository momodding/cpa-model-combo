# ADR 0013: Quota routing diagram

Extends ADR 0008 (headroom routing), ADR 0009 and ADR 0010 (Quota tab). No binding decision is reversed.

- Backend: a 512-event in-memory ring (`routing_events.go`) owns sequence numbers and timestamps. It has its own mutex, is never held with `runtime.mu`, never fetches quota, and survives configuration reloads. Events carry only `quotaDiagnosticIdentity` (ID, provider, auth index, label); no secrets.
- `GET /v0/management/plugins/model-combo/quota-status` adds `cooldowns`, `events`, `event_cursor`, `events_truncated`, and accepts `events_since`. Cursor 0 reads the whole buffer; only a positive cursor with a real gap is truncated. A non-numeric or negative value returns 400 `invalid_query`. The existing `routes` field is unchanged.
- Event order per failed candidate is `failed → cooldown → fallback` in every execute and stream path, including unrestricted models.
- Frontend: Vue 3.5.46 runtime global build and `@vue-flow/core` 1.48.2 IIFE and CSS are vendored unmodified in `dashboard/vendor/` and inlined through `{{include}}`. No build step, CDN, or `eval`; the page CSP stays `script-src 'unsafe-inline'`. `diagram.js` uses `Vue.h` render functions, not template compilation.
- `quotaState` stays the single scheduler. `IntersectionObserver` on `#routing-diagram` sets the delay: 2 s in view, 15 s otherwise; polling stops when the tab is inactive or `document.hidden`. If Vue or Vue Flow is missing or mounting throws, the legacy text list renders instead.
- Third-party licenses (MIT, ISC, BSD-3-Clause for `d3-ease`) are reproduced in `THIRD_PARTY_NOTICES.md`.

## Verification status

- Run, exit 0: `gofmt -l .` (empty), `go vet ./...`, `go test -race -count=1 ./...`, and `node --test 'tests/dashboard/*.test.mjs'` (32 pass, 0 fail). `TestModelRouterManagementDashboardReusesCPAMCSessionAndTheme` failed on the vendored bundles' `http://`/`innerHTML` text; its forbidden-text guard now cuts the verbatim vendor files out first and still fails if a vendor file is not embedded byte-for-byte.
- Not run: real-browser checks (visual states, themes, reduced motion, request cadence), the fail-then-recover text fallback, the 2 s/15 s cadence, and `npm run test:e2e` (needs a disposable CPA instance and Chrome). `/usr/bin/google-chrome` is absent, and omp's managed browser failed to start twice ("Shared browser daemon unavailable"; the earlier open timed out after ~18 s). Per AGENTS.md these must be verified in GitHub CI; the diagram is unverified in a browser until then.

## Delivery

ADR/BDR 0008 prohibit pushing in this session, while AGENTS.md requires push and PR and names GitHub CI as the fallback for verification that cannot run locally. User decision (2026-11-23): override the 0008 push prohibition for this branch only. Commit, push `omp/quota-routing-diagram`, and open a PR to `main`; CI supplies the browser and `npm run test:e2e` verification. Merge only after CI is green.
