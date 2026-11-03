// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { TMDBClient } from '../src/client.js';

const connection = { provider: 'tmdb', name: 'tmdb' };
const call = {
  id: 'tmdb-unit-call', connection, operation: { connectorId: 'tmdb', operationId: 'getMovie' },
  context: { flowId: 'tmdb-unit-flow', cancellationSignal: new AbortController().signal },
  reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
} as unknown as Call;

it('uses the configured request timeout and rejects an oversized successful response', async () => {
  let observedTimeout = 0;
  const client = new TMDBClient({ baseUrl: 'http://127.0.0.1:29800/3', timeoutMs: 1234, maxResponseBytes: 8 }, {
    resolve: async () => ({ apiKey: new SecretString('test-secret-token') }),
  }, { send: async request => {
    observedTimeout = request.timeoutMs ?? 0;
    return { status: 200, headers: {}, body: Buffer.from('{"id":343611}') };
  } });
  const result = await client.getMovie().invoke(call, { id: 343611 });
  expect(observedTimeout).toBe(1234);
  expect(result.kind).toBe('branch');
  if (result.kind !== 'branch') throw new Error('expected terminal branch');
  expect(result.branch).toBe('invalidResponse');
  expect(result.failure?.kind).toBe('RESPONSE_TOO_LARGE');
});
