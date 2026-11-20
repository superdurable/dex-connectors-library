// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { OceanbaseClient } from '../../src/client.js';
import { newConnection, type Credentials } from '../../src/index.js';
import { ConnectionName, OceanbaseInspectionFlow } from '../../examples/basic/flow.js';

it('retries a provider 503, completes Digest auth, and persists an instance page through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const directory = await mkdtemp(join(tmpdir(), 'dex-oceanbase-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = {
    accessKeyId: new SecretString('REDACTED_OCEANBASE_ACCESS_KEY'),
    accessKeySecret: new SecretString('REDACTED_OCEANBASE_SECRET'),
    projectId: new SecretString('REDACTED_PROJECT_ID'),
  };
  const provider = new OceanbaseClient({}, { resolve: async () => credentials }, {
    nonce: () => new Uint8Array(16).fill(7),
    send: async request => {
      requests++;
      const url = new URL(request.url);
      expect(url.hostname).toBe('api-cloud-cn.oceanbase.com');
      expect(url.pathname).toBe('/api/v2/instances');
      expect(request.headers?.['X-Ob-Project-Id']).toBe('REDACTED_PROJECT_ID');
      if (requests === 1) return { status: 503, headers: {}, body: new Uint8Array() };
      if (requests === 2) return { status: 401, headers: { 'WWW-Authenticate': 'Digest realm="oceanbase", nonce="server-nonce", qop="auth", algorithm=MD5' }, body: new Uint8Array() };
      expect(request.headers?.Authorization).toMatch(/^Digest username=/);
      return {
        status: 200, headers: {},
        body: new TextEncoder().encode(JSON.stringify({ success: true, data: { dataList: [{ instanceId: 'obcluster-1', instanceName: 'demo', state: 'ONLINE', region: 'ap-guangzhou' }] }, totalCount: 1 })),
      };
    },
  });
  const connection = newConnection(provider, { provider: 'oceanbase', name: ConnectionName });
  const flow = new OceanbaseInspectionFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `oceanbase-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { pageNumber: 1, pageSize: 10 });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput()).toEqual({ items: [{ id: 'obcluster-1', name: 'demo', state: 'ONLINE', region: 'ap-guangzhou' }], total: 1, pageNumber: 1, pageSize: 10 });
    expect(requests).toBe(3);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 120_000);
