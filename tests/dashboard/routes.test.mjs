import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';
import { test } from 'node:test';

const here = dirname(fileURLToPath(import.meta.url));
const source = readFileSync(join(here, '../../dashboard/configuration/routes.js'), 'utf8');

const { normalizeRoute, serializeRoutes, validateRoutes, configFromResponse, configurationPatchBody, validateQuotaPolling } = vm.runInNewContext(
  `${source}\n({ normalizeRoute, serializeRoutes, validateRoutes, configFromResponse, configurationPatchBody, validateQuotaPolling })`,
  { DEFAULT_FALLBACK_ON_STATUS: '401, 402, 403, 404, 408, 429, 500, 502, 503, 504' },
);

const targets = [{ model: 'gpt-a', weight: 2 }];

test('GET normalization keeps random unchanged', () => {
  const route = normalizeRoute({ alias: 'r', strategy: 'random', cooldown_seconds: 30, targets }, 0);
  assert.equal(route.strategy, 'random');
  assert.equal(route.cooldown_seconds, '30');
  assert.equal('success_rate_threshold' in route, false);
});

test('GET normalization keeps last_known_good policy values unchanged', () => {
  const route = normalizeRoute({
    alias: 'r',
    strategy: 'last_known_good',
    cooldown_seconds: 60,
    success_rate_threshold: 0.75,
    minimum_samples: 5,
    observation_window_minutes: 15,
    targets,
  }, 0);
  assert.equal(route.strategy, 'last_known_good');
  assert.equal(route.success_rate_threshold, '0.75');
  assert.equal(route.minimum_samples, '5');
  assert.equal(route.observation_window_minutes, '15');
});

test('unedited last_known_good route serializes its stored values unchanged', () => {
  const loaded = normalizeRoute({
    alias: 'r',
    strategy: 'last_known_good',
    cooldown_seconds: 60,
    success_rate_threshold: 0.75,
    minimum_samples: 5,
    observation_window_minutes: 15,
    targets,
  }, 0);
  const [payload] = serializeRoutes([loaded]);
  assert.equal(payload.strategy, 'last_known_good');
  assert.equal(payload.success_rate_threshold, 0.75);
  assert.equal(payload.minimum_samples, 5);
  assert.equal(payload.observation_window_minutes, 15);
  assert.deepEqual([...validateRoutes(serializeRoutes([loaded]))], []);
});

test('all five strategies validate', () => {
  for (const strategy of ['priority', 'round-robin', 'random', 'last_known_good', 'headroom']) {
    const route = normalizeRoute({ alias: 'r-' + strategy, strategy, cooldown_seconds: 60, targets }, 0);
    const payload = serializeRoutes([route]);
    assert.deepEqual([...validateRoutes(payload)], [], strategy);
  }
});

test('invalid last_known_good policy is rejected', () => {
  const base = { alias: 'r', strategy: 'last_known_good', cooldown_seconds: 60, targets };
  const cases = [
    [{ success_rate_threshold: 0 }, /success rate threshold/],
    [{ success_rate_threshold: 1.5 }, /success rate threshold/],
    [{ minimum_samples: 0 }, /minimum samples/],
    [{ observation_window_minutes: 0 }, /observation window/],
  ];
  for (const [override, message] of cases) {
    const route = normalizeRoute({ ...base, success_rate_threshold: 0.9, minimum_samples: 20, observation_window_minutes: 60, ...override }, 0);
    const errors = validateRoutes(serializeRoutes([route]));
    assert.equal(errors.length, 1, JSON.stringify(override));
    assert.match(errors[0], message);
  }
});

test('unknown strategy still rejected', () => {
  const route = normalizeRoute({ alias: 'r', strategy: 'lottery', cooldown_seconds: 60, targets }, 0);
  assert.equal(route.strategy, 'priority');
  const errors = validateRoutes([{ ...serializeRoutes([route])[0], strategy: 'lottery' }]);
  assert.match(errors.join('\n'), /choose a strategy/);
});

test('GET config -> unedited save builds exact PATCH payload', () => {
  const remote = {
    enabled: true,
    priority: 3,
    routes: [
      { alias: 'rand', strategy: 'random', cooldown_seconds: 30, targets: [{ model: 'm-a', weight: 1 }] },
      { alias: 'lkg', strategy: 'last_known_good', cooldown_seconds: 60, success_rate_threshold: 0.75, minimum_samples: 5, observation_window_minutes: 15, targets: [{ model: 'm-b', weight: 2 }] },
    ],
  };
  const loaded = configFromResponse(remote);
  const body = configurationPatchBody(loaded);
  assert.deepEqual(JSON.parse(JSON.stringify(body)), {
    enabled: true,
    priority: 3,
    routes: [
      { alias: 'rand', strategy: 'random', cooldown_seconds: 30, targets: [{ model: 'm-a', weight: 1 }] },
      { alias: 'lkg', strategy: 'last_known_good', cooldown_seconds: 60, targets: [{ model: 'm-b', weight: 2 }], success_rate_threshold: 0.75, minimum_samples: 5, observation_window_minutes: 15 },
    ],
    fallback: { enabled: true, fallback_on_status: [401, 402, 403, 404, 408, 429, 500, 502, 503, 504], no_fallback_on_status: [], retry_codes: [], no_retry_codes: [], max_attempts: 0, stream_fallback_before_payload: true },
    quota_polling: { refresh_seconds: 60, idle_seconds: 600, ttl_seconds: 60, max_stale_seconds: 300, fetch_timeout_seconds: 5, max_concurrent: 3, cold_wait_ms: 1500, default_unknown_quota_percent: 50 },
    'model-routes': null,
  });
  assert.deepEqual([...validateRoutes(body.routes)], []);
});

test('toggling enabled changes only the enabled field of the PATCH body', () => {
  const remote = {
    enabled: true,
    priority: 7,
    routes: [
      { alias: 'lkg', strategy: 'last_known_good', cooldown_seconds: 45, success_rate_threshold: 0.8, minimum_samples: 10, observation_window_minutes: 30, targets: [{ model: 'm-b', weight: 1 }] },
    ],
  };
  const state = configFromResponse(remote);
  const before = configurationPatchBody(state);
  state.enabled = false;
  const after = configurationPatchBody(state);
  assert.equal(before.enabled, true);
  assert.equal(after.enabled, false);
  assert.deepEqual(JSON.parse(JSON.stringify({ ...after, enabled: true })), JSON.parse(JSON.stringify(before)));
  assert.equal(after.priority, 7);
  assert.deepEqual(JSON.parse(JSON.stringify(after.routes)), JSON.parse(JSON.stringify(before.routes)));
  assert.equal(after.routes[0].success_rate_threshold, 0.8);
  assert.equal(after.routes[0].minimum_samples, 10);
  assert.equal(after.routes[0].observation_window_minutes, 30);
  assert.equal(after['model-routes'], null);
});

test('legacy account lists migrate independently and explicit empty overrides inheritance', () => {
  const route = normalizeRoute({ alias: 'r', allowed_accounts: ['second', 'first'], targets: [{ model: 'a' }, { model: 'b', allowed_accounts: [] }, { model: 'c', allowed_accounts: ['third'] }] }, 0);
  const payload = JSON.parse(JSON.stringify(serializeRoutes([route])[0]));
  assert.equal('allowed_accounts' in payload, false);
  assert.deepEqual(payload.targets.map(target => target.allowed_accounts || []), [['second', 'first'], [], ['third']]);
  route.targets[0].allowed_accounts.push('extra');
  assert.deepEqual(Array.from(route.targets[2].allowed_accounts), ['third']);
  const legacy = normalizeRoute({ alias: 'r', models: ['a', 'b'], allowed_accounts: ['first'] }, 0);
  assert.deepEqual(JSON.parse(JSON.stringify(serializeRoutes([legacy])[0].targets.map(target => target.allowed_accounts))), [['first'], ['first']]);
});

test('invalid target account lists are rejected with target paths', () => {
  for (const accounts of [null, [''], [42], ['a', ' a ']]) {
    const route = normalizeRoute({ alias: 'r', targets: [{ model: 'a', allowed_accounts: accounts }] }, 0);
    const errors = validateRoutes(serializeRoutes([route]));
    assert.ok(errors.some(error => error.includes('routes[0].targets[0].allowed_accounts')));
  }
});

test('target eligibility respects unknown, disabled and known model support', () => {
  const tab = readFileSync(join(here, '../../dashboard/configuration/tab.js'), 'utf8');
  const state = { accounts: [], catalogLoaded: true, priority: '0', fallback: {}, quota_polling: configFromResponse({}).quota_polling };
  const context = { state, routesEl: { addEventListener() {} }, validateRoutes, validateFallback: () => [], validateQuotaPolling, Set, Map };
  const { validateClient, modelsForRoute } = vm.runInNewContext(`${tab}\n({ validateClient, modelsForRoute })`, context);
  const route = { alias: 'r', strategy: 'priority', cooldown_seconds: 60, targets: [{ model: 'wanted', weight: 1, allowed_accounts: ['one'] }] };
  state.accounts = [{ id: 'one', models: [], model_support_unknown: true }, { id: 'two', models: [{ id: 'discovered' }] }];
  assert.equal(validateClient([route]).length, 0);
  assert.equal(modelsForRoute(route.targets[0]).get('discovered'), 'support unknown');
  state.accounts[0].model_support_unknown = false;
  assert.ok(validateClient([route]).some(error => error.includes('does not support')));
  state.accounts[0].disabled = true;
  assert.ok(validateClient([route]).some(error => error.includes('unavailable')));
});

test('configuration snapshot tracks target and account order', () => {
  const tab = readFileSync(join(here, '../../dashboard/configuration/tab.js'), 'utf8');
  const state = { enabled: true, priority: '0', fallback: {}, routes: [normalizeRoute({ alias: 'r', targets: [{ model: 'a', allowed_accounts: ['b', 'a'] }, { model: 'b', allowed_accounts: [] }] }, 0)] };
  const { configurationSnapshot } = vm.runInNewContext(`${source}\n${tab}\n({ configurationSnapshot })`, { state, routesEl: { addEventListener() {} } });
  const original = configurationSnapshot();
  state.routes[0].targets[0].allowed_accounts.reverse();
  assert.notEqual(configurationSnapshot(), original);
  state.routes[0].targets[0].allowed_accounts.reverse();
  state.routes[0].targets.reverse();
  assert.notEqual(configurationSnapshot(), original);
  const saved = JSON.parse(JSON.stringify(serializeRoutes(state.routes)));
  const reloaded = normalizeRoute(saved[0], 0);
  assert.deepEqual(JSON.parse(JSON.stringify(reloaded.targets.map(target => [target.model, target.allowed_accounts]))), [['b', []], ['a', ['b', 'a']]]);
});

test('headroom policies and polling settings round trip and validate', () => {
  const loaded = configFromResponse({ routes: [{ alias: 'r', strategy: 'headroom', unknown_quota: 'exclude', targets }], quota_polling: { ttl_seconds: 20, max_stale_seconds: 30 } });
  let body = configurationPatchBody(loaded);
  assert.equal(body.routes[0].unknown_quota, 'exclude');
  assert.equal(body.quota_polling.ttl_seconds, 20);
  assert.deepEqual([...validateRoutes(body.routes)], []);
  assert.deepEqual([...validateQuotaPolling(loaded.quota_polling)], []);
  loaded.quota_polling.ttl_seconds = '31';
  assert.match(validateQuotaPolling(loaded.quota_polling).join(' '), /TTL/);
  loaded.quota_polling.ttl_seconds = '20';
  for (const bad of ['', '-1', '100.1', 'abc']) {
    loaded.quota_polling.default_unknown_quota_percent = bad;
    assert.match(validateQuotaPolling(loaded.quota_polling).join(' '), /unknown quota score/, bad);
  }
  loaded.quota_polling.default_unknown_quota_percent = '35.5';
  assert.deepEqual([...validateQuotaPolling(loaded.quota_polling)], []);
  assert.equal(configurationPatchBody(loaded).quota_polling.default_unknown_quota_percent, 35.5);
  loaded.quota_polling.ttl_seconds = '0';
  assert.match(validateQuotaPolling(loaded.quota_polling).join(' '), /positive integer/);
  loaded.routes[0].strategy = 'priority';
  body = configurationPatchBody(loaded);
  assert.equal('unknown_quota' in body.routes[0], false);
});
