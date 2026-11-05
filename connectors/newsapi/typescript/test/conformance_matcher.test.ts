// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { it } from 'vitest';
import { ExchangeRecorder, SyntheticRandomSource, type Exchange, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { assertUnexpectedGetBodyIsRecorded } from '../../../../conformance/runners/typescript/http_server_case.js';

interface MatcherCase {
  name: string; requestPath: string; requestHeaders: Record<string, string[]>;
  requestBody: string; exchange: Exchange; expectedMismatches: string[];
}
const cases = JSON.parse(readFileSync(new URL('../../../../conformance/runners/matcher_cases.json', import.meta.url), 'utf8')) as MatcherCase[];

it('matches Go synthetic random bytes and rejection sampling', () => {
  const seed = 'SYNTHETIC_CONFORMANCE_PARITY_VECTOR';
  const bytes = new SyntheticRandomSource(seed).read(64).toString('hex');
  const expected = '8340adfda3332f0d504929aa79cc566bfd436f83db3b60780dc6dd4c666b777d' +
    '891789301f0e1cca5155d82b3cdea5a89baeb0cfc50cac2c00eb26ca6a40565f';
  if (bytes !== expected) throw new Error(`synthetic random stream ${bytes} differs from Go`);
  const source = new SyntheticRandomSource(seed);
  const observed = [26, 26, 10, 12, 74, 44, 43, 1, 256, 257].map(maximum => source.goRandomInt(maximum));
  if (JSON.stringify(observed) !== JSON.stringify([3, 0, 3, 3, 47, 13, 16, 0, 73, 107])) {
    throw new Error(`synthetic bounded values ${JSON.stringify(observed)} differ from Go`);
  }
});

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
