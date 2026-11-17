import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';
import { test } from 'node:test';

const here = dirname(fileURLToPath(import.meta.url));
const tables = readFileSync(join(here, '../../dashboard/usage/tables.js'), 'utf8');
const tab = readFileSync(join(here, '../../dashboard/usage/tab.js'), 'utf8');

function metricHarness() {
  const values = new Map();
  const context = {
    usageState: { overview: null },
    document: { getElementById(id) { return values.get(id) || (values.set(id, { textContent: '', style: { removeProperty() {} }, classList: { remove() {}, add() {} }, clientWidth: 0, scrollWidth: 0 }), values.get(id)); } },
    requestAnimationFrame() {},
    formatCompact: value => `compact:${value}`,
    formatUSD: value => `usd:${value}`,
    formatNumber: value => `number:${value}`,
    effectiveCacheReadValue: value => Number(value.effective_cache_read_tokens || 0),
  };
  context.metricFitFrame = 0;
  context.scheduleMetricFit = () => {};
  const api = vm.runInNewContext(`${tab}\n({ renderUsageSummary })`, context);
  return { ...api, values, state: context.usageState };
}

test('usage summary renders valid zero values and empty model lists distinctly from unavailable', () => {
  const { renderUsageSummary, values, state } = metricHarness();
  state.overview = {
    summary: { total_tokens: 0, input_tokens: 0, output_tokens: 0, requests: 0, failed_requests: 0 },
    costs: { total_usd: 0, priced_requests: 0, requests: 0 },
    router_models: [], provider_models: [],
  };
  renderUsageSummary();
  assert.equal(values.get('metric-tokens').textContent, '0');
  assert.match(values.get('metric-cost').textContent, /0\.00/);
  assert.equal(values.get('metric-requests').textContent, '0');
  assert.equal(values.get('metric-router').textContent, 'None');
  assert.equal(values.get('metric-provider').textContent, 'None');
  state.overview = { summary: null, costs: null, router_models: null, provider_models: null };
  renderUsageSummary();
  assert.deepEqual(['metric-tokens', 'metric-cost', 'metric-requests', 'metric-router', 'metric-provider'].map(id => values.get(id).textContent), ['—', '—', '—', '—', '—']);
});

test('full dashboard render refreshes usage summary exactly once', () => {
  let count = 0;
  const context = {
    renderUsageSummary() { count++; }, renderUsageFilterOptions() {}, renderGroupTable() {}, renderRequestTable() {}, renderUsageCharts() {},
  };
  const functionSource = tables.match(/function renderUsageDashboard\(\)\{[\s\S]*?\n\}/)?.[0];
  assert.ok(functionSource);
  vm.runInNewContext(`${functionSource}\nrenderUsageDashboard()`, context);
  assert.equal(count, 1);
});

function refreshHarness(responses) {
  const calls = { dashboard: 0, summary: 0, status: [] };
  const element = () => ({ setAttribute() {}, hidden: true, disabled: false });
  const context = {
    usageState: { active: true, timer: 0, filterTimer: 0, generation: 0, group: { offset: 0 }, request: { offset: 0 }, overview: { sentinel: 'prior' }, range: 'custom', attribution: '', routerModel: '', providerModel: '', source: '', serviceTier: '', result: '' },
    document: { hidden: false },
    window: { clearTimeout() {} },
    AbortController,
    URLSearchParams,
    USAGE_API: '/usage',
    usageStatusEl: { textContent: '', dataset: {} },
    usagePanelEl: element(), usageRefreshEl: element(), usageLiveEl: element(),
    renderUsageDashboard() { calls.dashboard++; },
    requestManagementJSON: (...args) => responses.shift()(...args),
  };
  const api = vm.runInNewContext(`${tab}
resolvedUsageRange = () => ({ label: "range" }); usageBaseQuery = () => new URLSearchParams(); appendUsageTableQuery = () => {}; scheduleUsagePoll = () => {};
({ refreshUsage })`, context);
  return { ...api, calls, state: context.usageState, status: context.usageStatusEl };
}

const fullResult = (tokens) => ({ overview: { summary: { total_tokens: tokens } }, groups: {}, requests: {}, generated_at: new Date().toISOString() });

test('full refresh renders the dashboard once and replaces overview data', async () => {
  const { refreshUsage, calls, state } = refreshHarness([() => Promise.resolve(fullResult(7))]);
  await refreshUsage(false);
  assert.equal(calls.dashboard, 1);
  assert.equal(state.overview.summary.total_tokens, 7);
});

test('failed refresh keeps the previous overview and reports the failure', async () => {
  const { refreshUsage, calls, state, status } = refreshHarness([() => Promise.reject(new Error('boom'))]);
  await refreshUsage(false);
  assert.equal(calls.dashboard, 0);
  assert.equal(state.overview.sentinel, 'prior');
  assert.match(status.textContent, /Refresh failed: boom/);
  assert.equal(status.dataset.tone, 'error');
});

test('a superseded response cannot overwrite newer data', async () => {
  let resolveFirst;
  const first = new Promise((resolve) => { resolveFirst = resolve; });
  const { refreshUsage, state } = refreshHarness([() => first, () => Promise.resolve(fullResult(2))]);
  const stale = refreshUsage(false);
  await refreshUsage(false);
  resolveFirst(fullResult(1));
  await stale;
  assert.equal(state.overview.summary.total_tokens, 2);
});

test('initial load waits for preferences before refreshing', async () => {
  let finish;
  const preferences = new Promise(resolve => { finish = resolve; });
  const calls = [];
  const context = { usageState: { active: true, initialized: false }, document: { hidden: false }, usageLiveEl: {}, load: () => preferences, calls };
  const api = vm.runInNewContext(`${tab}\nloadUsagePreferences = load; refreshUsage = reset => calls.push(reset); setUsageStatus = () => {}; ({startUsageDashboard})`, context);
  api.startUsageDashboard();
  api.startUsageDashboard();
  assert.equal(calls.length, 0);
  finish();
  await preferences;
  await Promise.resolve();
  assert.deepEqual(calls, [false]);
});

test('polling schedules a refresh only while active and visible', () => {
  let callback;
  const calls = [];
  const context = { usageState: { active: true }, document: { hidden: false }, calls, window: { clearTimeout() {}, setTimeout(fn, delay) { callback = fn; assert.equal(delay, 15000); return 1; } } };
  const api = vm.runInNewContext(`${tab}\nrefreshUsage = reset => calls.push(reset); ({scheduleUsagePoll})`, context);
  api.scheduleUsagePoll();
  callback();
  assert.deepEqual(calls, [false]);
  context.document.hidden = true;
  callback = null;
  api.scheduleUsagePoll();
  assert.equal(callback, null);
});

test('filter changes cancel stale work and schedule a full refresh', () => {
  let callback, aborted = false;
  const calls = [];
  const state = { group: { offset: 5 }, request: { offset: 7 }, generation: 0, controller: { abort() { aborted = true; } } };
  const context = { usageState: state, calls, clearChartActive() {}, window: { clearTimeout() {}, setTimeout(fn, delay) { callback = fn; assert.equal(delay, 250); return 1; } } };
  const api = vm.runInNewContext(`${tab}\nrefreshUsage = reset => calls.push(reset); ({usageControlChanged})`, context);
  api.usageControlChanged();
  assert.equal(aborted, true);
  assert.equal(state.pendingFullRefresh, true);
  assert.equal(state.generation, 1);
  assert.equal(state.group.offset, 0);
  assert.equal(state.request.offset, 0);
  callback();
  assert.deepEqual(calls, [false]);
});

test('only a pending filter change upgrades a partial refresh to the full dashboard request', async () => {
  for (const [pending, path] of [[true, '/usage/dashboard?'], [false, '/usage/overview?']]) {
    const urls = [];
    const { refreshUsage, calls, state } = refreshHarness([(url) => { urls.push(url); return Promise.resolve(fullResult(3)); }]);
    const filterKey = JSON.stringify([state.range, state.customFrom, state.customTo, state.attribution, state.routerModel, state.providerModel, state.source, state.serviceTier, state.result]);
    Object.assign(state, { displayedRange: { label: 'range' }, displayedFilterKey: filterKey, pendingFullRefresh: pending });
    await refreshUsage(false, 'overview');
    assert.ok(urls[0].startsWith(path), `pending=${pending}: ${urls[0]}`);
    assert.equal(calls.dashboard, pending ? 1 : 0);
  }
});
