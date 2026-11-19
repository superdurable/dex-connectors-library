// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { ShopifyClient } from '../src/client.js';

const call = { context: { cancellationSignal: new AbortController().signal } } as Call;
const credentials = { resolve: async () => ({ accessToken: new SecretString('test-token') }) };

it('uses the configured timeout and limit on one order request', async () => {
  let observed: ProviderRequest | undefined;
  const client = new ShopifyClient({ shopSubdomain: 'test-shop', timeoutMs: 2300, maxResponseBytes: 512 }, credentials, {
    send: async request => {
      observed = request;
      return { status: 200, headers: {}, body: Buffer.from('{"order":{"id":101}}') };
    },
  });
  const result = await client.getOrder().invoke(call, { orderId: 101 });
  expect(result).toMatchObject({ kind: 'branch', branch: 'found' });
  expect(observed?.timeoutMs).toBe(2300);
  expect(observed?.maxResponseBytes).toBe(512);
  expect(observed?.headers?.['X-Shopify-Access-Token']).toBe('test-token');
  expect(observed?.url).toBe('https://test-shop.myshopify.com/admin/api/2026-10/orders/101.json');
});

it('encodes list order query parameters in Go URL order', async () => {
  const requests: string[] = [];
  const client = new ShopifyClient({ shopSubdomain: 'test-shop' }, credentials, {
    send: async request => {
      requests.push(request.url);
      return { status: 200, headers: {}, body: Buffer.from('{"orders":[]}') };
    },
  });
  await client.listOrders().invoke(call, { limit: 10 });
  await client.listOrders().invoke(call, { limit: 0 });
  expect(requests).toEqual([
    'https://test-shop.myshopify.com/admin/api/2026-10/orders.json?limit=10&status=any',
    'https://test-shop.myshopify.com/admin/api/2026-10/orders.json?status=any',
  ]);
});

it('classifies large error bodies by status and large success bodies as invalid', async () => {
  const limited = new ShopifyClient({ shopSubdomain: 'test-shop' }, credentials, {
    send: async () => { throw new BodyTooLargeError(429, { 'retry-after': '3' }); },
  });
  const retry = await limited.getOrder().invoke(call, { orderId: 101 });
  expect(retry.kind).toBe('retry');
  expect(retry).toMatchObject({ retryAfterMs: 3000 });
  const oversized = new ShopifyClient({ shopSubdomain: 'test-shop' }, credentials, {
    send: async () => { throw new BodyTooLargeError(200, {}); },
  });
  expect(await oversized.getOrder().invoke(call, { orderId: 101 })).toMatchObject({ kind: 'branch', branch: 'invalidResponse' });
});

it('treats refused provider connections as Retry and invalid provider fields as invalidResponse', async () => {
  const refused = new ShopifyClient({ shopSubdomain: 'test-shop' }, credentials, {
    send: async () => { throw new ProviderTransportError(true); },
  });
  expect((await refused.listOrders().invoke(call, { limit: 10 })).kind).toBe('retry');
  const malformed = new ShopifyClient({ shopSubdomain: 'test-shop' }, credentials, {
    send: async () => ({ status: 200, headers: {}, body: Buffer.from('{"orders":[{"id":"101"}]}') }),
  });
  expect(await malformed.listOrders().invoke(call, { limit: 10 })).toMatchObject({ kind: 'branch', branch: 'invalidResponse' });
});

it('rejects invalid shop settings and missing credentials before a request', async () => {
  expect(() => new ShopifyClient({ shopSubdomain: 'bad/shop' }, credentials)).toThrow();
  expect(() => new ShopifyClient({ shopSubdomain: 'test-shop', timeoutMs: 0 }, credentials)).toThrow();
  const client = new ShopifyClient({ shopSubdomain: 'test-shop' }, { resolve: async () => { throw new Error('secret details'); } }, {
    send: async () => { throw new Error('must not send'); },
  });
  const result = await client.getOrder().invoke(call, { orderId: 101 });
  expect(result).toMatchObject({ kind: 'branch', branch: 'defect' });
  expect(result.failure?.message).not.toContain('secret details');
});
