// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { QuickBooksClient } from '../src/client.js';

const call = { id: 'quickbooks-unit-call', idempotencyKey: 'quickbooks-unit-call', connection: { provider: 'quickbooks', name: 'test' }, operation: { connectorId: 'quickbooks', operationId: 'createInvoice' }, context: { cancellationSignal: new AbortController().signal } } as Call;
const credentials = { accessToken: new SecretString('quickbooks-test-token'), clientId: 'test-client', clientSecret: new SecretString('test-secret'), refreshToken: new SecretString(''), idToken: new SecretString('') };
const source = { resolve: async () => credentials, resolveWithRefresh: async () => credentials };
const response = (status: number, body: unknown): ProviderResponse => ({ status, headers: {}, body: Buffer.from(JSON.stringify(body)) });

function client(send: (request: ProviderRequest) => Promise<ProviderResponse>, provider = source) {
  return new QuickBooksClient({ realmId: '9341453050298464' }, provider, { send, localProviderURL: 'http://127.0.0.1' });
}

describe('QuickBooks provider boundaries', () => {
  it('treats malformed host credentials as a terminal defect', async () => {
    const provider = { resolve: async (): Promise<typeof credentials> => { throw new TypeError('malformed'); }, resolveWithRefresh: async (): Promise<typeof credentials> => { throw new TypeError('malformed'); } };
    const result = await client(async () => { throw new Error('must not send'); }, provider).getInvoice().invoke(call, { invoiceId: '130' });
    expect(result).toMatchObject({ kind: 'branch', branch: 'defect', failure: { kind: 'AUTHENTICATION' } });
  });

  it('retries a transient host refresh failure', async () => {
    const provider = { resolve: async (): Promise<typeof credentials> => { throw new Error('temporary outage'); }, resolveWithRefresh: async (): Promise<typeof credentials> => { throw new Error('temporary outage'); } };
    const result = await client(async () => { throw new Error('must not send'); }, provider).getInvoice().invoke(call, { invoiceId: '130' });
    expect(result).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' } });
  });

  it('rejects invalid calendar dates before a provider request', async () => {
    const result = await client(async () => { throw new Error('must not send'); }).createInvoice().invoke(call, { customerId: '58', lines: [{ itemId: '1', amount: '1.00' }], txnDate: '2026-02-30' });
    expect(result).toMatchObject({ kind: 'branch', branch: 'defect', failure: { kind: 'VALIDATION' } });
  });

  it('accepts Unicode addresses and IP domain literals, but rejects malformed literals', async () => {
    const seen: string[] = [];
    const sender = async (request: ProviderRequest): Promise<ProviderResponse> => { seen.push(request.url); return response(200, { QueryResponse: {} }); };
    const provider = client(sender);
    expect(await provider.findCustomer().invoke(call, { emailAddress: 'é@例子.测试' })).toMatchObject({ kind: 'branch', branch: 'notFound' });
    expect(await provider.findCustomer().invoke(call, { emailAddress: 'a@[127.0.0.1]' })).toMatchObject({ kind: 'branch', branch: 'notFound' });
    expect(await provider.findCustomer().invoke(call, { emailAddress: 'a@[garbage]' })).toMatchObject({ kind: 'branch', branch: 'defect' });
    expect(seen).toHaveLength(2);
  });

  it('sends an exact rounded decimal rather than a binary floating point amount', async () => {
    let body = '';
    const sender = async (request: ProviderRequest): Promise<ProviderResponse> => { body = Buffer.from(request.body ?? []).toString('utf8'); return response(200, {}); };
    const result = await client(sender).createInvoice().invoke(call, { customerId: '58', lines: [{ itemId: '1', quantity: '0.015', unitPrice: '1' }] });
    expect(result.kind).toBe('uncertain');
    expect(body).toContain('"Amount":0.02');
    expect(body).not.toContain('"Amount":"0.02"');
  });

  it('classifies an oversized 429 diagnostic by status', async () => {
    const sender = async (): Promise<ProviderResponse> => ({ status: 429, headers: {}, body: Buffer.alloc(80_000, 120) });
    const result = await client(sender).getInvoice().invoke(call, { invoiceId: '130' });
    expect(result).toMatchObject({ kind: 'retry', failure: { kind: 'RATE_LIMIT' } });
  });

  it('refreshes once after 401 and preserves the mutation requestid', async () => {
    const urls: string[] = [];
    const sourceWithRejection = {
      ...source,
      resolveAfterRejection: async () => ({ ...credentials, accessToken: new SecretString('replacement-token') }),
    };
    const sender = async (request: ProviderRequest): Promise<ProviderResponse> => {
      urls.push(request.url);
      return urls.length === 1 ? response(401, {}) : response(200, { Invoice: { Id: '130', CustomerRef: { value: '58' }, TotalAmt: 1, Balance: 1, Line: [] } });
    };
    const result = await client(sender, sourceWithRejection).createInvoice().invoke(call, { customerId: '58', lines: [{ itemId: '1', amount: '1.00' }] });
    expect(result).toMatchObject({ kind: 'branch', branch: 'created' });
    expect(urls).toHaveLength(2);
    expect(urls[0]).toBe(urls[1]);
    expect(new URL(urls[0]!).searchParams.get('requestid')).toBe(call.idempotencyKey);
  });
});
