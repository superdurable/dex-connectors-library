// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type CredentialRefreshDriver } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { ExcelClient } from '../src/client.js';
import type { Credentials } from '../src/models.js';
import { HeartbeatContext, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

it('uses the refreshed token for every request after one 401', async () => {
  const original: Credentials = { authMethodId: 'microsoft-oauth', accessToken: new SecretString('old-token'),
    oauthClientId: 'client', oauthClientSecret: new SecretString('secret'), refreshToken: new SecretString('refresh') };
  const refreshed: Credentials = { ...original, accessToken: new SecretString('new-token') };
  let refreshes = 0;
  const provider = { resolve: async () => original, resolveWithRefresh: async () => original,
    resolveAfterRejection: async () => { refreshes++; return refreshed; } };
  const driver: CredentialRefreshDriver<Credentials> = { refreshRequired: () => false, refresh: async () => { throw new TypeError('unexpected token exchange'); } };
  const tokens: string[] = [];
  const client = new ExcelClient({}, provider, { refreshDriver: driver, endpoint: 'http://127.0.0.1:40661/v1.0',
    transport: async request => {
      tokens.push(request.headers!.Authorization!);
      if (tokens.length === 1) return { status: 401, headers: {}, body: Buffer.from('{}') };
      if (request.url.includes('/columns?')) return { status: 200, headers: {}, body: Buffer.from('{"value":[{"id":"c1","index":0,"name":"Key"}]}') };
      return { status: 200, headers: {}, body: Buffer.from('{"rowCount":1,"columnCount":1,"values":[["k1"]]}') };
    } });
  const scenario = { callId: 'a', operation: 'getTableRows' } as Scenario;
  const call = scenarioCall(scenario, { provider: 'microsoft', name: 'approvals' }, 'microsoft-excel', new HeartbeatContext(undefined));
  const result = await client.getTableRows().invoke(call as Parameters<ReturnType<ExcelClient['getTableRows']>['invoke']>[0],
    { driveId: 'b!drive', workbookId: 'item', table: 'Decisions' });
  expect(result.kind).toBe('branch');
  expect(tokens).toEqual(['Bearer old-token', 'Bearer new-token', 'Bearer new-token']);
  expect(refreshes).toBe(1);
});

it('classifies an oversized error body by its HTTP status', async () => {
  const { BodyTooLargeError } = await import('@superdurable/connectors-sdk/providerhttp');
  const { GraphClient } = await import('../src/graph.js');
  const credentials: Credentials = { authMethodId: 'microsoft-oauth', accessToken: new SecretString('token'),
    oauthClientId: 'client', oauthClientSecret: new SecretString('secret'), refreshToken: new SecretString('refresh') };
  const source = { resolve: async () => credentials, resolveWithRefresh: async () => credentials };
  const driver: CredentialRefreshDriver<Credentials> = { refreshRequired: () => false,
    refresh: async () => { throw new Error('unused'); } };
  const graph = new GraphClient({}, source, driver, async () => { throw new BodyTooLargeError(404, {}); });
  const call = scenarioCall({ callId: 'excel-oversized-error', operation: 'getValues' } as Scenario,
    { provider: 'microsoft', name: 'approvals' }, 'microsoft-excel', new HeartbeatContext(undefined));
  const result = await graph.request(call as never, credentials, 'GET', '/missing');
  expect(result.status).toBe(404);
  expect(result.transportFailure).toBe(false);
  expect(graph.readFailure(call as never, 'getValues', result, {
    NotFound: 'notFound', ProviderRejected: 'providerRejected', InvalidResponse: 'invalidResponse',
  })).toMatchObject({ branch: 'notFound' });
});
