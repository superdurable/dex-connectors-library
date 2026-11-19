// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { strict as assert } from 'node:assert';
import { readFileSync, writeFileSync, mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { test } from 'node:test';
import { ExchangeRecorder, loadScenarios } from './connector_scenarios.ts';

const cases = JSON.parse(readFileSync(new URL('../clock_cases.json', import.meta.url), 'utf8'));

test('replays Youdao-shaped signed form digests and catches a changed signature', () => {
  const matcherCases = JSON.parse(readFileSync(new URL('../matcher_cases.json', import.meta.url), 'utf8'));
  for (const matcherCase of matcherCases.filter(item => item.name.startsWith('signed-form-'))) {
    const recorder = new ExchangeRecorder({ exchanges: [matcherCase.exchange] });
    recorder.record({ method: 'POST', url: 'http://localhost' + matcherCase.requestPath,
      headers: Object.fromEntries(Object.entries(matcherCase.requestHeaders).map(([name, values]) => [name, values[0]])),
      body: matcherCase.requestBody });
    assert.equal(recorder.requestCount, 1);
    assert.equal(recorder.mismatches.length, matcherCase.expectedMismatches.length, matcherCase.name);
    for (const expected of matcherCase.expectedMismatches) assert.match(recorder.mismatches.join('\n'), new RegExp(expected));
  }
});

test('loadScenarios validates signed form digest descriptors', () => {
  const source = JSON.parse(readFileSync(new URL('../../../conformance/connectors/airtable/scenarios.json', import.meta.url), 'utf8'));
  const scenario = source.scenarios[0];
  scenario.randomSeed = 'SYNTHETIC_CONFORMANCE_FORM';
  scenario.randomFormFields = ['salt'];
  scenario.exchanges[0].form = { salt: { $syntheticSha256: 'a'.repeat(64) } };
  const directory = mkdtempSync(join(tmpdir(), 'connector-signed-form-'));
  try {
    const path = join(directory, 'scenarios.json');
    writeFileSync(path, JSON.stringify(source));
    assert.deepEqual(loadScenarios(pathToFileURL(path))[0].randomFormFields, ['salt']);
    scenario.exchanges[0].form.salt.$syntheticSha256 = 'invalid';
    writeFileSync(path, JSON.stringify(source));
    assert.throws(() => loadScenarios(pathToFileURL(path)), /invalid form/);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

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
