// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError } from '@superdurable/connectors-sdk/providerhttp';
import { WooCommerceClient } from '../src/client.js';

const call: Call = {
  context: { flowId: 'woocommerce-unit', cancellationSignal: new AbortController().signal } as Call['context'],
  id: 'unit-call', connection: { provider: 'woocommerce', name: 'woocommerce' }, operation: { connectorId: 'woocommerce', operationId: 'listOrders' },
  reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
};

describe('WooCommerce read boundaries', () => {
  it('keeps invalid input and missing credentials terminal without sending', async () => {
    let sent = 0;
    const client = new WooCommerceClient({ storeUrl: 'http://localhost/shop' }, { resolve: async () => ({ consumerKey: new SecretString('ck_test'), consumerSecret: new SecretString('') }) }, { send: async () => { sent++; throw new Error('unexpected'); } });
    const invalid = await client.listOrders().invoke(call, { page: -1 });
    const missing = await client.getOrder().invoke(call, { orderId: 1 });
    expect(invalid.kind).toBe('branch');
    if (invalid.kind !== 'branch') throw new Error('expected a branch');
    expect(invalid.branch).toBe('defect');
    expect(invalid.failure?.kind).toBe('VALIDATION');
    expect(missing.kind).toBe('branch');
    if (missing.kind !== 'branch') throw new Error('expected a branch');
    expect(missing.branch).toBe('defect');
    expect(missing.failure?.kind).toBe('AUTHENTICATION');
    expect(sent).toBe(0);
  });

  it('uses a site path, Basic Auth, and the configured request timeout', async () => {
    const requests: { url: string; timeoutMs: number; authorization: string }[] = [];
    const client = new WooCommerceClient({ storeUrl: 'http://localhost/shop', timeoutMs: 1900 }, { resolve: async () => ({ consumerKey: new SecretString('ck_test'), consumerSecret: new SecretString('cs_test') }) }, {
      send: async request => {
        requests.push({ url: request.url, timeoutMs: request.timeoutMs ?? 0, authorization: request.headers?.Authorization ?? '' });
        return { status: 200, headers: {}, body: Buffer.from('[]') };
      },
    });
    const result = await client.listOrders().invoke(call, { page: 2, perPage: 10 });
    expect(result.kind).toBe('branch');
    if (result.kind !== 'branch') throw new Error('expected a branch');
    expect(result.branch).toBe('listed');
    expect(requests).toEqual([{ url: 'http://localhost/shop/wp-json/wc/v3/orders?page=2&per_page=10', timeoutMs: 1900, authorization: 'Basic Y2tfdGVzdDpjc190ZXN0' }]);
  });

  it('classifies status before an oversized error body and rejects malformed success data', async () => {
    const credentials = { resolve: async () => ({ consumerKey: new SecretString('ck_test'), consumerSecret: new SecretString('cs_test') }) };
    const largeError = new WooCommerceClient({ storeUrl: 'http://localhost/shop' }, credentials, {
      send: async () => { throw new BodyTooLargeError(503, {}); },
    });
    const retry = await largeError.getOrder().invoke(call, { orderId: 101 });
    expect(retry.kind).toBe('retry');
    if (retry.kind !== 'retry') throw new Error('expected a retry');
    expect(retry.failure.kind).toBe('AVAILABILITY');
    const malformed = new WooCommerceClient({ storeUrl: 'http://localhost/shop' }, credentials, {
      send: async () => ({ status: 200, headers: {}, body: Buffer.from('{"id":"101"}') }),
    });
    const invalid = await malformed.getOrder().invoke(call, { orderId: 101 });
    expect(invalid.kind).toBe('branch');
    if (invalid.kind !== 'branch') throw new Error('expected a branch');
    expect(invalid.branch).toBe('invalidResponse');
    expect(invalid.failure?.kind).toBe('PROTOCOL');
  });
});
