# BDR 0013: Quota routing diagram

Extends BDR 0008–0010.

- The Quota tab shows a routing diagram (combo → model → credential) replacing the text-only headroom list. Nodes show state, quota percent where known, and a cooldown countdown; edges mark the active path and fallbacks.
- States: pending, evaluating, selected, executing, success, failed, skipped, exhausted, cooldown, fallback. Each has a glyph and label, so colour is never the only signal. Reduced-motion users get no animation.
- A request selector pins the view to one request (default: latest). Selecting a node shows its details and event timeline. A Fit button recentres the graph.
- The diagram refreshes every 2 seconds while visible and every 15 seconds when scrolled away; nothing polls while the tab is inactive or the page is hidden.
- If the diagram cannot load, the previous text list is shown. Routing behaviour, quota polling, and the credentials grid are unchanged.
- Events live in memory only (latest 512) and reset on restart; the diagram says so when events were dropped. No credential secrets appear in events.

- Delivery: the user overrode the BDR 0008 no-push rule for this feature only (2026-11-23). The branch is pushed and reviewed as a pull request; browser and end-to-end checks run in GitHub CI, and merge waits for CI to pass.
