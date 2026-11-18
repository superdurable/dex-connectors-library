// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { strict as assert } from 'node:assert';
import { readFileSync, writeFileSync, mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { test } from 'node:test';
import { HeartbeatContext, isSameHeartbeat, loadScenarios } from './connector_scenarios.ts';

test('isSameHeartbeat compares a count-free recording without the exact count', () => {
  const callId = '5af8fafa-06ff-5b2d-bb13-04693809fabf';
  const legacy = { callId, attempt: 1, state: 'canResend' };
  const exact = { singleDispatchMarkers: { [callId]: { ...legacy, sendCount: 0 } } };
  assert.equal(isSameHeartbeat(exact, legacy, callId), true);
  assert.equal(isSameHeartbeat(exact, { ...legacy, state: 'notSent' }, callId), false);
  assert.equal(isSameHeartbeat(exact, { ...legacy, sendCount: 1 }, callId), false);
  assert.equal(isSameHeartbeat(exact, { singleDispatchMarkers: { [callId]: { ...legacy, sendCount: 0 } } }, callId), true);
});

test('HeartbeatContext records the checkpoint map heartbeat as written', async () => {
  const callId = '5af8fafa-06ff-5b2d-bb13-04693809fabf';
  const checkpoint = { singleDispatchMarkers: { [callId]: { callId, attempt: 1, state: 'claimed' } } };
  const context = new HeartbeatContext(undefined);
  await context.recordHeartbeat(checkpoint);
  assert.deepEqual(context.getLastHeartbeatValue(), checkpoint);
  assert.equal(context.hasWrittenHeartbeat, true);
  const retried = new HeartbeatContext({ attempt: 1, value: checkpoint });
  assert.equal(retried.attempt, 2);
  assert.deepEqual(retried.getLastHeartbeatValue(), checkpoint);
});

test('HeartbeatContext presents a recording in the installed SDK checkpoint format', () => {
  const callId = '5af8fafa-06ff-5b2d-bb13-04693809fabf';
  const marker = { callId, attempt: 1, state: 'claimed', sendCount: 1 };
  const checkpoint = { singleDispatchMarkers: { [callId]: marker } };
  const flatForMapSDK = new HeartbeatContext({ ...marker }, undefined, 'flat', callId, true);
  assert.deepEqual(flatForMapSDK.getLastHeartbeatValue(), checkpoint);
  const mapForFlatSDK = new HeartbeatContext({ attempt: 1, value: checkpoint }, undefined, 'map', callId, false);
  assert.deepEqual(mapForFlatSDK.getLastHeartbeatValue(), marker);
  assert.deepEqual(new HeartbeatContext({ ...marker }, undefined, 'flat', callId, false).getLastHeartbeatValue(), marker);
  const foreign = { ...marker, callId: 'b8b7c0e5-3d8c-5d5e-9a51-0c4d2b7e6f10' };
  assert.deepEqual(new HeartbeatContext({ attempt: 1, value: foreign }, undefined, 'foreign', callId, true).getLastHeartbeatValue(), foreign);
  const both = { singleDispatchMarkers: { [callId]: marker, [foreign.callId]: foreign } };
  assert.deepEqual(new HeartbeatContext({ attempt: 1, value: both }, undefined, 'both', callId, false).getLastHeartbeatValue(), both);
});

const cases = JSON.parse(readFileSync(new URL('../clock_cases.json', import.meta.url), 'utf8'));

test('loadScenarios validates shared fixed clock cases', () => {
  const source = JSON.parse(readFileSync(new URL('../../../conformance/connectors/airtable/scenarios.json', import.meta.url), 'utf8'));
  const directory = mkdtempSync(join(tmpdir(), 'connector-clock-'));
  try {
    const path = join(directory, 'scenarios.json');
    for (const clock of cases.valid) {
      source.scenarios[0].fixedClock = clock;
      writeFileSync(path, JSON.stringify(source));
      assert.equal(loadScenarios(pathToFileURL(path))[0].fixedClock, clock);
    }
    for (const clock of cases.invalid) {
      source.scenarios[0].fixedClock = clock;
      writeFileSync(path, JSON.stringify(source));
      assert.throws(() => loadScenarios(pathToFileURL(path)), String(clock));
    }
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test('loadScenarios validates a prior marker sendCount like the marker decoder', () => {
  const source = JSON.parse(readFileSync(new URL('../../../conformance/connectors/airtable/scenarios.json', import.meta.url), 'utf8'));
  const scenario = source.scenarios[0];
  const directory = mkdtempSync(join(tmpdir(), 'connector-send-count-'));
  try {
    const path = join(directory, 'scenarios.json');
    for (const sendCount of [0, 2]) {
      scenario.priorHeartbeat = { callId: scenario.callId, attempt: 2, state: 'claimed', sendCount };
      writeFileSync(path, JSON.stringify(source));
      assert.equal(loadScenarios(pathToFileURL(path))[0].priorHeartbeat.sendCount, sendCount);
    }
    for (const sendCount of [-1, 1.5, '2', null, true]) {
      scenario.priorHeartbeat = { callId: scenario.callId, attempt: 2, state: 'claimed', sendCount };
      writeFileSync(path, JSON.stringify(source));
      assert.throws(() => loadScenarios(pathToFileURL(path)), /invalid prior heartbeat/, String(sendCount));
    }
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});
