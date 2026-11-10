# 07. Manual CPA Verification

## 1. Scope and evidence rules

This procedure verifies routing-only behavior of `cpa-model-combo` inside a user-managed CLIProxyAPI (CPA) host.

It excludes accounts, OAuth, credentials, provider setup, usage, pricing, and production deployment.

Run every command in disposable paths. Do not use existing CPA processes or existing configuration.

Record each result as `PASS`, `FAIL`, or `BLOCKED`. Tests remain unverified until user executes this procedure.

The procedure does not authorize reading credentials or calling `host.auth.get`.

## 2. Supported host, operating system, and architecture

Build plugin for same operating system and CPU architecture as CPA host. Go `c-shared` libraries are not portable across operating systems or CPU targets.

This repository is built against CPA v7.3.9. The current verification target is exactly CPA v7.3.9; later versions remain unverified.

The host must support native plugins, `model_router`, `executor`, `model_registrar`, request interception, `usage_plugin`, and `host.model.*` callbacks.

Current local artifact evidence covers macOS amd64 only:

```text
dist/cpa-model-combo.dylib: Mach-O 64-bit dynamically linked shared library x86_64
```

Linux `.so` and Windows `.dll` artifacts require native builds on matching targets. No Linux or Windows support claim is made here.

## 3. Native build

From repository root, run:

```sh
make build
file dist/cpa-model-combo.dylib
nm -gU dist/cpa-model-combo.dylib | grep -E '(_cliproxy_plugin_init|_ModelRouterPlugin(Call|Free|Shutdown))'
```

Expected macOS exports are:

```text
_cliproxy_plugin_init
_ModelRouterPluginCall
_ModelRouterPluginFree
_ModelRouterPluginShutdown
```

Run repository checks before manual verification:

```sh
make fmt-check
go vet ./...
go build ./...
go test ./... -count=1
go test -race ./... -count=1
```

A macOS deployment-target linker warning is non-fatal only when build commands still succeed. Record warning text.

## 4. Filename and plugin ID

Use canonical plugin ID `cpa-model-combo`.

Use the host-native extension and preserve the basename:

```text
macOS:  cpa-model-combo.dylib
Linux:  cpa-model-combo.so
Windows: cpa-model-combo.dll
```

Do not rename the basename. Do not add a version suffix. CPA derives plugin ID from filename.

## 5. Disposable install

Set disposable paths. Replace `${CPA_BIN}` with user-built CPA v7.3.9 exactly; later versions remain unverified.

```sh
export CPA_BIN=/absolute/path/to/cpa
export CPA_ROOT="$(mktemp -d)"
export CPA_CONFIG="$CPA_ROOT/config.yaml"
export PLUGIN_DIR="$CPA_ROOT/plugins"
mkdir -p "$PLUGIN_DIR"
cp dist/cpa-model-combo.dylib "$PLUGIN_DIR/cpa-model-combo.dylib"
```

Create `$CPA_CONFIG` with an absolute plugin directory and routing-only configuration:

```yaml
host: 127.0.0.1
port: 18317
auth-dir: "/absolute/path/to/disposable-auth"
api-keys: []
plugins:
  enabled: true
  dir: "/absolute/path/to/disposable-root/plugins"
  configs:
    cpa-model-combo:
      enabled: true
      priority: 100
      data_path: "/absolute/path/to/disposable-root/cpa-model-combo.db"
      routes:
        - alias: auto
          strategy: priority
          cooldown_seconds: 60
          targets:
            - model: target-a
              weight: 1
            - model: target-b
              weight: 1
```

Use paths matching actual disposable directories. Do not put credentials or provider keys in this configuration.

## 6. CPA startup and readiness verification

Stop before this section if another process owns the chosen port. Start only disposable CPA:

```sh
set -eu
LOG="$CPA_ROOT/cpa.log"
"$CPA_BIN" -config "$CPA_CONFIG" >"$LOG" 2>&1 &
CPA_PID=$!
trap 'kill "$CPA_PID" 2>/dev/null || true; wait "$CPA_PID" 2>/dev/null || true' EXIT INT TERM

ready=0
for _ in $(seq 1 30); do
  if curl -fsS -o /dev/null "http://127.0.0.1:18317/"; then
    ready=1
    break
  fi
  sleep 1
done

if [ "$ready" -ne 1 ]; then
  echo "CPA readiness failed"
  cat "$LOG"
  exit 1
fi
```

Record CPA version, startup logs, selected port, and plugin-load lines. A readiness timeout is `BLOCKED` for endpoint checks.

## 7. Registration and canonical resource check

After readiness, verify model registration through the CPA dashboard or model-list surface.

Verify configured aliases appear, including `auto`. Record exact response status and body.

Check canonical plugin resource route:

```sh
curl -sS -D "$CPA_ROOT/resource.headers" -o "$CPA_ROOT/resource.body" \
  http://127.0.0.1:18317/v0/resource/plugins/model-combo/config
awk 'NR == 1 || tolower($0) ~ /^content-type:/' "$CPA_ROOT/resource.headers"
wc -c < "$CPA_ROOT/resource.body"
```

Record HTTP status, `Content-Type`, body length, and redacted body shape. Do not include credentials.

## 8. Dashboard URL and management checks

Open this URL in user browser only after CPA is ready:

```text
http://127.0.0.1:18317/
```

Verify cpa-model-combo route configuration appears in dashboard. Verify route strategy, targets, weights, cooldown, save, and reload

Management routes are protected when configured with a management secret. Test only with a safe disposable secret supplied by user. Do not discover, read, or reuse real secrets.

If management is disabled, record expected rejection or `404`; do not call it a routing success.

## 9. Routing smoke checks

Use disposable host/provider fixtures or CPA-supported test endpoints. Do not configure real providers.

Run these checks and record selected target, response model, status, and fallback result:

1. Priority selects first eligible target.
2. Failed target enters cooldown and is skipped.
3. Cooldown expiry permits target selection again.
4. Recognized quota, rate-limit, auth, provider, server, and transport failures fail over.
5. Round-robin distributes requests across targets.
6. Weighted round-robin gives higher-weight targets proportionally larger share.
7. CPA reload preserves cooldown and round-robin state.
8. Requested thinking suffix remains on selected target unless target defines its own suffix.
9. Alias response model is rewritten to requested alias.

Random strategy, last-known-good behavior, cloning semantics, and exclusion semantics are not established by current documentation evidence. Mark each `UNVERIFIED` unless user execution and source-supported configuration prove it.

## 10. Streaming checks

Run both streaming and non-streaming requests with disposable fixtures.

Verify:

- Streaming response model is rewritten to requested alias.
- Retry occurs only before first upstream payload.
- After first payload, routing is committed and no retry occurs.
- Request body remains available for any permitted pre-payload retry.
- Failure after commitment returns stream failure rather than silently switching target.

Record first-payload timing, selected target, retry count, final status, and response model. Stream behavior remains unverified until user executes these checks.

## 11. Known crash and limitations

A previous disposable macOS CPA attempt observed this crash during plugin loading/call:

```text
fatal error: unknown caller pc
_cgoexp_2f2761c2cc37_ModelRouterPluginCall
main.ModelRouterPluginCall
.../abi.go:164
.../dashboard.go:36
.../internal/pluginhost.(*guardedPluginClient).Call
.../internal/pluginhost/client_guard.go:33
.../internal/pluginhost.(*Host).waitForPluginLoad
.../internal/pluginhost/host.go:446
.../internal/pluginhost.(*Host).ApplyConfig
.../internal/pluginhost/host.go:294
```

This is crash evidence, not proof of successful plugin load or endpoint behavior. Stop verification after a crash, preserve the complete log, and mark registration, dashboard, routing, stream, and endpoint checks `BLOCKED`.

Known limitations:

- Current local artifact evidence covers macOS amd64 only.
- Linux and Windows artifacts are not verified here.
- No CPA endpoint result is guaranteed until user executes this procedure.
- No provider, account, OAuth, credential, or `host.auth.get` behavior is covered.

## 12. Troubleshooting and rollback

### Troubleshooting

- **Plugin not discovered:** verify `plugins.enabled`, absolute `plugins.dir`, exact `cpa-model-combo` basename, and host-native extension.
- **Wrong architecture:** rebuild on same OS and CPU architecture as CPA.
- **Missing exports:** rerun `nm` and verify all four required exports.
- **Readiness timeout:** preserve `$CPA_ROOT/cpa.log`; check port ownership and configuration syntax.
- **Alias absent:** verify `cpa-model-combo` config is enabled and registration logs contain no errors.
- **Dashboard route absent:** verify dashboard URL, plugin registration, and disposable config path.
- **Management rejection:** confirm disposable management secret and route protection; do not weaken real configuration.
- **Stream retry mismatch:** record whether first upstream payload already arrived; retries after commitment are not expected.
- **macOS `unknown caller pc`:** stop CPA, preserve complete crash log, mark host integration blocked, and do not infer endpoint results.

### Rollback

Stop disposable CPA and wait for process exit:

```sh
kill "$CPA_PID" 2>/dev/null || true
wait "$CPA_PID" 2>/dev/null || true
```

Remove only disposable root and plugin paths after preserving logs:

```sh
rm -rf "$CPA_ROOT"
```

Confirm no disposable process remains and chosen port is free. Do not stop existing CPA processes. Do not modify repository files, provider configuration, credentials, or containers.
