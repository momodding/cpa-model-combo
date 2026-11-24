# Quota routing cards: one collapsible card per model combo

## Goal
Replace the single shared Vue Flow canvas in the Quota tab with one collapsible card per configured route (model combo). Each card header shows:
- the combo alias
- the strategy
- a status dot for the route's most recent request: succeeded, failed, pending, or no requests yet

The body holds that route's own flow diagram and node details. Keep the incumbent visual system (shared.css tokens, `.quota-card` geometry). No new dependencies, no build step, CSP unchanged.

## Setup
- `git fetch marketplace`. Create the worktree `../cpa-model-combo-routing-cards` on the new branch `omp/quota-routing-cards`, based on `marketplace/main` (AGENTS.md: a feature worktree is required). Leave `/Users/momodding/Documents/cpa-model-combo` untouched.
- Copy this plan to `plans/quota-routing-cards.md` in the worktree.
- Write ADR and BDR `0014-quota-routing-cards.md`. Cite ADR 0013 as amended: the per-route canvases replace the single canvas, and the ring, API, cadence and fallback are unchanged.

## Behaviour (contract)
- **One card per route.** Show one card for each route with a non-blank alias, in config order (the same filter `routingGraph` uses). Each card's graph is `routingGraph([route], state.accounts, data)`. Positions already start at y=0 per call, and node and edge ids are already route-scoped. **No model change.**
- **Collapse.** Each card is a native `<details class="rd-card">` with a `<summary>` header. This reuses the incumbent `details.quota-settings` pattern and gives keyboard and screen-reader support for free.
  - The open/closed state lives in memory in `routingDiagram.open: Set<alias>` and survives polls.
  - On first render with no prior state, only the first card is open.
  - No persistence across reloads (skipped on purpose).
- **Lazy canvas.** Only open cards render a `VueFlow` (a unique `id` prop per alias). Collapsed cards render the header only. This also removes the problem of fitting a hidden canvas.
  - Fit runs once when a card opens, and when that card's structure key changes.
  - Each open card has its own **Fit** button. The global Fit button is removed.
- **Canvas height.** Height = clamp(rows × 96 + 48, 180, 440) px, where rows is the number of credential rows (the `routingGraph` span). Below 768px the maximum is 360px.
- **Header status dot.** Add a new pure helper in `routing-model.js`: `routeLastRequest(requests, alias, now)`. It returns `{state, at}`, where state is one of `success | failed | pending | idle`. It uses the newest `routingRequests` entry whose `route===alias`:
  - `success` if the request's rolled-up state is `success`. `routingRequests` already makes success sticky.
  - `pending` if the last state is in-flight (`evaluating`, `selected`, `executing` or `fallback`) and `now - at < ROUTING_STALE_EXECUTING_MS`.
  - `failed` for any other terminal or stale state: `failed`, `exhausted`, `cooldown`, `skipped`, or in-flight but stale.
  - `idle` if no request is buffered for that route.
  - The status ignores the request picker, so it always means "last request".
- **Header layout (summary).** Left to right:
  1. a chevron, rotated by `[open]` via CSS
  2. the status dot: an 8px disc in `--rd-c` with `rd-<state>` classes. `idle` is a hollow ring in `--muted`. `pending` gets a soft pulse that is disabled under `prefers-reduced-motion`.
  3. the alias, 15px/600, ellipsis, full alias in `title`
  4. the strategy as a `.quota-badge`-style chip, using the Configuration labels (`priority`→"Priority order", `round-robin`→"Round robin", `random`→"Random", `last_known_good`→"Last known good", `headroom`→"Quota headroom"). Unknown values show raw.
  5. right-aligned muted status text: "Succeeded · 12:04:31", "Failed · …", "Pending · …" or "No requests yet", with tabular numerals. Status is never conveyed by colour alone.
  
  The summary also gets `aria-label="<alias>, <strategy>, last request <status>"`.
- **Request picker.** Keep the single toolbar `<select>`. If the focused request belongs to route X, card X shows that request and every other card shows its own Live (newest event for that route). This is done by passing route-filtered events into `nodeStates`/`nodeEvents`, with focus = picked id if it belongs to the route, else `''`. The legend stays in the shared toolbar.
- **Details.** Selection stays a single global `selected` node id. The existing `#routing-diagram-details` aside is moved into the body of the card that owns the selected node, below the canvas. Collapsing that card clears the selection. The details content builder is unchanged.
- **Unchanged:**
  - polling cadence: the IntersectionObserver still watches `#routing-diagram`, which becomes the card-list container
  - `quotaState` as the scheduler
  - the event ring and `mergeEvents`
  - the API
  - the `#quota-routes` text fallback, still returned when `updateRoutingDiagram` returns false
  - the cooldown clock
  - the empty-state text when no routes exist

## Code changes
- `dashboard/quota/routing-model.js`: add `routeLastRequest`, documented in the header comment's globals list.
- `dashboard/quota/diagram.js`:
  - Change the state to hold per-route entries `{alias, strategy, graph, byId, structureKey, rows}`, plus `open:Set`, `flows:Map<alias,instance>`, `needsFit:Set<alias>`, `states:Map<alias,Map>` (a shallowRef of a Map), and `routes` (a shallowRef array).
  - The Vue app's root render outputs the card list via `h('details',{open, onToggle})`. `onToggle` syncs `open` and triggers fit or clears the selection.
  - `routingNodeView` looks nodes up via the per-route `byId`, found from the node id prefix, or from a merged global `byId` (simplest: keep one global `byId` built from all route graphs).
  - The edge-class computation and the structure/edge keys become per route.
  - `routingTryFit(alias)` checks that card's container width and node dimensions.
  - The global Fit listener is removed.
- `dashboard/quota/panel.html`: remove the `#routing-diagram-fit` button. `#routing-diagram` loses its fixed canvas role and becomes the card list. The details aside stays in the markup and gets moved at runtime.
- `dashboard/quota/diagram.css`:
  - `.routing-diagram` becomes a `display:grid; gap:12px` list with no border.
  - `.rd-card` reuses the `.quota-card` surface: 1px `--line`, radius 14px, paper 82%, hover `--line-strong`.
  - `.rd-card > summary` is a flex row with min-height 52px, padding 12px 16px, `list-style:none`, and `::-webkit-details-marker` hidden. It has a focus-visible ring using the existing outline recipe.
  - `.rd-canvas` is the per-card dotted grid surface with its inline height. Add `.rd-dot`, `.rd-strategy` and `.rd-status`.
  - Keep the reduced-motion block, extended to `.rd-dot`.
  - Move the existing `.routing-diagram .vue-flow__…` selectors to `.rd-canvas`.
- `tab.js`: no change expected. `routingDiagramShown` and `observeRoutingDiagram` keep their ids.

## Verification
- `node --test 'tests/dashboard/*.test.mjs'`: new `routeLastRequest` cases (success after a fallback, final failure ending in cooldown, fresh executing → pending, stale executing → failed, other route ignored, idle).
- `gofmt -l .`, `go vet ./...`, `go test -race -count=1 ./...`. `TestAssembleModelRouterDashboardEmbedsEveryAssetOnce` and the management forbidden-text guard must stay green: no literal script tags, no `innerHTML` in first-party code.
- E2E (`tests/e2e/dashboard.test.mjs`, in the existing quota test): mock `quota-status` with events for two routes. Assert:
  - `.rd-card` count equals the route count
  - the header contains the alias and the strategy label
  - the dot `data-state` is `success` or `failed`
  - toggling a summary renders or removes that card's `.vue-flow`
  
  The browser is unavailable locally, so this runs in GitHub CI, as recorded in ADR 0014.
- Run the impeccable mechanical detector on `dashboard/quota/` and fix any findings.
- Do not claim local visual verification.

## Delivery
- Commit after verification passes. Then ask the user before pushing and opening a PR: the ADR 0013 push override applied only to that branch, and global rules forbid pushing.

## Out of scope
- Persisting open/closed state across reloads, per-card request pickers, and reordering or filtering cards.
