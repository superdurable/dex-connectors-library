// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { Client } from '../src/index.js';

const connection = { provider: 'harvest', name: 'harvest' };
const call = { id: '5ecde15b-017b-5681-83c4-dcbd11fe5e99', connection, context: { cancellationSignal: new AbortController().signal }, operation: { connectorId: 'harvest', operationId: 'listProjects' } } as Call;
const credentials = { resolve: async () => ({ apiKey: new SecretString('test-secret-key') }) };

describe('Harvest client', () => {
  it('rejects invalid account IDs at construction', () => {
    expect(() => new Client({ accountId: '0' }, credentials)).toThrow(/decimal ID/);
  });

  it('keeps credential lookup errors terminal and makes no provider request', async () => {
    let requests = 0;
    const client = new Client({ accountId: '123' }, { resolve: async () => { throw new Error('secret'); } }, async () => { requests++; throw new Error('unexpected request'); });
    const attempt = await client.listProjects().invoke(call, {});
    expect(attempt.kind).toBe('branch');
    if (attempt.kind === 'branch') { expect(attempt.branch).toBe('defect'); expect(attempt.failure?.kind).toBe('AUTHENTICATION'); expect(attempt.failure?.message).not.toContain('secret'); }
    expect(requests).toBe(0);
  });

  it('retries a refused local transport request', async () => {
    const client = new Client({ accountId: '123' }, credentials, async (request: ProviderRequest) => { void request; throw new ProviderTransportError(true); });
    const attempt = await client.listProjects().invoke(call, {});
    expect(attempt.kind).toBe('retry');
    if (attempt.kind === 'retry') expect(attempt.failure.kind).toBe('AVAILABILITY');
  });

  it('passes the account ID in a header and extracts the next cursor', async () => {
    let request: ProviderRequest | undefined;
    const client = new Client({ accountId: '123' }, credentials, async current => {
      request = current;
      return { status: 200, headers: {}, body: Buffer.from('{"projects":[],"links":{"next":"https://api.harvestapp.com/v2/projects?cursor=next_2"}}') };
    });
    const attempt = await client.listProjects().invoke(call, { cursor: 'first_1', perPage: 10 });
    expect(request?.headers?.['Harvest-Account-Id']).toBe('123');
    expect(request?.url).toContain('/projects?cursor=first_1&per_page=10');
    if (attempt.kind === 'branch') expect(attempt.value.nextCursor).toBe('next_2');
  });
});
