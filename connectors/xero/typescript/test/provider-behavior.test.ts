// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { ReauthorizationRequiredError, RetryError, SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { XeroClient, encodeGoQuery, escapeGoPath } from '../src/client.js';
import type { Credentials } from '../src/generated/models.js';

const connection = { provider: 'xero', name: 'xero-test' };
const token = 'xero-test-access-token-0123456789';
const invoiceID = '243216c5-369e-4056-ac67-05388f86dc81';
const tenantID = '70784a63-d24b-46a9-a4db-0e70a274b056';
const invoiceBody = Buffer.from(JSON.stringify({ Invoices: [{ InvoiceID: invoiceID, Type: 'ACCREC', Status: 'DRAFT' }] }));
const call = { id: '801feb97-5ad4-5063-b93c-9a25f054e79b', connection, context: { cancellationSignal: new AbortController().signal } } as Call;

function staticCredentials(method: 'custom-connection' | 'xero-oauth') {
  const credentials: Credentials = { authMethodId: method, clientId: '0F1E2D3C4B5A69788796A5B4C3D2E1F0', clientSecret: new SecretString('client-secret'), accessToken: new SecretString(token), ...(method === 'xero-oauth' ? { refreshToken: new SecretString('refresh-token') } : {}) };
  return new StaticCredentialProvider([[connection, credentials]]);
}

function response(status: number, body: Uint8Array): ProviderResponse { return { status, headers: {}, body }; }

it('escapes Xero paths and sorted query values like Go', () => {
  expect(escapeGoPath("A/B!?~$&+=:@,;'()*")).toBe('A%2FB%21%3F~$&+=:@%2C%3B%27%28%29%2A');
  expect(encodeGoQuery({ where: 'a b~!*', IDs: 'a,b' })).toBe('IDs=a%2Cb&where=a+b~%21%2A');
});

it('selects and caches the named OAuth tenant without exposing its name in requests', async () => {
  const requests: ProviderRequest[] = [];
  const client = new XeroClient({ organisation: 'Maple Florist' }, staticCredentials('xero-oauth'), {
    send: async request => {
      requests.push(request);
      if (request.url.endsWith('/connections')) return response(200, Buffer.from(JSON.stringify([
        { tenantId: tenantID.toUpperCase(), tenantType: 'ORGANISATION', tenantName: 'Maple Florist' },
        { tenantId: '06638157-fdfa-47f4-91d0-875b5f5c18c6', tenantType: 'ORGANISATION', tenantName: 'Another Company' },
      ])));
      return response(200, invoiceBody);
    },
  });
  for (let index = 0; index < 2; index++) {
    const attempt = await client.getInvoice().invoke(call, { invoiceId: invoiceID });
    expect(attempt.kind).toBe('branch');
    expect(attempt.kind === 'branch' ? attempt.branch : 'retry').toBe('found');
  }
  expect(requests.map(request => new URL(request.url).pathname)).toEqual(['/connections', `/api.xro/2.0/Invoices/${invoiceID}`, `/api.xro/2.0/Invoices/${invoiceID}`]);
  expect(requests[1]?.headers?.['Xero-Tenant-Id']).toBe(tenantID);
  expect(requests[2]?.headers?.['Xero-Tenant-Id']).toBe(tenantID);
});

it('separates terminal, transient, and broken credential sources', async () => {
  for (const [error, outcome] of [
    [new ReauthorizationRequiredError(), 'providerRejected'],
    [new RetryError({ kind: 'AVAILABILITY', provider: 'xero', operation: 'refreshCredentials', message: 'temporarily unavailable' }), 'retry'],
    [new Error('missing connection'), 'defect'],
  ] as const) {
    const credentials = { resolveWithRefresh: async () => { throw error; }, resolve: async () => { throw error; } };
    const client = new XeroClient({}, credentials, { send: async () => { throw new Error('provider must not be called'); } });
    const attempt = await client.listContacts().invoke(call, {});
    expect(attempt.kind === 'retry' ? 'retry' : attempt.branch).toBe(outcome);
  }
});

it('accepts Go email forms and rejects malformed domain literals before dispatch', async () => {
  const addresses = ['ü@exämple.com', 'a@[127.0.0.1]', 'a@[::1]'];
  const rejected = ['a@[IPv6:::1]', 'a..b@example.com'];
  const requests: ProviderRequest[] = [];
  const client = new XeroClient({}, staticCredentials('custom-connection'), { send: async request => { requests.push(request); return response(200, Buffer.from('{"Contacts":[]}')); } });
  for (const emailAddress of addresses) {
    const attempt = await client.findContactByEmail().invoke(call, { emailAddress });
    expect(attempt.kind === 'branch' ? attempt.branch : 'retry').toBe('notFound');
  }
  for (const emailAddress of rejected) {
    const attempt = await client.findContactByEmail().invoke(call, { emailAddress });
    expect(attempt.kind === 'branch' ? attempt.branch : 'retry').toBe('defect');
  }
  expect(requests).toHaveLength(addresses.length);
});
