// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { AddressRefusal, BodyTooLargeError } from '@superdurable/connectors-sdk/providerhttp';
import { PipedriveClient, validatePipedriveEndpoint } from '../src/client.js';
import { loadScenarios, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import type { Credentials } from '../src/generated/models.js';

const connection = { provider: 'pipedrive', name: 'pipedrive-crm' };
const scenario = loadScenarios(new URL('../../../../conformance/connectors/pipedrive/scenarios.json', import.meta.url)).find(item => item.operation === 'getObject') as Scenario;

function call() {
  return scenarioCall(scenario, connection, 'pipedrive', {
    flowId: 'deal-intake-flow', stepExecutionId: 'get:1', attempt: 1,
    cancellationSignal: new AbortController().signal,
  }) as Call;
}

function source(resolve: () => Promise<Credentials>) { return { resolve, resolveWithRefresh: resolve }; }

it('selects the returned OAuth company domain and bearer token', async () => {
  const requests: Array<{ url: string; authorization: string | undefined }> = [];
  const client = new PipedriveClient({ endpoint: 'https://api.pipedrive.com' }, source(async () => ({ authMethodId: 'pipedrive-oauth', accessToken: new SecretString('oauth-token'), apiDomain: 'http://127.0.0.1:45440' })), { send: async request => {
    requests.push({ url: request.url, authorization: request.headers?.Authorization });
    return { status: 200, headers: {}, body: Buffer.from('{"success":true,"data":{"id":301,"title":"Acme"}}') };
  } });
  const result = await client.getObject().invoke(call(), { objectType: 'deals', objectId: '301' });
  expect(result.kind).toBe('branch');
  expect(requests).toEqual([{ url: 'http://127.0.0.1:45440/api/v2/deals/301', authorization: 'Bearer oauth-token' }]);
});

it('rejects a non-Pipedrive endpoint before any credential or provider call', () => {
  expect(() => validatePipedriveEndpoint('https://api.example.com')).toThrow();
  expect(() => validatePipedriveEndpoint('https://pipedrive.com.evil.example')).toThrow();
  expect(() => validatePipedriveEndpoint('https://api.pipedrive.com/api/v2')).toThrow();
});

it('classifies unavailable credential storage as retry and malformed material as defect', async () => {
  const missing = new PipedriveClient({}, source(async () => { throw new Error('temporarily unavailable'); }));
  const unavailable = await missing.getObject().invoke(call(), { objectType: 'deals', objectId: '301' });
  expect(unavailable.kind).toBe('retry');
  const malformed = new PipedriveClient({}, source(async () => ({ authMethodId: 'api-token', apiToken: new SecretString('bad\nkey') })));
  const invalid = await malformed.getObject().invoke(call(), { objectType: 'deals', objectId: '301' });
  expect(invalid.kind).toBe('branch');
  if (invalid.kind === 'branch') expect(invalid.branch).toBe('defect');
});

it('retries an address refusal before provider dispatch', async () => {
  const client = new PipedriveClient({}, source(async () => ({ authMethodId: 'api-token', apiToken: new SecretString('safe-token') })), {
    send: async () => { throw new AddressRefusal('test address refused'); },
  });
  const attempt = await client.getObject().invoke(call(), { objectType: 'deals', objectId: '301' });
  expect(attempt.kind).toBe('retry');
  if (attempt.kind === 'retry') expect(attempt.failure.kind).toBe('TRANSPORT');
});

it('classifies a large 429 by status and a large success body by size', async () => {
  const credentials = source(async () => ({ authMethodId: 'api-token', apiToken: new SecretString('safe-token') }));
  const throttled = new PipedriveClient({}, credentials, { send: async () => { throw new BodyTooLargeError(429, {}); } });
  const retry = await throttled.getObject().invoke(call(), { objectType: 'deals', objectId: '301' });
  expect(retry.kind).toBe('retry');
  if (retry.kind === 'retry') expect(retry.failure.kind).toBe('RATE_LIMIT');
  const oversized = new PipedriveClient({}, credentials, { send: async () => { throw new BodyTooLargeError(200, {}); } });
  const invalid = await oversized.getObject().invoke(call(), { objectType: 'deals', objectId: '301' });
  expect(invalid.kind).toBe('branch');
  if (invalid.kind === 'branch') expect(invalid.branch).toBe('invalidResponse');
});

it('retains a full int64 record ID and rejects a wrong-typed association', async () => {
  const client = new PipedriveClient({}, source(async () => ({ authMethodId: 'api-token', apiToken: new SecretString('safe-token') })), {
    send: async request => ({ status: 200, headers: {}, body: Buffer.from(request.url.endsWith('/301')
      ? '{"success":true,"data":{"id":301,"owner_id":"bad"}}'
      : '{"success":true,"data":{"id":9223372036854775807,"title":"Maximum"}}') }),
  });
  const invalid = await client.getObject().invoke(call(), { objectType: 'deals', objectId: '301' });
  expect(invalid.kind).toBe('branch');
  if (invalid.kind === 'branch') expect(invalid.branch).toBe('invalidResponse');
  const largest = await client.getObject().invoke(call(), { objectType: 'deals', objectId: '9223372036854775807' });
  expect(largest.kind).toBe('branch');
  if (largest.kind === 'branch') expect(largest.value.id).toBe('9223372036854775807');
});

it('rejects impossible RFC 3339 dates before sending a list request', async () => {
  let requests = 0;
  const client = new PipedriveClient({}, source(async () => ({ authMethodId: 'api-token', apiToken: new SecretString('safe-token') })), {
    send: async () => { requests++; return { status: 200, headers: {}, body: Buffer.from('{"success":true,"data":[]}') }; },
  });
  for (const updatedSince of ['2026-02-30T00:00:00Z', '2025-02-29T00:00:00Z', '2026-01-01T24:00:00Z']) {
    const result = await client.listObjects().invoke(call(), { objectType: 'deals', updatedSince });
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') expect(result.branch).toBe('defect');
  }
  expect(requests).toBe(0);
});
