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
