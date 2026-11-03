// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
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
