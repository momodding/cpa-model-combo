# ADR 0014: Quota routing cards

Amends ADR 0013 (Quota routing diagram). No other binding decision is reversed.

- The single shared Vue Flow canvas is replaced by one collapsible card per route with a non-blank alias, in config order. Each card renders `routingGraph([route], state.accounts, data)`; the model (`routing-model.js`) is unchanged apart from the new pure helper `routeLastRequest(requests, alias, now)`.
- Unchanged from ADR 0013: the 512-event ring and `mergeEvents`, the API, `quotaState` as the only scheduler, the 2 s / 15 s `IntersectionObserver` cadence (it now watches the card-list container `#routing-diagram`), the cooldown clock, the `#quota-routes` text fallback, vendored Vue / Vue Flow, and the page CSP.
- Each card is a native `<details class="rd-card">`. Open state lives in memory (`routingDiagram.open`), survives polls, is not persisted across reloads, and only the first card is open on first render. Only open cards mount a `VueFlow` (id = alias); fit runs when a card opens or its structure key changes. The global Fit button is replaced by a per-card Fit button.
- Header status dot = `routeLastRequest`: `success` (sticky roll-up), `pending` (in-flight and younger than `ROUTING_STALE_EXECUTING_MS`), `failed` (any other terminal or stale state), `idle` (no buffered request). It ignores the request picker.
- The single request picker stays. A card shows the picked request only if it belongs to that route; otherwise its own newest request. The shared details aside is moved into the card owning the selected node; collapsing that card clears the selection.

## Verification status

- Local: `node --test 'tests/dashboard/*.test.mjs'`, `gofmt -l .`, `go vet ./...`, `go test -race -count=1 ./...` (results recorded in the delivery commit message).
- Not run locally: real-browser checks (card layout, collapse, fit, themes, reduced motion) and `npm run test:e2e` — no browser is available here. Per AGENTS.md they are verified in GitHub CI via the extended quota test in `tests/e2e/dashboard.test.mjs`. The cards are unverified visually until CI runs.

## Delivery

Commit locally on `omp/quota-routing-cards`. The ADR 0013 push override applied to that branch only; global rules forbid pushing, so the user is asked before any push or PR.
