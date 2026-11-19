// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { expect, it } from 'vitest';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { YouzanClient, newConnection, type ShopBasic } from '../../src/index.js';
import { YouzanShopBasicFlow, ConnectionName, shopAttribute } from '../../examples/shop-basic/flow.js';

const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801';
const encoded = (value: unknown) => new TextEncoder().encode(JSON.stringify(value));
let requests = 0;
let shouldReject = false;
async function send(request: ProviderRequest): Promise<ProviderResponse> {
  const url = new URL(request.url);
  if (request.method !== 'POST' || url.pathname !== '/api/youzan.shop.basic.get/3.0.0' || url.searchParams.get('access_token') !== 'sample-test-token')
    return { status: 400, headers: {}, body: encoded({}) };
  requests++;
  if (shouldReject) return { status: 200, headers: {}, body: encoded({ success: false, code: 160100100 }) };
  if (requests === 1) return { status: 503, headers: {}, body: encoded({}) };
  return { status: 200, headers: {}, body: encoded({ success: true, code: 200, data: {
    sid: 59931296, name: 'Sample Shop', role: 1, line_of_business: 70000, url: 'https://h5.youzan.com/v2',
  } }) };
}

it('retries the checked-in Flow, persists shop metadata, and fails on an unwired rejection', async () => {
  const reference = { provider: 'youzan', name: ConnectionName };
  const credentials = new StaticCredentialProvider([[reference, { accessToken: new SecretString('sample-test-token') }]]);
  const connector = new YouzanClient({}, credentials, { send });
  const flow = new YouzanShopBasicFlow(newConnection(connector, reference));
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-youzan-ts-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start(); await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = 'youzan-ts-' + randomUUID();
    await client.startFlow(flow, flowId, {});
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    const output: ShopBasic = { shopID: 59931296, name: 'Sample Shop', role: 1, lineOfBusiness: 70000, url: 'https://h5.youzan.com/v2' };
    expect(result.singleOutput<ShopBasic>()).toEqual(output);
    expect(await client.getAttributes(flowId, shopAttribute)).toEqual([output]);
    expect(requests).toBe(2);
    shouldReject = true;
    const rejectedId = 'youzan-ts-rejected-' + randomUUID();
    await client.startFlow(flow, rejectedId, {});
    const rejected = await client.waitForFlow(rejectedId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toMatch(/providerRejected/);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 120_000);
