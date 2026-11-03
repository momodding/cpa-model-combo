# 05. Account Routing Contract

Status: audit contract. Describes what the pinned source verifiably does for account (auth) routing and what remains unverified.

## 1. Scope and Pinned Version

- Scope: host-side account selection for plugin-issued model execution, and the host auth callbacks plugins use to discover accounts.
- Pinned version: `v7.3.9`, commit `61fdfc34`, module `github.com/router-for-me/CLIProxyAPI/v7`, go 1.26.0.
- Source root for all citations: `.cpa-source/` (untracked, read-only). Paths below are relative to it.
- Out of scope: provider executors, upstream HTTP behavior, quota and credit logic, management API.
- Method: static source reading only. No tests run; no CPA behavioral matrix tests run or claimed as passed. No auth files, credential contents, or `host.auth.get` responses opened or read.

## 2. Contract Matrix

Legend: VERIFIED = behavior traced in source. PARTIAL = some behavior traced, remainder unknown. BLOCKED = required behavior not verified; must not ship as a guarantee.

| # | Contract item | Status | Evidence |
|---|---|---|---|
| C1 | Plugins can list accounts via `host.auth.list` | VERIFIED | §3.1, §3.2 |
| C2 | `host.auth.list` entries carry `id` (from `auth.ID`) and `auth_index` (from `auth.Index`) | VERIFIED | §3.2 |
| C3 | Plugin can pin execution to one account via `auth_id` in `HostModelExecutionRequest` | VERIFIED | §3.3 |
| C4 | Sync and stream model execution attach the pin to context and metadata | PARTIAL (sync context/metadata wiring VERIFIED; stream handler wiring traced at `sdk/api/handlers/model_execution.go:134-136`; plugin callback-to-`ExecuteModelStream` hop lacks direct current-source evidence) | §3.4 |
| C5 | Pin is carried as `PinnedAuthMetadataKey` in execution metadata | VERIFIED | §3.4, §3.5 |
| C6 | Conductor parses the pin from metadata | VERIFIED | §3.6 |
| C7 | Pin is honored by the auth pick path in non-home mode | BLOCKED | §4.1 (no pin consumer found); home unpinned branches §4.5 |
| C8 | Pin is honored by home dispatch (`pinnedAuthID` parameter) | PARTIAL | §4.2; unpinned branches §4.5 |
| C9 | Pin value matched to `auth.ID` or `auth.Index` | BLOCKED | §4.3 |
| C10 | Unknown pinned ID fails closed | BLOCKED | §4.4 |
| C11 | Disabled pinned account fails closed | BLOCKED | §4.4 |
| C12 | Cooling-down or unavailable pinned account fails closed | BLOCKED | §4.4 |
| C13 | Provider mismatch between pin and requested model fails closed | BLOCKED | §4.4 |
| C14 | No silent fallback to another account when pin is set | BLOCKED | §4.4, §4.5 |
| C15 | Callback surface exposes only the fields it documents | PARTIAL | §5 |

Overall: strict account restriction and silent-fallback behavior are BLOCKED. Home dispatch has unpinned call branches (§4.5), so source does not support strict pin enforcement. P2 is investigation-only (§6). Strict pin is BLOCKED (§7).

## 3. Verified Mechanics

### 3.1 Callback declarations

- `sdk/pluginabi/types.go`, const block, lines 95-110. `MethodHostAuthList = "host.auth.list"` (106), `host.auth.get` (107), `host.auth.get_runtime` (108), `host.auth.save` (109).

### 3.2 `host.auth.list` and related handlers

- `internal/pluginhost/auth_callbacks.go`
  - Request/response structs, lines 19-32: `rpcHostAuthGetRequest{auth_index}` (20-21); `rpcHostAuthListResponse{files}` (23-25, JSON key `files`); `rpcHostAuthGetResponse{auth_index, name, path, json}` (27-32).
  - `callHostAuthList`, lines 53-66. Exact invocation: empty or whitespace-only request bytes are accepted without decode. Non-empty request must decode as JSON into `map[string]any` (55-60); malformed or non-object JSON returns an error. Request keys are not read. Response: `marshalRPCResult(rpcHostAuthListResponse{Files: entries})` (65), i.e. `{"files": [HostAuthFileEntry, ...]}`.
  - `callHostAuthGet`, lines 68-93: returns raw auth record JSON via `authPhysicalJSONByIndex`. Credential-bearing; see §5. Not called in this audit.
  - `callHostAuthGetRuntime`, lines 95-114: uses `authByIndex` and `buildHostAuthFileEntry`. Not re-read in this update.
  - `callHostAuthSave`, lines 116-133. Not re-read in this update.
  - `listAuthFiles`, lines 135-151: uses `manager.List()` when a manager is set; otherwise falls back to `listAuthFilesFromDisk` (153+).
- `buildHostAuthFileEntry`, lines 392-491: `ID` = `auth.ID` (410); `AuthIndex` = `auth.Index` (411); `Name` falls back to `auth.ID` when `FileName` is empty (405-408); `Path` = `authAttribute(auth, "path")` (401). Entry omitted when runtime-only and disabled (398-400) or when not runtime-only and path is empty (402-404). `Source` hard-coded `"memory"` (421). Projection also sets `Success`, `Failed`, `Disabled`, `Unavailable`, `RuntimeOnly`, `Status`, `StatusMessage`, `Type`, `Provider`, `Label` (412-423). Lines 424-489 not read in this update.

### 3.3 Request type

- `sdk/pluginapi/types.go`, lines 610-632, `HostModelExecutionRequest`: `ForcedProvider` (628-629, `omitempty`), `AuthID` (630-631, JSON `auth_id`, `omitempty`).

### 3.4 Handler wiring

- `sdk/api/handlers/model_execution.go`
  - `ExecuteModel`, lines 99-123: if `req.AuthID != ""`, calls `WithPinnedAuthID(ctx, req.AuthID)` (104-106), then `executeWithAuthManagerFormats`.
  - `ExecuteModelStream`, lines 129-153 (read this update): if `req.AuthID != ""`, calls `WithPinnedAuthID(ctx, req.AuthID)` (134-136), then `executeStreamWithAuthManagerFormats` (137). Returns `ModelExecutionStream` built from chunks (145-153). Handler attachment is VERIFIED. End-to-end plugin stream propagation remains PARTIAL: plugin callback-to-`ExecuteModelStream` hop lacks direct current-source evidence. Downstream enforcement is not verified (§4).
- `sdk/api/handlers/handlers_context.go`
  - `WithPinnedAuthID`, lines 68-78: trims; stores value under `pinnedAuthContextKey{}`.
  - `pinnedAuthIDFromContext`, line 179+.
- `sdk/api/handlers/handlers.go`
  - `requestExecutionMetadata`, lines 196-227: when pin is non-empty, sets `coreexecutor.PinnedAuthMetadataKey` (220-221).

### 3.5 Metadata key

- `PinnedAuthMetadataKey` is set at `sdk/api/handlers/handlers.go:220-221` and read at `sdk/cliproxy/auth/conductor_execution.go:1679`.

### 3.6 Conductor parsing

- `sdk/cliproxy/auth/conductor_execution.go`, `pinnedAuthIDFromMetadata`, lines 1675-1691: accepts `string` or `[]byte`, returns empty otherwise.
- Non-test call sites observed: line 264 (home retry-round gate) and line 954 (home cooldown retry-limit gate). Both use the result only as `== ""`, to decide whether a home retry limit applies.

## 4. Unverified and Blocked Behavior

### 4.1 Non-home pick path (C7: BLOCKED)

- Grep of `sdk/cliproxy/auth/` for `pinned|Pinned` returned, in non-test code, only `conductor_execution.go` (264, 954, 1675-1679) and `conductor_home.go` (388, 398). Output was truncated; the full set was not confirmed.
- Not located: code that passes the parsed pin into the non-home selector or filters candidates by it.
- Conclusion: pin is parsed; enforcement in the normal pick path is NOT verified.

### 4.2 Home dispatch (C8: PARTIAL)

- `sdk/cliproxy/auth/conductor_home.go` interfaces accept `pinnedAuthID`: `homeDispatchSessionHierarchyDispatcher.RPopAuthWithSessionHierarchy` (388) and `homeDispatchConstraintsDispatcher.RPopAuthWithConstraints` (398).
- Verified (this update): dispatch sites at `conductor_home.go:1015-1035` pass `pinnedAuthID` to five interface variants (lines 1020, 1023, 1025, 1030, 1033).
- Not verified: concrete implementations enforce it, or behavior on mismatch. Unpinned call branches: see §4.5.

### 4.3 ID vs AuthIndex matching (C9: BLOCKED)

- Verified mapping: `HostAuthFileEntry.ID` maps `auth.ID`; `HostAuthFileEntry.AuthIndex` maps `auth.Index` (`internal/pluginhost/auth_callbacks.go`, 410-411).
- Verified: `HostModelExecutionRequest.AuthID` (`sdk/pluginapi/types.go`, 630-631) is passed through as the pin value (§3.4, §3.5).
- BLOCKED: which field the conductor compares the pin against (`auth.ID`, `auth.Index`, or both). No source read in this audit establishes the matching field.
- Consequence: a pin value taken from either field may fail to match or match unexpectedly. No field choice is recommended here.

### 4.5 Home dispatcher unpinned fallback (C7, C8, C14: BLOCKED)

- `sdk/cliproxy/auth/conductor_home.go` lines 1015-1035 (read this update).
- Unpinned branch A: when `credentialPolicy` is empty and the client implements neither `homeDispatchRetryRoundConstraintsDispatcher` nor `homeDispatchConstraintsDispatcher`, line 1027 calls `client.RPopAuth(ctx, requestedModel, sessionID, dispatchHeaders, homeAuthCountFromMetadata(opts.Metadata))`. This call has no pin argument. Pin dropped.
- Unpinned branch B: when `credentialPolicy` is set and the client is a policy dispatcher without constraints, line 1035 calls `policyClient.RPopAuthWithPolicy(...)`. This call has no pin argument. Pin dropped.
- No-match branch: clients implementing none of the listed interfaces return `home_unavailable` (1037-1039). No pin-related error.
- Consequence: source does not enforce the pin on home dispatch branches A and B. Do not claim strict pin enforcement. Concrete dispatcher implementations remain unverified (§4.2).

### 4.4 Failure semantics (C10 to C14: BLOCKED)

Not verified:
- unknown pinned ID (error vs. ignored);
- disabled account;
- cooling-down or unavailable account;
- provider mismatch (for example a pin to a non-matching provider for the requested model);
- whether the retry/fallback loop in `conductor_execution.go` (around lines 263 and 953) can select another account after the pin fails.

Treat any pin as possibly bypassable until traced end to end. Account restrictions cannot ship on this basis.

## 5. Privacy Boundary

1. `host.auth.list` returns `HostAuthFileEntry` records (`sdk/pluginapi/types.go`, struct from line 686). Fields read this update (687-722): `id`, `auth_index`, `name`, `type`, `provider`, `label`, `status`, `status_message`, `disabled`, `unavailable`, `runtime_only`, `source`, `path`, `size`, `modtime`, `updated_at`, `created_at`, `last_refresh`. Fields after line 722 not read. Projection mapping verified in §3.2. `path` (backing file path) and `name` expose local file identifiers; treat as sensitive metadata. No `Email`, `ProjectID`, `Account`, or `AccountType` field was verified in this update. Plugins MUST NOT forward these fields outside the host without a separate decision.
2. `host.auth.get` (`auth_callbacks.go`, 68-93) returns raw auth record JSON. This is credential-bearing material. Routing MUST NOT depend on it, and plugins MUST NOT call it for routing.
3. `host.auth.get_runtime` (95-114) returns the same entry shape as list. Not required for routing.
4. Routing plugins MUST NOT construct IDs, parse file names, or read auth directories to obtain pin values.
5. This audit did not open, read, or expose any credential file or auth record. Claims here come from reading callback code only. No claim is made that credentials were exposed or read during this audit.

## 6. P2 Scope: Investigation Only

P2 is investigation-only. It MUST NOT ship as a routing feature.

- Goal: confirm the matching field (§4.3) and the failure semantics (§4.4) by trace and by tests.
- P2 MUST NOT set `auth_id` in production routing decisions.
- P2 MUST NOT present the pin as enforced, advisory, or preferred to users or other plugins.
- P2 makes no change to host enforcement.
- Exit criterion: a documented, traced answer for each BLOCKED row in §2, with test evidence. Until then, no account restriction, pin, or fallback behavior is a shipping guarantee.

## 7. Explicit Strict-Pin Blocker

Strict pin (pinned execution uses the exact account or fails, with no fallback) MUST NOT ship. Blocking items:

- B1: non-home pick path does not visibly consume the pin (§4.1).
- B2: ID vs AuthIndex matching unresolved (§4.3).
- B3: failure semantics for unknown, disabled, cooling, unavailable, and provider-mismatch accounts unverified (§4.4).
- B4: silent fallback in the retry loop unverified (§4.4).
- B5: home dispatch branches drop `pinnedAuthID` (`RPopAuth` at `conductor_home.go:1027`, `RPopAuthWithPolicy` at `:1035`) (§4.5).

Unblock requires: end-to-end trace of the pick path with pin; tests demonstrating fail-closed behavior for each §4.4 case (tests not run in this audit); and a resolved matching-field decision backed by source. Until then, account restrictions cannot ship.

## 8. Verification Still Required

- Trace the non-home pick path for pinned-ID use (locate the selector/picker file and its pinned handling).
- Trace concrete implementations of `RPopAuthWithSessionHierarchy` and `RPopAuthWithConstraints`.
- Confirm the matching field for the pin (`ID` vs `Index`).
- Run existing and new tests for each §4.4 row.
