// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { NetlifyClient } from '../src/client.js';

const call = { id: 'test', context: { flowId: 'test', cancellationSignal: new AbortController().signal }, connection: { provider: 'netlify', name: 'netlify' }, operation: { connectorId: 'netlify', operationId: 'listDeploys' } } as Call;
const credentials = { resolve: async () => ({ accessToken: new SecretString('test-accesstoken') }) };
it('escapes a site path as Go url.PathEscape does', async () => {
  let requested = '';
  const client = new NetlifyClient({ baseUrl: 'http://127.0.0.1:23200' }, credentials, { send: async (request: ProviderRequest) => {
    requested = request.url;
    return { status: 200, headers: {}, body: Buffer.from('[]') };
  } });
  const result = await client.listDeploys().invoke(call, { siteId: "a/b c!'()*" });
  expect(result.kind).toBe('branch');
  if (result.kind === 'branch') expect(result.branch).toBe('listed');
  expect(requested).toContain('/sites/a%2Fb%20c%21%27%28%29%2A/deploys');
});
it('treats missing credentials as terminal and refused transport as retry', async () => {
  const unavailable = new NetlifyClient({}, { resolve: async () => { throw new Error('unavailable'); } });
  const terminal = await unavailable.listSites().invoke(call, {});
  expect(terminal.kind).toBe('branch');
  if (terminal.kind === 'branch') expect(terminal.branch).toBe('defect');
  expect(terminal.failure?.kind).toBe('AUTHENTICATION');
  const refused = new NetlifyClient({}, credentials, { send: async () => { throw new ProviderTransportError(true); } });
  const retry = await refused.listSites().invoke(call, {});
  expect(retry.kind).toBe('retry');
});
