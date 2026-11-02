// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { request } from 'node:http';
import assert from 'node:assert/strict';
import { ExchangeRecorder, withScenarioHTTPServer, type Scenario } from './connector_scenarios.js';

export async function assertUnexpectedGetBodyIsRecorded(): Promise<void> {
  const scenario: Scenario = {
    name: 'get-body', operation: 'test', callId: 'call', input: {},
    exchanges: [{ method: 'GET', path: '/', keyMode: 'absent', status: 200, headers: {}, body: '' }],
    expected: { branch: '', failureKind: '', retry: false, retryAfterMs: 0, uncertain: false, requestCount: 1 },
  };
  const recorder = new ExchangeRecorder(scenario);
  await withScenarioHTTPServer(recorder, baseUrl => new Promise<void>((resolve, reject) => {
    const outgoing = request(baseUrl, { method: 'GET', headers: { 'Content-Length': '7' } }, incoming => {
      incoming.resume(); incoming.on('end', resolve);
    });
    outgoing.on('error', reject);
    outgoing.end('payload');
  }));
  assert.ok(recorder.mismatches.includes('request 0: unasserted body'));
}
