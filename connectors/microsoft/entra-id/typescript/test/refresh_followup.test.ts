// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, type Call, type CredentialRefreshDriver } from '@superdurable/connectors-sdk';
import { EntraClient } from '../src/client.js';
import type { Credentials } from '../src/generated/models.js';
import { HeartbeatContext, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

it('uses the refreshed credential for every follow-up request in one Invoke', async () => {
  const credentials: Credentials = { authMethodId: 'microsoft-oauth', clientId: 'client', clientSecret: new SecretString('secret'),
    accessToken: new SecretString('old-token'), refreshToken: new SecretString('refresh') };
  const replacement: Credentials = { ...credentials, accessToken: new SecretString('new-token') };
  const source = { resolve: async () => credentials, resolveWithRefresh: async () => credentials,
    resolveAfterRejection: async () => replacement };
  const driver: CredentialRefreshDriver<Credentials> = { refreshRequired: () => false,
    refresh: async () => { throw new Error('unexpected refresh'); } };
  const tokens: string[] = [];
  const client = new EntraClient({}, source, { refreshDriver: driver, send: async request => {
    tokens.push(request.headers!.Authorization!);
    return { status: tokens.length === 1 ? 401 : 200, headers: {}, body: Buffer.from('{}') };
  } });
  const call = scenarioCall({ callId: 'entra-follow-up', operation: 'disableUser' } as Scenario,
    { provider: 'microsoft', name: 'entra-id' }, 'microsoft-entra-id', new HeartbeatContext(undefined)) as Call;
  const resolved = await client.resolve(call);
  expect(resolved).toBe(credentials);
  expect((await client.exchange(call, resolved!, 'disableUser', { method: 'PATCH', path: '/users/user1', payload: { accountEnabled: false } })).outcome).toBe('succeeded');
  expect((await client.exchange(call, resolved!, 'disableUser', { method: 'GET', path: '/users/user1' })).outcome).toBe('succeeded');
  expect(tokens).toEqual(['Bearer old-token', 'Bearer new-token', 'Bearer new-token']);
  expect(credentials.accessToken?.reveal()).toBe('old-token');
});

it('accepts a JSON null revoke response as Go does', async () => {
  const credentials: Credentials = { authMethodId: 'microsoft-oauth', clientId: 'client', clientSecret: new SecretString('secret'),
    accessToken: new SecretString('token'), refreshToken: new SecretString('refresh') };
  const client = new EntraClient({}, { resolve: async () => credentials, resolveWithRefresh: async () => credentials }, { send: async () =>
    ({ status: 200, headers: {}, body: Buffer.from('null') }) });
  const call = scenarioCall({ callId: 'entra-revoke-null', operation: 'revokeSignInSessions' } as Scenario,
    { provider: 'microsoft', name: 'entra-id' }, 'microsoft-entra-id', new HeartbeatContext(undefined)) as Call;
  const result = await client.revokeSignInSessions().invoke(call, { userKey: 'user@example.com' });
  expect(result).toMatchObject({ kind: 'branch', branch: 'revoked' });
});

it('uses Go TrimSpace for user keys, including U+0085 but not BOM', async () => {
  const credentials: Credentials = { authMethodId: 'microsoft-oauth', clientId: 'client', clientSecret: new SecretString('secret'),
    accessToken: new SecretString('token'), refreshToken: new SecretString('refresh') };
  let requests = 0;
  const client = new EntraClient({}, { resolve: async () => credentials, resolveWithRefresh: async () => credentials }, {
    send: async () => { requests++; return { status: 404, headers: {}, body: Buffer.from('{}') }; },
  });
  const call = scenarioCall({ callId: 'entra-trim', operation: 'getUser' } as Scenario,
    { provider: 'microsoft', name: 'entra-id' }, 'microsoft-entra-id', new HeartbeatContext(undefined)) as Call;
  await client.getUser().invoke(call, { userKey: 'user@example.com\u0085' });
  expect(requests).toBe(1);
  expect(await client.getUser().invoke(call, { userKey: '\uFEFFuser@example.com' })).toMatchObject({ kind: 'branch', branch: 'defect' });
  expect(requests).toBe(1);
});
