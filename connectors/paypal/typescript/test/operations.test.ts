// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError } from '@superdurable/connectors-sdk/providerhttp';
import { PayPalClient } from '../src/client.js';

const call = { id: 'order-call', connection: { provider: 'paypal', name: 'paypal' }, operation: { connectorId: 'paypal', operationId: 'getOrder' },
  context: { flowId: 'order-flow', cancellationSignal: new AbortController().signal } } as Call;

it('escapes Go path segments and reuses the credential-scoped token', async () => {
  const paths: string[] = [];
  const client = new PayPalClient({ baseUrl: 'http://127.0.0.1:12345' }, {
    resolve: async () => ({ clientId: 'client-test', clientSecret: new SecretString('secret-test') }),
  }, { send: async request => {
    paths.push(new URL(request.url).pathname);
    return request.url.endsWith('/v1/oauth2/token')
      ? { status: 200, headers: {}, body: Buffer.from('{"access_token":"short-lived-token","token_type":"Bearer","expires_in":3600}') }
      : { status: 200, headers: {}, body: Buffer.from('{"id":"ORDER!101","status":"COMPLETED"}') };
  } });
  const first = await client.getOrder().invoke(call, { orderId: 'ORDER!101' });
  const second = await client.getOrder().invoke(call, { orderId: 'ORDER+101' });
  expect(first.kind).toBe('branch');
  expect(second.kind).toBe('branch');
  expect(paths).toEqual(['/v1/oauth2/token', '/v2/checkout/orders/ORDER%21101', '/v2/checkout/orders/ORDER+101']);
});

it('classifies unavailable local credentials as a terminal defect', async () => {
  const client = new PayPalClient({}, { resolve: async () => { throw new Error('unavailable'); } });
  const attempt = await client.getOrder().invoke(call, { orderId: 'ORDER101' });
  expect(attempt.kind).toBe('branch');
  if (attempt.kind !== 'branch') throw new Error('expected terminal defect');
  expect(attempt.branch).toBe('defect');
  expect(attempt.failure?.kind).toBe('AUTHENTICATION');
});

it.each(['2024-02-30T00:00:00Z', '2023-02-29T00:00:00Z', '2024-01-01T24:00:00Z'])(
  'rejects Go-invalid RFC 3339 date %s before any request', async startDate => {
    const client = new PayPalClient({}, { resolve: async () => { throw new Error('should not resolve credentials'); } });
    const attempt = await client.listTransactions().invoke(call, { startDate, endDate: '2024-03-01T00:00:00Z', page: 1 });
    expect(attempt.kind).toBe('branch');
    if (attempt.kind !== 'branch') throw new Error('expected defect');
    expect(attempt.branch).toBe('defect');
    expect(attempt.failure?.kind).toBe('VALIDATION');
  },
);

it('rejects a date window one nanosecond beyond 31 days', async () => {
  const client = new PayPalClient({}, { resolve: async () => { throw new Error('should not resolve credentials'); } });
  const attempt = await client.listTransactions().invoke(call, {
    startDate: '2024-01-01T00:00:00Z', endDate: '2024-02-01T00:00:00.000000001Z', page: 1,
  });
  expect(attempt.kind).toBe('branch');
  if (attempt.kind !== 'branch') throw new Error('expected defect');
  expect(attempt.branch).toBe('defect');
});

it.each(['2024-01-01T00:00:00+24:00', '2024-01-01T00:00:00+24:01', '2024-01-01T00:00:00+00:60', '2024-01-01T00:00:00,5Z'])(
  'accepts Go-valid RFC 3339 date %s', async startDate => {
    let requests = 0;
    const client = new PayPalClient({ baseUrl: 'http://127.0.0.1:12345' }, {
      resolve: async () => ({ clientId: 'client-test', clientSecret: new SecretString('secret-test') }),
    }, { send: async request => {
      requests++;
      return request.url.endsWith('/v1/oauth2/token')
        ? { status: 200, headers: {}, body: Buffer.from('{"access_token":"token","token_type":"Bearer"}') }
        : { status: 200, headers: {}, body: Buffer.from('{"page":1,"total_pages":1}') };
    } });
    const attempt = await client.listTransactions().invoke(call, { startDate, endDate: startDate, page: 1 });
    expect(attempt.kind).toBe('branch');
    if (attempt.kind !== 'branch') throw new Error('expected listed branch');
    expect(attempt.branch).toBe('listed');
    expect(requests).toBe(2);
  },
);

it.each([
  ['order status', 'getOrder', '{"id":"ORDER101","status":42}'],
  ['page number', 'listTransactions', '{"page":"1"}'],
  ['transaction field', 'listTransactions', '{"transaction_details":[{"transaction_info":{"transaction_id":42}}]}'],
] as const)('rejects wrong-typed %s', async (_label, operation, body) => {
  const client = new PayPalClient({ baseUrl: 'http://127.0.0.1:12345' }, {
    resolve: async () => ({ clientId: 'client-test', clientSecret: new SecretString('secret-test') }),
  }, { send: async request => request.url.endsWith('/v1/oauth2/token')
    ? { status: 200, headers: {}, body: Buffer.from('{"access_token":"token","token_type":"Bearer"}') }
    : { status: 200, headers: {}, body: Buffer.from(body) } });
  const attempt = operation === 'getOrder' ? await client.getOrder().invoke(call, { orderId: 'ORDER101' })
    : await client.listTransactions().invoke(call, { startDate: '2024-01-01T00:00:00Z', endDate: '2024-01-02T00:00:00Z', page: 1 });
  expect(attempt.kind).toBe('branch');
  if (attempt.kind !== 'branch') throw new Error('expected invalid response');
  expect(attempt.branch).toBe('invalidResponse');
  expect(attempt.failure?.kind).toBe('PROTOCOL');
});

it.each([
  ['GET', 503, 'retry', 'AVAILABILITY'],
  ['GET', 404, 'notFound', 'PROVIDER_REJECTION'],
  ['POST', 503, 'retry', 'AVAILABILITY'],
  ['POST', 401, 'providerRejected', 'AUTHENTICATION'],
] as const)('classifies oversized %s %i by status', async (method, status, branch, failureKind) => {
  const client = new PayPalClient({ baseUrl: 'http://127.0.0.1:12345', maxResponseBytes: 1_024 }, {
    resolve: async () => ({ clientId: 'client-test', clientSecret: new SecretString('secret-test') }),
  }, { send: async request => {
    if (request.method === method) throw new BodyTooLargeError(status, { 'retry-after': '2' });
    return { status: 200, headers: {}, body: Buffer.from('{"access_token":"token","token_type":"Bearer"}') };
  } });
  const attempt = await client.getOrder().invoke(call, { orderId: 'ORDER101' });
  expect(attempt.kind).toBe(branch === 'retry' ? 'retry' : 'branch');
  if (attempt.kind === 'branch') expect(attempt.branch).toBe(branch);
  expect(attempt.failure?.kind).toBe(failureKind);
});

it('clears the cached token after a read returns 401', async () => {
  let tokenRequests = 0;
  const client = new PayPalClient({ baseUrl: 'http://127.0.0.1:12345' }, {
    resolve: async () => ({ clientId: 'client-test', clientSecret: new SecretString('secret-test') }),
  }, { send: async request => {
    if (request.method === 'POST') {
      tokenRequests++;
      return { status: 200, headers: {}, body: Buffer.from('{"access_token":"token","token_type":"Bearer","expires_in":3600}') };
    }
    return { status: 401, headers: {}, body: Buffer.from('{}') };
  } });
  await client.getOrder().invoke(call, { orderId: 'ORDER101' });
  await client.getOrder().invoke(call, { orderId: 'ORDER101' });
  expect(tokenRequests).toBe(2);
});
