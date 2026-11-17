import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';
import { test } from 'node:test';

const here = dirname(fileURLToPath(import.meta.url));
const tables = readFileSync(join(here, '../../dashboard/usage/tables.js'), 'utf8');
const tab = readFileSync(join(here, '../../dashboard/usage/tab.js'), 'utf8');
const definitions = [{ key: 'request_id' }, { key: 'request_endpoint' }, { key: 'result' }];

function setup(hidden = []) {
  const usageState = { hiddenRequestColumns: new Set(hidden), hiddenGroupColumns: new Set() };
  const context = { usageState, requestColumns: definitions, groupColumns: definitions };
  const api = vm.runInNewContext(`${tables}\n${tab}\nrenderColumnControls = () => {}; renderRequestTable = () => {}; renderGroupTable = () => {}; scheduleUsagePreferencesSave = () => {};\n({ visibleColumns, validHiddenColumns, changeColumnVisibility })`, context);
  return { ...api, usageState };
}

test('request identity columns can be hidden like other columns', () => {
  const { visibleColumns, changeColumnVisibility, usageState } = setup();
  for (const key of ['request_id', 'request_endpoint']) {
    const input = { dataset: { columnKind: 'request', columnKey: key }, checked: false, closest() { return this; } };
    changeColumnVisibility({ target: input });
  }
  assert.deepEqual(Array.from(visibleColumns(definitions, usageState.hiddenRequestColumns), (column) => column.key), ['result']);
});

test('malformed all-hidden preferences reveal the first defined column', () => {
  const { validHiddenColumns, visibleColumns } = setup();
  const hidden = validHiddenColumns(['request_id', 'request_endpoint', 'result', 'unknown'], definitions);
  assert.deepEqual(Array.from(visibleColumns(definitions, hidden), (column) => column.key), ['request_id']);
  assert.deepEqual(Array.from(validHiddenColumns([], definitions)), []);
});

test('last visible column cannot be hidden by a change event', () => {
  const { changeColumnVisibility, usageState } = setup(['request_id', 'request_endpoint']);
  const input = { dataset: { columnKind: 'request', columnKey: 'result' }, checked: false, closest() { return this; } };
  changeColumnVisibility({ target: input });
  assert.equal(usageState.hiddenRequestColumns.has('result'), false);
});

test('cooling skips show safe routing details without execution accounting', () => {
  function element() {
    return {
      children: [], className: '', textContent: '',
      appendChild(child) { this.children.push(child); },
      append(child) { this.children.push(child); },
      prepend(child) { this.children.unshift(child); },
      get firstElementChild() { return this.children[0]; },
    };
  }
  const context = {
    document: { createElement: element },
    formatRequestTime: value => `time:${value}`,
    resultDisplay: value => value,
    usageState: { expandedRequests: new Set() },
  };
  const { requestRow, requestValue } = vm.runInNewContext(`${tables}\n({ requestRow, requestValue })`, context);
  const skip = { dispatched: false, status: 'cooling_down', order: 3, retry_at: 'later', provider_model: 'model', auth_id: 'secret', request_id: 'fake', total_tokens: 42, latency_ns: 123, estimated_cost: { priced: true, total_usd: 5 } };
  const columns = ['request_id', 'provider_model', 'credential', 'result', 'total_tokens', 'latency', 'cost'].map(key => ({ key }));
  const row = element();
  requestRow(row, skip, columns, { ordinal: 1, parent: { trace_id: 'trace' } });
  assert.equal(row.children[0].textContent, '—');
  assert.equal(row.children[0].children[0].textContent, 'Skipped 3');
  assert.equal(row.children[0].children[1].textContent, 'Retry at time:later');
  assert.equal(row.children[1].textContent, 'model');
  assert.equal(row.children[2].textContent, 'Unavailable');
  assert.equal(row.children[3].children[0].textContent, 'Cooling down');
  assert.equal(row.children[3].children[0].className, 'status-pill');
  assert.deepEqual(row.children.slice(4).map(cell => cell.textContent), ['—', '—', '—']);
  assert.equal(requestValue({ attempts: [skip] }, 'provider_model', false), 'No executions recorded');
  skip.credential_label = 'Safe label';
  assert.equal(requestValue(skip, 'credential', true), 'Safe label');
});
