# ADR 0010: Quota dashboard polish

Status: accepted. Builds on ADR 0009; no conflict with existing decisions.

- Quota status renders as a semantic table (Account, Provider, State, Remaining, Fetched, Notes) built with `textContent` only. No new data is fetched or persisted.
- Removed the decorative body grid background and the "CPA execution map" eyebrow.
- Kept the Inter-first font stack; replacing it is a redesign and out of scope.
- No API, routing or config change.
