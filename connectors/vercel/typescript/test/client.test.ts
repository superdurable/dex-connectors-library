// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { AddressRefusal } from '@superdurable/connectors-sdk/providerhttp';
import { VercelClient } from '../src/client.js';

const call = { id: 'vercel-unit', context: { cancellationSignal: new AbortController().signal }, connection: { provider: 'vercel', name: 'vercel' } } as Call;
const credentials = { resolve: async () => ({ accessToken: new SecretString('test-accesstoken') }) };

describe('Vercel request behavior', () => {
  it('uses Go path and query escaping for a deployment hostname and team', async () => {
    let requestedURL = '';
    const client = new VercelClient({ baseUrl: 'http://127.0.0.1:30300' }, credentials, { send: async request => {
      requestedURL = request.url;
      return { status: 200, headers: {}, body: Buffer.from('{"id":"dpl_one"}') };
    } });
    const result = await client.getDeployment().invoke(call, { idOrUrl: "app.example/a!b$&+;?'*", teamId: 'team ~*' });
    expect(result.kind).toBe('branch');
    expect(requestedURL).toBe('http://127.0.0.1:30300/v13/deployments/app.example%2Fa%21b$&+%3B%3F%27%2A?teamId=team+%7E%2A');
  });
  it('rejects invalid response fields and oversized success bodies', async () => {
    const client = new VercelClient({ baseUrl: 'http://127.0.0.1:30300', maxResponseBytes: 20 }, credentials, { send: async () => ({ status: 200, headers: {}, body: Buffer.from('{"id":123,"name":"long"}') }) });
    const result = await client.getDeployment().invoke(call, { idOrUrl: 'dpl_one' });
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') expect(result.branch).toBe('invalidResponse');
  });
  it('classifies wrong-typed provider fields as an invalid response', async () => {
    const client = new VercelClient({ baseUrl: 'http://127.0.0.1:30300' }, credentials, { send: async () => ({ status: 200, headers: {}, body: Buffer.from('{"id":123}') }) });
    const result = await client.getDeployment().invoke(call, { idOrUrl: 'dpl_one' });
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') expect(result.branch).toBe('invalidResponse');
  });
  it('classifies large provider errors by status', async () => {
    const client = new VercelClient({ baseUrl: 'http://127.0.0.1:30300', maxResponseBytes: 1 }, credentials, { send: async () => ({ status: 429, headers: {}, body: Buffer.alloc(100_000) }) });
    const result = await client.listDeployments().invoke(call, {});
    expect(result.kind).toBe('retry');
  });
  it('retries an address-policy refusal like a Go transport error', async () => {
    const client = new VercelClient({ baseUrl: 'http://127.0.0.1:30300' }, credentials, { send: async () => { throw new AddressRefusal('test refusal'); } });
    const result = await client.listDeployments().invoke(call, {});
    expect(result.kind).toBe('retry');
  });
  it('validates request limits before using credentials', () => {
    expect(() => new VercelClient({ timeoutMs: -1 }, credentials)).toThrow();
    expect(() => new VercelClient({ maxResponseBytes: -1 }, credentials)).toThrow();
  });
});
