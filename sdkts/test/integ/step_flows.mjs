// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, StepList, Worker, forceComplete, openBlobCache } from '@superdurable/dex';
import { RetryError, goToBranch, newMutationBranch, newMutationRetry, newMutationStep, newMutationUncertain, newQueryBranch, newQueryRetry, newQueryStep } from '../../dist/index.js';
import { branchCase, newMutation } from '../../dist/connectormock/index.js';
import { newFence } from '../../dist/singledispatch/index.js';

const serverAddress = process.env.DEX_SERVER_ADDRESS ?? '127.0.0.1:18891';
const failure = { kind: 'AVAILABILITY', provider: 'fixture', operation: 'getItem', message: 'temporary provider failure' };
const retry = { executeRetry: { initialIntervalMs: 1_000, backoffCoefficient: 1, maximumIntervalMs: 1_000, maximumAttempts: 3, totalDurationMs: 10_000 } };
const annotations = { groupId: 'fixture', groupLabel: 'Fixture', explanation: 'Calls the fixture provider.' };
const connection = { provider: 'fixture', name: 'default' };
const flows = [];
const scenarios = [];

class Done {
  constructor(stepType) { this.stepType = stepType; }
  getStepType() { return this.stepType; }
  execute(_context, result) { return forceComplete(result); }
}

function queryFlow(name, invoke, selected = 'found', branches = [{ id: 'found', description: 'Found' }, { id: 'defect', description: 'Defect', optional: true }], options = {}) {
  const done = new Done(`${name}Done`);
  const step = newQueryStep({ stepType: `${name}Query`, annotations, connection, mapToOperationInput: input => input,
    operation: { definition: () => ({ operation: { connectorId: 'fixture', operationId: 'getItem' }, branches, stepDefaults: {} }), invoke },
    branches: [goToBranch('found', done), ...(selected !== 'found' ? [goToBranch(selected, done)] : [])], stepOptionsOverride: options });
  const flowType = `Sdkts${name}${randomUUID().replaceAll('-', '')}`;
  const flow = { getFlowType: () => flowType, getSteps: () => StepList.startStep(step).otherSteps(done) };
  flows.push(flow);
  return flow;
}

function mutationFlow(name, invoke, selected = 'created', branches = [{ id: 'created', description: 'Created' }, { id: 'defect', description: 'Defect', optional: true }], options = {}) {
  const done = new Done(`${name}Done`);
  const step = newMutationStep({ stepType: `${name}Mutation`, annotations, connection, mapToOperationInput: input => input,
    operation: { definition: () => ({ operation: { connectorId: 'fixture', operationId: 'createItem' }, branches, stepDefaults: {} }), idempotencyKey: callId => callId, invoke },
    branches: [goToBranch('created', done), ...(selected !== 'created' ? [goToBranch(selected, done)] : [])], stepOptionsOverride: options });
  const flowType = `Sdkts${name}${randomUUID().replaceAll('-', '')}`;
  const flow = { getFlowType: () => flowType, getSteps: () => StepList.startStep(step).otherSteps(done) };
  flows.push(flow);
  return flow;
}

scenarios.push({ flow: queryFlow('Happy', (_call, input) => newQueryBranch('found', { id: input.id })), status: 'completed', branch: 'found' });
scenarios.push({ flow: queryFlow('Branch', () => newQueryBranch('missing', { id: 'absent' }), 'missing', [
  { id: 'found', description: 'Found' }, { id: 'missing', description: 'Absent', optional: true }, { id: 'defect', description: 'Defect', optional: true },
]), status: 'completed', branch: 'missing' }); // Business reaction: report an absent item as a terminal result.

let retriedCalls = 0;
scenarios.push({ flow: queryFlow('RetrySuccess', (_call, input) => ++retriedCalls === 1 ? newQueryRetry(failure) : newQueryBranch('found', { id: input.id }), 'found', undefined, retry), status: 'completed', branch: 'found', verify: () => assert.equal(retriedCalls, 2) });
let exhaustedCalls = 0;
scenarios.push({ flow: queryFlow('RetryExhaustion', () => { exhaustedCalls++; return newQueryRetry(failure); }, 'found', undefined,
  { executeRetry: { ...retry.executeRetry, maximumAttempts: 2 } }), status: 'failed', verify: () => assert.equal(exhaustedCalls, 2) });

scenarios.push({ flow: mutationFlow('Uncertain', () => newMutationUncertain({ id: 'item-1' }, failure), 'uncertain', [
  { id: 'created', description: 'Created' }, { id: 'uncertain', description: 'Unknown effect', optional: true }, { id: 'defect', description: 'Defect', optional: true },
]), status: 'completed', branch: 'uncertain' }); // Business reaction: surface the unconfirmable write for review.

let fencedSends = 0;
let didReadBack = false;
scenarios.push({ flow: mutationFlow('Fenced', async (call, input) => {
  const fence = newFence(call, 15_000);
  const decision = fence.decide();
  if (decision.kind === 'sendAllowed') {
    await fence.dispatch(async signal => { assert.equal(signal.aborted, false); fencedSends++; return { providerId: input.id }; });
    throw new RetryError(failure); // Simulate losing an accepted provider response.
  }
  assert.equal(decision.kind, 'readBackRequired');
  assert.equal(decision.marker?.state, 'claimed');
  didReadBack = true;
  return newMutationBranch('created', { id: input.id });
}, 'created', undefined, { ...retry, executeMethodTimeoutMs: 15_000, heartbeatTimeoutMs: 10_000, executeDurability: 'sync' }), status: 'completed', branch: 'created', verify: () => { assert.equal(fencedSends, 1); assert.equal(didReadBack, true); } });

let cadenceHeartbeats = 0;
scenarios.push({ flow: mutationFlow('HeartbeatCadence', async (call, input) => {
  const originalContext = call.context;
  const observedContext = new Proxy(originalContext, { get(target, property) {
    if (property === 'recordHeartbeat') return async (...arguments_) => { cadenceHeartbeats++; return target.recordHeartbeat(...arguments_); };
    const value = Reflect.get(target, property);
    return typeof value === 'function' ? value.bind(target) : value;
  } });
  const fence = newFence({ ...call, context: observedContext }, 20_000);
  assert.equal(fence.decide().kind, 'sendAllowed');
  await fence.dispatch(async () => { await new Promise(resolve => setTimeout(resolve, 5_500)); });
  assert.ok(cadenceHeartbeats >= 2, `expected a repeated claim heartbeat, got ${cadenceHeartbeats}`);
  return newMutationBranch('created', { id: input.id });
}, 'created', undefined, { executeMethodTimeoutMs: 20_000, heartbeatTimeoutMs: 10_000, executeDurability: 'sync' }), status: 'completed', branch: 'created', verify: () => assert.ok(cadenceHeartbeats >= 2) });

const mockDefinition = { operation: { connectorId: 'fixture', operationId: 'createItem' }, branches: [
  { id: 'created', description: 'Created' }, { id: 'defect', description: 'Defect', optional: true },
], stepDefaults: {} };
const scripted = newMutation(mockDefinition).respond(branchCase('created', { id: 'item-1' })).strict();
let mockAttempts = 0;
scenarios.push({ flow: mutationFlow('MockReplay', (call, input) => {
  const answer = scripted.invoke(call, input);
  return ++mockAttempts === 1 ? newMutationRetry(failure) : answer;
}, 'created', undefined, retry), status: 'completed', branch: 'created', verify: () => {
  assert.equal(mockAttempts, 2);
  assert.equal(scripted.calls()[1]?.replay, true);
  scripted.assertNoFailures();
} });

const registry = new Registry(flows);
const cacheDirectory = await mkdtemp(join(tmpdir(), 'sdkts-dex-cache-'));
const cache = openBlobCache({ directory: cacheDirectory, maxBytes: 64 * 1024 * 1024 });
const worker = new Worker(registry, cache, { serverAddress, pull: { maxConcurrentTasks: 8, reservedRpcTasks: 0 } });
const client = new Client(registry, cache, { serverAddress });
try {
  await worker.start();
  await worker.waitPullRegistered({ timeoutMs: 10_000 });
  for (const scenario of scenarios) {
    const flowId = `sdkts-${randomUUID()}`;
    await client.startFlow(scenario.flow, flowId, { id: 'item-1' });
    const result = await client.waitForFlow(flowId, 30_000);
    assert.equal(result.status, scenario.status, `${scenario.flow.getFlowType()}: ${result.errorMessage ?? 'wrong status'}`);
    if (scenario.branch) assert.equal(result.singleOutput().branch, scenario.branch);
    scenario.verify?.();
  }
} finally {
  await client.close();
  await worker.close();
  cache.close();
  await rm(cacheDirectory, { recursive: true, force: true });
}
