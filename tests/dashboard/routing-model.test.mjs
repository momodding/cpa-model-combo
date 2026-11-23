import assert from 'node:assert/strict';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';
import { test } from 'node:test';

const here = dirname(fileURLToPath(import.meta.url));
const source = readFileSync(join(here, '../../dashboard/quota/routing-model.js'), 'utf8');

const { routingGraph, nodeStates, mergeEvents, nodeEvents } = vm.runInNewContext(
  `${source}\n({ routingGraph, nodeStates, mergeEvents, nodeEvents })`
);

test('allowed vs headroom-unrestricted credential join, including thinking-suffix and model_support_unknown', () => {
  const accounts = [
    { id: 'acc-disabled', disabled: true, provider: 'anthropic', auth_index: '0', models: [{ id: 'claude-3-7-sonnet' }] },
    { id: 'acc-unknown', disabled: false, provider: 'anthropic', auth_index: '1', model_support_unknown: true, models: [] },
    { id: 'acc-thinking', disabled: false, provider: 'anthropic', auth_index: '2', models: [{ id: 'claude-3-7-sonnet' }] },
    { id: 'acc-unrelated', disabled: false, provider: 'openai', auth_index: '0', models: [{ id: 'gpt-4o' }] },
  ];

  // Headroom strategy without allowed_accounts includes eligible accounts matching target or thinking-suffix base
  const headroomRoutes = [{
    alias: 'route-hr',
    strategy: 'headroom',
    targets: [{ model: 'claude-3-7-sonnet(thought)' }]
  }];
  const hrGraph = routingGraph(headroomRoutes, accounts, null);
  const hrCredNodeIds = hrGraph.nodes.filter(n => n.type === 'credential').map(n => n.data.credential.id);
  assert.deepEqual([...hrCredNodeIds], ['acc-unknown', 'acc-thinking']);
  assert.equal(hrGraph.nodes.some(n => n.id.includes('acc-disabled')), false);
  assert.equal(hrGraph.nodes.some(n => n.id.includes('acc-unrelated')), false);

  // Priority strategy with allowed_accounts only includes specified eligible accounts
  const priorityAllowedRoutes = [{
    alias: 'route-prio',
    strategy: 'priority',
    targets: [{ model: 'claude-3-7-sonnet', allowed_accounts: ['acc-thinking', 'acc-disabled'] }]
  }];
  const prioAllowedGraph = routingGraph(priorityAllowedRoutes, accounts, null);
  const prioCredNodeIds = prioAllowedGraph.nodes.filter(n => n.type === 'credential').map(n => n.data.credential.id);
  assert.deepEqual([...prioCredNodeIds], ['acc-thinking']);

  // Non-headroom strategy without allowed_accounts has no credential nodes; edge goes combo -> model only
  const prioNoAllowedRoutes = [{
    alias: 'route-direct',
    strategy: 'priority',
    targets: [{ model: 'gpt-4o' }]
  }];
  const directGraph = routingGraph(prioNoAllowedRoutes, accounts, null);
  assert.equal(directGraph.nodes.filter(n => n.type === 'credential').length, 0);
  assert.equal(directGraph.edges.length, 1);
  assert.equal(directGraph.edges[0].source, 'route:route-direct');
  assert.equal(directGraph.edges[0].target, 'model:route-direct:gpt-4o');
});

test('precedence: event > cooldown > exhausted > pending', () => {
  const routes = [{
    alias: 'route-prec',
    strategy: 'headroom',
    targets: [{ model: 'gpt-4o' }]
  }];
  const accounts = [
    { id: 'acc-event', provider: 'p1', auth_index: '1', model_support_unknown: true },
    { id: 'acc-cooldown', provider: 'p2', auth_index: '2', model_support_unknown: true },
    { id: 'acc-exhausted', provider: 'p3', auth_index: '3', model_support_unknown: true },
    { id: 'acc-pending', provider: 'p4', auth_index: '4', model_support_unknown: true },
  ];
  const now = Date.parse('2026-11-23T12:00:00Z');
  const quota = {
    credentials: [
      { provider: 'p1', auth_index: '1', state: 'exhausted' },
      { provider: 'p2', auth_index: '2', state: 'exhausted' },
      { provider: 'p3', auth_index: '3', state: 'exhausted' },
      { provider: 'p4', auth_index: '4', state: 'known' },
    ],
    cooldowns: [
      { route: 'route-prec', model: 'gpt-4o', credential_id: 'acc-event', until: '2026-11-23T12:05:00Z' },
      { route: 'route-prec', model: 'gpt-4o', credential_id: 'acc-cooldown', until: '2026-11-23T12:05:00Z' },
    ]
  };
  const events = [
    {
      seq: 1,
      router_request_id: 'req-1',
      route: 'route-prec',
      model: 'gpt-4o',
      credential: { id: 'acc-event' },
      state: 'success',
      at: '2026-11-23T11:59:50Z'
    }
  ];

  const graph = routingGraph(routes, accounts, quota);
  const states = nodeStates(graph, quota, events, 'req-1', now);

  assert.equal(states.get('cred:route-prec:gpt-4o:acc-event').state, 'success'); // event beats cooldown & exhausted
  assert.equal(states.get('cred:route-prec:gpt-4o:acc-cooldown').state, 'cooldown'); // cooldown beats exhausted
  assert.equal(states.get('cred:route-prec:gpt-4o:acc-exhausted').state, 'exhausted'); // exhausted beats pending
  assert.equal(states.get('cred:route-prec:gpt-4o:acc-pending').state, 'pending'); // default pending
});

test('stale executing degrades to pending', () => {
  const routes = [{
    alias: 'route-stale',
    strategy: 'headroom',
    targets: [{ model: 'gpt-4o' }]
  }];
  const accounts = [
    { id: 'acc-stale', provider: 'p1', auth_index: '1', model_support_unknown: true },
    { id: 'acc-fresh', provider: 'p2', auth_index: '2', model_support_unknown: true },
  ];
  const now = Date.parse('2026-11-23T12:05:00Z');
  const events = [
    {
      seq: 1,
      router_request_id: 'req-live',
      route: 'route-stale',
      model: 'gpt-4o',
      credential: { id: 'acc-stale' },
      state: 'executing',
      at: '2026-11-23T12:02:00Z' // 180s ago (> 120s)
    },
    {
      seq: 2,
      router_request_id: 'req-live',
      route: 'route-stale',
      model: 'gpt-4o',
      credential: { id: 'acc-fresh' },
      state: 'executing',
      at: '2026-11-23T12:04:30Z' // 30s ago (<= 120s)
    }
  ];

  const graph = routingGraph(routes, accounts, null);
  const states = nodeStates(graph, null, events, 'req-live', now);

  assert.equal(states.get('cred:route-stale:gpt-4o:acc-stale').state, 'pending');
  assert.equal(states.get('cred:route-stale:gpt-4o:acc-fresh').state, 'executing');
});

test('cursor regression resets the buffer', () => {
  const initialEvents = [
    { seq: 10, router_request_id: 'req-1', route: 'r1', state: 'success', at: '2026-11-23T12:00:00Z' },
    { seq: 11, router_request_id: 'req-2', route: 'r1', state: 'failed', at: '2026-11-23T12:00:05Z' }
  ];
  const initial = mergeEvents([], { events: initialEvents, event_cursor: 11 });
  assert.equal(initial.cursor, 11);
  assert.equal(initial.buffer.length, 2);
  assert.equal(initial.reset, false);

  // Server restart: event_cursor drops to 2 (< 11)
  const restartedEvents = [
    { seq: 1, router_request_id: 'req-new', route: 'r1', state: 'evaluating', at: '2026-11-23T12:01:00Z' },
    { seq: 2, router_request_id: 'req-new', route: 'r1', state: 'executing', at: '2026-11-23T12:01:01Z' }
  ];
  const result = mergeEvents(initial.buffer, { events: restartedEvents, event_cursor: 2 });
  assert.equal(result.reset, true);
  assert.equal(result.cursor, 2);
  assert.deepEqual([...result.buffer].map(e => e.seq), [1, 2]);
  assert.equal(result.requests[0].id, 'req-new');
});

test('hottest-child rollup', () => {
  const routes = [{
    alias: 'route-rollup',
    strategy: 'headroom',
    targets: [
      { model: 'model-a' },
      { model: 'model-b' }
    ]
  }];
  const accounts = [
    { id: 'acc-1', provider: 'p', auth_index: '1', model_support_unknown: true },
    { id: 'acc-2', provider: 'p', auth_index: '2', model_support_unknown: true },
    { id: 'acc-3', provider: 'p', auth_index: '3', model_support_unknown: true },
  ];
  const events = [
    { seq: 1, router_request_id: 'req-hot', route: 'route-rollup', model: 'model-a', credential: { id: 'acc-1' }, state: 'pending' },
    { seq: 2, router_request_id: 'req-hot', route: 'route-rollup', model: 'model-a', credential: { id: 'acc-2' }, state: 'success' },
    { seq: 3, router_request_id: 'req-hot', route: 'route-rollup', model: 'model-b', credential: { id: 'acc-3' }, state: 'executing' },
  ];

  const graph = routingGraph(routes, accounts, null);
  const states = nodeStates(graph, null, events, 'req-hot');

  // Model A rolls up pending + success -> success
  assert.equal(states.get('model:route-rollup:model-a').state, 'success');
  // Model B rolls up executing -> executing
  assert.equal(states.get('model:route-rollup:model-b').state, 'executing');
  // Combo rolls up model-a (success) + model-b (executing) -> executing (executing > success)
  assert.equal(states.get('route:route-rollup').state, 'executing');
});

test('fallback edge marking and active path marking', () => {
  const routes = [{
    alias: 'route-fb',
    strategy: 'headroom',
    targets: [{ model: 'model-x' }]
  }];
  const accounts = [
    { id: 'acc-cand1', provider: 'p', auth_index: '1', model_support_unknown: true },
    { id: 'acc-cand2', provider: 'p', auth_index: '2', model_support_unknown: true },
  ];
  const events = [
    { seq: 1, router_request_id: 'req-fb', route: 'route-fb', model: 'model-x', credential: { id: 'acc-cand1' }, state: 'selected' },
    { seq: 2, router_request_id: 'req-fb', route: 'route-fb', model: 'model-x', credential: { id: 'acc-cand1' }, state: 'failed', reason: '429' },
    { seq: 3, router_request_id: 'req-fb', route: 'route-fb', model: 'model-x', credential: { id: 'acc-cand1' }, state: 'fallback', reason: '429' },
    { seq: 4, router_request_id: 'req-fb', route: 'route-fb', model: 'model-x', credential: { id: 'acc-cand2' }, state: 'selected' },
    { seq: 5, router_request_id: 'req-fb', route: 'route-fb', model: 'model-x', credential: { id: 'acc-cand2' }, state: 'executing' }
  ];

  const graph = routingGraph(routes, accounts, null);
  const states = nodeStates(graph, null, events, 'req-fb');

  // Edge into cand1 received fallback event -> marked fallback
  assert.equal(states.get('model:route-fb:model-x>cred:route-fb:model-x:acc-cand1').state, 'fallback');
  // Edge into cand2 received selected/executing -> marked active
  assert.equal(states.get('model:route-fb:model-x>cred:route-fb:model-x:acc-cand2').state, 'active');
  // Combo to model edge is on the path -> marked active
  assert.equal(states.get('route:route-fb>model:route-fb:model-x').state, 'active');

  // nodeEvents returns focused-request events for cand1 in chronological order
  const cand1Events = nodeEvents(graph, events, 'cred:route-fb:model-x:acc-cand1', 'req-fb');
  assert.deepEqual([...cand1Events].map(e => e.state), ['selected', 'failed', 'fallback']);
});
