// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { it } from 'vitest';
import { ExchangeRecorder, type Exchange, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { assertUnexpectedGetBodyIsRecorded } from '../../../../conformance/runners/typescript/http_server_case.js';

interface MatcherCase {
  name: string; requestPath: string; requestHeaders: Record<string, string[]>;
  requestBody: string; exchange: Exchange; expectedMismatches: string[];
}
const cases = JSON.parse(readFileSync(new URL('../../../../conformance/runners/matcher_cases.json', import.meta.url), 'utf8')) as MatcherCase[];

it.each(cases)('matches shared request fields: $name', test => {
  const scenario: Scenario = { name: test.name, operation: 'matcher', callId: 'call', input: {},
    exchanges: [test.exchange], expected: { branch: '', failureKind: '', retry: false, retryAfterMs: 0,
      uncertain: false, requestCount: 1 } };
  const recorder = new ExchangeRecorder(scenario);
  recorder.record({ method: test.exchange.method, url: 'http://localhost' + test.requestPath,
    headers: test.requestHeaders, body: test.requestBody });
  if (recorder.mismatches.length !== test.expectedMismatches.length) {
    throw new Error(`mismatches ${JSON.stringify(recorder.mismatches)} differ from ${JSON.stringify(test.expectedMismatches)}`);
  }
  for (const field of test.expectedMismatches) {
    if (!recorder.mismatches.some(mismatch => mismatch.includes(field))) throw new Error(`missing ${field}`);
  }
});
it('records a GET request body through the HTTP server', assertUnexpectedGetBodyIsRecorded);
