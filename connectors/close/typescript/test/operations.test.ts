// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { AddressRefusal } from '@superdurable/connectors-sdk/providerhttp';
import { CloseClient } from '../src/client.js';

const call = { id: 'close-test', context: { cancellationSignal: new AbortController().signal } } as Call;

describe('Close operations', () => {
  it('uses the default list limit and Basic credentials', async () => {
    const requests: Array<{ url: string; authorization: string }> = [];
    const client = new CloseClient({ baseUrl: 'http://127.0.0.1:21201/api/v1' }, {
      resolve: async () => ({ apiKey: new SecretString('test-secret-key') }),
    }, { send: async request => {
      requests.push({ url: request.url, authorization: request.headers?.Authorization ?? '' });
      return { status: 200, headers: {}, body: Buffer.from('{"data":[{"id":"lead_1","name":"Meridian"}],"has_more":false}') };
    } });
    const result = await client.listLeads().invoke(call, { skip: 2 });
    expect(result.kind).toBe('branch');
    if (result.kind !== 'branch') throw new Error('expected a listed branch');
    expect(result.branch).toBe('listed');
    expect(requests).toEqual([{ url: 'http://127.0.0.1:21201/api/v1/lead/?_skip=2&_limit=100', authorization: 'Basic dGVzdC1zZWNyZXQta2V5Og==' }]);
  });

  it('rejects an invalid lead ID before resolving a credential', async () => {
    const client = new CloseClient({}, { resolve: async () => { throw new Error('must not resolve'); } });
    const result = await client.getLead().invoke(call, { leadId: 'contact_1' });
    expect(result.kind).toBe('branch');
    if (result.kind !== 'branch') throw new Error('expected a defect branch');
    expect(result.branch).toBe('defect');
  });

  it('retries a refused provider address', async () => {
    const client = new CloseClient({}, {
      resolve: async () => ({ apiKey: new SecretString('test-secret-key') }),
    }, { send: async () => { throw new AddressRefusal('host has no resolved address'); } });
    const result = await client.listLeads().invoke(call, { skip: 0 });
    expect(result.kind).toBe('retry');
    if (result.kind !== 'retry') throw new Error('expected a retry');
    expect(result.failure.kind).toBe('AVAILABILITY');
  });
});
