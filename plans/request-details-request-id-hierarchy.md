# request-details-request-id-hierarchy
<!-- source-branch: omp/request-details-request-id-hierarchy -->
<!-- work-branch: omp/request-details-request-id-hierarchy-2 -->

## Goal and approved policy

Implement the user's **No heuristic binding** choice in the Model Combo feature worktree `/Users/momodding/Documents/cpa-model-combo-request-id-hierarchy`. This plan replaces the previous schema-7/fork plan. No CLIProxyAPI changes, fork dependency, identity inference, or new lifecycle negotiation.

Stock CPA v7.3.9 cannot reliably populate new stored usage Request ID/API path fields plugin-only. Keep historical values readable; make their columns ordinary hideable columns; preserve access to provider attempts when Request ID is hidden.

## Established evidence (do not repeat the investigation)

- Stock source at `/Users/momodding/Documents/cpa-model-combo-v1.0.1/.cpa-source`, commit `61fdfc341b96178a8dcb53f2efc46cbc341d267c`, SDK v7.3.9/schema 6.
- `sdk/api/handlers/handlers.go:196-235`, `requestExecutionMetadata`: path available through request metadata. Interception carries RequestID/TraceID and metadata (`internal/pluginhost/adapters_interceptors.go:108-140`).
- `pluginapi.UsageRecord` has no RequestID, TraceID, or endpoint. `internal/pluginhost/adapters_usage_translation.go:132-205`, `usageAdapter.HandleUsage`, builds the usage RPC from the usage record, not interceptor identity. `rpc_client.go:604-607` sends it directly. Executor metadata sanitization does not establish a later usage callback join key.
- `usage_record.go:15-66`, `HandleUsage` / `storedRecordFromUsage`, does not populate RequestID/RequestEndpoint. Existing attribution matching is not an exact inbound identity join; never extend it to these fields.
- Stock schema 6 supports `request.complete`; this plugin advertises `RequestLifecycle: false` (`abi.go:305-321`). Completion support alone would not connect provider usage to inbound requests.
- Historical session/query behavior remains in `usage_query.go:540-643`; `requestParent` reads historical identity/endpoint/session fields. Provider callbacks remain the only accounting input.
- `abi_test.go:292-338`, `TestABIStockSchemaNegotiationAndDispatch`, already proves unknown identity fields do not populate stored records. Keep this contract.

## Decisions

1. No heuristic correlation using time, credentials, model/body matching, FIFO, markers, hashes, or router IDs. Do not store bodies, query strings, credentials, or completion errors.
2. Keep Request ID/API path definitions and labels, historical fields, database schema, management schema version 2, and legacy `router_request_id` grouping unchanged.
3. **Keep existing visibility defaults unchanged.** No automatic hiding based on record contents, no default migration, and no new preference setting. Users may hide either column; explicit empty hidden lists remain authoritative. This is the smallest change and avoids unexpectedly concealing historical data.
4. Attempt controls occupy the Request ID cell when visible; otherwise the first visible data cell. Do not add a fixed utility column. The fallback cell retains its formatted value/status badge and gets a prepended toggle (parent) or `Attempt N` label (child). When Request ID is visible, retain the current child label in that cell rather than displaying child identity.
5. Keep at least one data column visible. Existing UI guard and `validHiddenColumns` fallback remain; backend all-hidden submissions now fail existing validation rather than silently forcing Request ID visible. Legacy malformed all-hidden preferences may be read unchanged; the existing UI sanitizer reveals the first defined column. Do not add database repair or write-on-read.
6. Add ADR/BDR 0005 and mark 0004 superseded, rather than rewriting historical decisions. User's no-heuristic/plugin-only choice authorizes the supersession. No new changelog file: none exists.

## Implementation units

### 1. Remove forced visibility while preserving preference validation

- `usage_preferences.go:46-87`, `normalizeDashboardPreferences`: remove the call to `removeRequiredRequestColumn`; remove that helper and now-unused `slices` import. Retain `normalizeColumnList` trimming, deduplication, sorting, unknown-column rejection, and all-hidden rejection (`:102-124`).
- `usage_store.go:742-755`, `QueryPreferences`: remove forced Request ID removal on reads. Preserve unmarshal/default semantics; no schema or stored-data migration.
- `dashboard/usage/tab.js:181-214`, `applyUsagePreferences`: remove `hiddenRequestColumns.delete('request_id')`. Leave `validHiddenColumns` and default handling unchanged.
- `dashboard/usage/tables.js:1-18`, `visibleColumns` / `renderColumnControls`: remove both Request ID special cases. Checked means not hidden; disabled means checked and last visible. Preserve `changeColumnVisibility` guard at `:156-164` for both tables.
- `usage_store_test.go:406-442`: replace `TestDashboardPreferencesNeverHideRequiredRequestID` with behavioral coverage for hiding both identity/path columns, whitespace/deduplication, save/read/reopen persistence, and legacy JSON retaining hidden Request ID plus saved page size. Verify an all-hidden save returns an error without overwriting the previous valid preferences. Verify all-but-one is accepted. Keep existing unrelated defaults tests.
- `usage_management_test.go:50-55`: extend existing preferences round trip to assert the API preserves hidden Request ID/API path. No new endpoint or wire field.

### 2. Preserve attempt interaction with arbitrary visible columns

- `dashboard/usage/tables.js:108-140`, `requestIDCell`, `requestRow`, `renderRequestTable`: separate cell data rendering from attempt decoration with the minimum local restructuring. `requestRow` selects Request ID if present, otherwise `columns[0]`; decorate exactly that cell after its ordinary rendering. Keep `requestKey`, `data-request-toggle`, aria labels, `aria-controls`, child IDs, expansion set, focus restoration, and refresh behavior unchanged.
- Use the existing `requestCell` renderers and DOM nodes; never reconstruct fallback content using `textContent` or `innerHTML` (would erase status badges). Access the just-appended cell via the row rather than requiring all cell helper return signatures to change. Remove obsolete `requestIDCell` implementation if replaced; no alias.
- Prefix child fallback cells with a text-bearing span `Attempt N` and spacing, retaining the underlying timestamp/model/status/number. Use the existing child indentation class or minimally rename its local CSS use if necessary; avoid restyling.
- Preserve no-children rows with no toggle, historical direct one-child expansion, empty-state colspan, single-visible-column behavior, keyboard activation, and independent keys `id:1234` versus `legacy:1234`.
- `dashboard/usage/panel.html:100`: replace the incorrect universal inbound-request hierarchy claim with: `Routed attempts expand within legacy router groups. Request ID and API path are available only from historical records; stock CPA usage callbacks cannot populate them reliably.`
- `dashboard/shared.js:22-25`: intentionally unchanged defaults and column order. No new dependency, column, or preference constant.

### 3. Update behavioral verification and contract docs

- `tests/e2e/dashboard.test.mjs:197-254`: retain historical fixtures and existing expansion checks. Explicitly open `#request-columns` before checkbox interaction. Replace the forced-disabled assertion with hiding/restoring Request ID and API path; verify toggle relocation, no lost cell value, child labels, expansion/focus across rerender and refresh, persistence after preference reload, and legacy groups. Exercise a formatted status cell as the only visible column: badge survives, toggle still works, last checkbox cannot be unchecked. Restore selections before unrelated test sections. Keep fixture semantics explicitly historical, not proof of live host identity.
- Add a small dependency-free `node:test` file `tests/dashboard/usage-columns.test.mjs`, using the existing VM source-loading convention, to cover ordinary Request ID hiding, all-hidden UI fallback, and last-visible guard where practical without recreating a fake DOM framework. DOM behavior is proved by assembled-page smoke/e2e, not source-string assertions.
- `/tmp/jsdom-smoke/run.mjs:124-129,242-284`: update only this disposable harness's obsolete forced-visible expectations. Begin with Request ID hidden; assert absent header and usable fallback toggle; exercise hiding API path, restoring ID, child data preservation, key collisions, refresh and preference PUT. Reassemble HTML from current sources using the existing Go overlay exporter; do not trust the stale HTML file. Keep harness outside repository.
- `management_test.go:137,176`: preserve unrelated group-default and function-boundary anchors; do not add source-string assertions for the new behavior.
- `README.md:108-112` and `docs/usage-tracking.md:137,188-190,208,233`: document historical-only Request ID/API path, ordinary hideable columns, unchanged defaults/saved choices, fallback control placement, and stock callback limitation. Do not describe legacy router groups as exact inbound requests. Requested model may also come from historical sessions; do not invent a new binding or broaden default changes.
- Add `docs/adr/0005-stock-cpa-usage-identity-limits.md` and `docs/bdr/0005-hideable-historical-request-columns.md` using current repository record format, recording the evidence, policy, UI behavior, and all-hidden validation change. Mark ADR/BDR 0004 superseded with relative links; preserve their old decision text as history. ADR/BDR 0003 legacy grouping remains valid.

## Verification and acceptance

Run in the feature worktree after implementation; never run integration against live user state.

- `gofmt -w usage_preferences.go usage_store.go usage_store_test.go usage_management_test.go` (only actually edited Go files), then `gofmt -l *.go` must be empty.
- `go test ./... -count=1` and `go vet ./...`.
- `node --test tests/dashboard/usage-columns.test.mjs`.
- Assembled dashboard smoke: `HIERARCHY_SMOKE_HTML=/tmp/jsdom-smoke/dashboard.html go test -overlay /tmp/jsdom-smoke/overlay.json -run '^TestExportHierarchySmokeDashboard$' -count=1`, then `node /tmp/jsdom-smoke/run.mjs`.
- Real browser on an isolated CPA instance: set `DASHBOARD_URL`, `DASHBOARD_MANAGEMENT_KEY`, `DASHBOARD_E2E_ALLOW_MUTATIONS=1`, and `PLAYWRIGHT_CHROMIUM_EXECUTABLE`, then `npm run test:e2e`. Check narrow and desktop layouts and visible focus. This suite mutates config/state: loopback disposable instance only.
- Acceptance: saved hidden ID/path survive save/load/reopen; all-hidden save fails without data loss; historical values are unchanged when revealed; parent/child controls remain accessible with ID hidden and with one arbitrary column visible; no status/model/time data is replaced; no extra network fetch on toggle; legacy and historical keys never collide; accounting and ABI behavior remain unchanged.

### Known baseline and contingencies

- Observed baseline `go test ./... -count=1` passed (linker warned about macOS 13 object targeting macOS 12).
- Observed `node --test tests/dashboard/routes.test.mjs`: six pass, two fail: `GET config -> unedited save builds exact PATCH payload` and `toggling enabled changes only the enabled field of the PATCH body`, both `DEFAULT_FALLBACK_ON_STATUS is not defined`. Do not rerun to rediscover or silently fix this unrelated fixture issue. Report it as an existing blocker to claiming an all-green JS suite.
- Chromium is unavailable locally on macOS 12; prior CPA integration attempts failed before readiness or timed out. Do not repeat these known failures merely to confirm them. Use an already available supported isolated runner if accessible. Otherwise perform the jsdom assembled-page smoke and explicitly report browser/layout/live-CPA verification blocked; jsdom is not visual or host integration proof.
- Do not claim new live Request ID/API path binding from mocked historical fixtures. No live identity population is expected.
- Preserve all history and pricing/preferences data; no migration, reset, retention change, SDK upgrade, or accounting change. Reverting this UI correction restores old forced visibility but does not change stored records.

## Execution and repository handling

Planning remains read-only except this local plan artifact. No implementation or repository state changes before approval.

At execution, persist the approved plan per workflow, preserving prior branch provenance comments. Use the requested feature worktree, do not switch the marketplace checkout or touch CPA worktrees. The existing feature HEAD was `0332760`; PR #4 already merged remotely. Do not reimplement its cutover or assume local main contains the remote merge. Resolve branch naming from the approval title and existing workflow rather than inventing a slug; do not reuse historical pending execution instructions from the superseded fork plan.

Drive implementation and publication through todo tracking. The user additionally requests: create a PR, then push the patch-version tag and create the release. Always pass `--repo momodding/cpa-model-combo` to manual `gh` commands. Do not merge the PR without separate authorization. Do not claim CI for a new change using the old successful SHA. Report verification blockers without suppressing them.

## Patch release after PR creation

### Release contract and version

- Latest published stable release, verified through `gh release list --repo momodding/cpa-model-combo --limit 5 --json tagName,isDraft,isPrerelease,publishedAt`, is `v1.0.2`. Target **v1.0.3**.
- Immediately before publication, repeat the release listing and inspect remote tags. If a newer stable release has appeared, use its major/minor and increment its patch by one. If the resulting tag already exists without a published stable release, stop and report the collision; never overwrite or delete a tag or silently skip a reserved version.
- `.github/workflows/release.yml:3-6,57-77` already triggers on `v*` tags, checks out the tagged commit, derives VERSION by stripping `v`, and passes that version to `.github/xgo/release.sh`. The script injects `main.pluginVersion` using linker flags. Release numbering is tag-driven: retain the existing source fallback and historical test/build fixture versions; no broad version-string replacement is required to publish the patch.
- `.github/workflows/release.yml:86-135` already creates the GitHub release with generated notes after verifying five native ZIPs and checksums. Reuse it; do not race it with a separate empty `gh release create` call, and do not change the packaging workflow.

### Ordered publication steps

1. Complete implementation verification first. Run `gofmt -l *.go` before any push; output must be empty. Known required verification failures or missing required runtime proof block publication; report the exact blocker rather than presenting jsdom as browser proof. Do not bypass the repository verification gate to satisfy the release request.
2. Push only the working branch to remote `marketplace`, never `origin`. Create a PR with `gh pr create --repo momodding/cpa-model-combo --base main --head <work-branch> --title "Fix historical request column visibility" --body-file <prepared-body-file>`. The body records plugin-only limits, behavior changes, actual verification evidence, and any nonblocking known baseline failures. If a PR already exists for the same head/base, reuse it rather than opening a duplicate.
3. Capture the PR head SHA with `gh pr view <number> --repo momodding/cpa-model-combo --json headRefOid,url`. Wait for all required checks on that exact SHA with `gh pr checks <number> --repo momodding/cpa-model-combo --watch`. Failed checks stop tagging. Do not merge remotely or tag the old main commit: the user requested publication after PR creation, so publish the verified PR head.
4. Recheck release/tag availability, then create an annotated tag at the verified PR head: `git tag -a v1.0.3 <verified-pr-head-sha> -m "Model Combo v1.0.3"`, substituting the patch version determined above if stable releases advanced. Push only this tag: `git push marketplace refs/tags/v1.0.3`. Do not use `--tags` or force. If an existing local tag points elsewhere, stop; if it points to the intended commit but was never pushed, reuse it after checking its target.
5. Find the triggered release run using `gh run list --repo momodding/cpa-model-combo --workflow release.yml --commit <verified-pr-head-sha> --json databaseId,headSha,headBranch,status,conclusion,event`; select the tag-push run for this tag. Watch it using `gh run watch <run-id> --repo momodding/cpa-model-combo --exit-status`. If no run starts, verify Actions/tag-trigger availability and report the blocker; do not create an assetless substitute release. A failed release run is not completion: inspect its logs and report the failure without moving the tag.
6. Verify publication with `gh release view v1.0.3 --repo momodding/cpa-model-combo --json tagName,isDraft,isPrerelease,url,assets`. It must be a published non-prerelease with `checksums.txt` and exactly five ZIP assets named `model-combo_1.0.3_<platform>.zip`, for linux_amd64, linux_arm64, darwin_amd64, darwin_arm64, and windows_amd64. The release workflow verifies each ZIP contains exactly one `model-combo-v1.0.3.so`, `.dylib`, or `.dll` as appropriate and verifies checksums. Confirm the remote tag resolves to the verified PR head. Substitute the selected version consistently throughout these checks.
7. Deliver the PR URL, tag, tagged SHA, release URL, successful release run, and actual verification results. State explicitly that the release was published from the PR head while the PR remains unmerged. Existing historical releases and tags remain untouched.

The release can become public before PR review/merge because the user requested this order. Publishing does not imply that new live Request ID/API path population is supported.
