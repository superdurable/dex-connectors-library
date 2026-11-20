// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { WeibanClient } from '../../src/client.js';
import { newConnection } from '../../src/index.js';
import { ConnectionName, WeibanInspectionFlow } from '../../examples/basic/flow.js';

it('retries Weiban 503 and persists a project page through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const tokenBody = new TextEncoder().encode(JSON.stringify({ errcode: 0, access_token: 'REDACTED_WEIBAN_ACCESS_TOKEN', expires_in: 7200 }));
  const projectBody = new TextEncoder().encode(JSON.stringify({ errcode: 0, total: 1, tag_list: [{ tag_id: 'tag_123', tag_name: 'Returning', tag_group: 'Lifecycle', private: 'must not persist' }] }));
  const directory = await mkdtemp(join(tmpdir(), 'dex-weiban-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new WeibanClient({}, { resolve: async () => ({ corpId: 'corp_test', secret: new SecretString('REDACTED_WEIBAN_SECRET') }) }, {
    send: async request => {
      requests++;
      if (requests === 1) return { status: 503, headers: {}, body: new Uint8Array() };
      return request.url.includes('access_token/get') ? { status: 200, headers: {}, body: tokenBody } : { status: 200, headers: {}, body: projectBody };
    },
  });
  const flow = new WeibanInspectionFlow(newConnection(provider, { provider: 'weiban', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `weiban-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, {});
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput()).toEqual({ total: 1, tags: [{ id: 'tag_123', name: 'Returning', group: 'Lifecycle' }] });
    expect(requests).toBe(3);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 120_000);
