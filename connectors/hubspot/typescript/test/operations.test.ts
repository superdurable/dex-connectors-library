// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { loadScenarios, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { HubSpotClient } from '../src/client.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/hubspot/scenarios.json', import.meta.url));
const connection = { provider: 'hubspot', name: 'hubspot-crm' };
function callFor(operation: string): Call {
  const scenario = scenarios.find(item => item.operation === operation)!;
  return scenarioCall(scenario as Scenario, connection, 'hubspot', {
    flowId: 'hubspot-unit', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
}
function client(send: (request: ProviderRequest) => Promise<ProviderResponse>): HubSpotClient {
  return new HubSpotClient({ endpoint: 'http://127.0.0.1:44721' }, new StaticCredentialProvider([[connection, { authMethodId: 'private-app-token', accessToken: new SecretString('pat-na1-test-token') }]]), { send });
}

it('rejects invalid search input before a provider request', async () => {
  let requests = 0;
  const hubspot = client(async () => { requests++; throw new Error('unexpected request'); });
  const attempt = await hubspot.searchObjects().invoke(callFor('searchObjects'), { objectType: 'deals', limit: 201 });
  expect(attempt.kind).toBe('branch');
  if (attempt.kind === 'branch') expect(attempt.branch).toBe('defect');
  expect(requests).toBe(0);
});

it('rejects wrong-typed provider fields as invalidResponse', async () => {
  const hubspot = client(async () => ({ status: 200, headers: {}, body: Buffer.from('{"id":"9001","properties":{"dealname":42}}') }));
  const attempt = await hubspot.getObject().invoke(callFor('getObject'), { objectType: 'deals', objectId: '9001' });
  expect(attempt.kind).toBe('branch');
  if (attempt.kind === 'branch') expect(attempt.branch).toBe('invalidResponse');
  expect(attempt.failure?.kind).toBe('PROTOCOL');
});

it('refreshes a rejected OAuth access token once through the host credential source', async () => {
  const tokens: string[] = [];
  const source = {
    resolve: async () => ({ authMethodId: 'hubspot-oauth', accessToken: new SecretString('old-token'), oauthClientId: 'client', oauthClientSecret: new SecretString('secret'), refreshToken: new SecretString('refresh') }),
    resolveWithRefresh: async () => ({ authMethodId: 'hubspot-oauth', accessToken: new SecretString('old-token'), oauthClientId: 'client', oauthClientSecret: new SecretString('secret'), refreshToken: new SecretString('refresh') }),
    resolveAfterRejection: async () => ({ authMethodId: 'hubspot-oauth', accessToken: new SecretString('new-token'), oauthClientId: 'client', oauthClientSecret: new SecretString('secret'), refreshToken: new SecretString('refresh') }),
  };
  const hubspot = new HubSpotClient({ endpoint: 'http://127.0.0.1:44721' }, source, { send: async request => {
    tokens.push(request.headers?.Authorization ?? '');
    return { status: tokens.length === 1 ? 401 : 200, headers: {}, body: Buffer.from(tokens.length === 1 ? '{}' : '{"total":0,"results":[]}') };
  } });
  const attempt = await hubspot.searchObjects().invoke(callFor('searchObjects'), { objectType: 'deals' });
  expect(attempt.kind).toBe('branch');
  if (attempt.kind === 'branch') expect(attempt.branch).toBe('searched');
  expect(tokens).toEqual(['Bearer old-token', 'Bearer new-token']);
});

it('treats a fractional batch status as Go HTTP 400', async () => {
  const hubspot = client(async () => ({ status: 207, headers: {}, body: Buffer.from('{"results":[],"errors":[{"status":"500.0","category":"VALIDATION_ERROR"}]}') }));
  const attempt = await hubspot.upsertObject().invoke(callFor('upsertObject'), { objectType: 'deals', idProperty: 'external_id', idValue: 'item-1', properties: {} });
  expect(attempt).toMatchObject({ kind: 'branch', branch: 'providerRejected', failure: { kind: 'VALIDATION' } });
});

it('counts Go JSON HTML escaping against the 3000-byte search limit', async () => {
  let requests = 0;
  const hubspot = client(async () => { requests++; throw new Error('unexpected request'); });
  const attempt = await hubspot.searchObjects().invoke(callFor('searchObjects'), { objectType: 'deals', query: '<'.repeat(550) });
  expect(attempt).toMatchObject({ kind: 'branch', branch: 'defect' });
  expect(requests).toBe(0);
});

it('rejects fractional rate interval milliseconds', async () => {
  const hubspot = client(async () => ({ status: 429, headers: { 'x-hubspot-ratelimit-interval-milliseconds': '2000.5' }, body: Buffer.from('{}') }));
  const attempt = await hubspot.searchObjects().invoke(callFor('searchObjects'), { objectType: 'deals' });
  expect(attempt).toMatchObject({ kind: 'retry', retryAfterMs: 0 });
});
