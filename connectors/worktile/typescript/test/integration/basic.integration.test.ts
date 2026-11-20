// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { WorktileClient } from '../../src/client.js';
import { newConnection } from '../../src/index.js';
import { ConnectionName, WorktileInspectionFlow } from '../../examples/basic/flow.js';

it('retries Worktile 503 and persists a project page through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const tokenBody = new TextEncoder().encode(JSON.stringify({ tenant_access_token: 'REDACTED_WORKTILE_ACCESS_TOKEN', expires_in: 7200 }));
  const projectBody = new TextEncoder().encode(JSON.stringify({ page_index: 0, page_size: 10, has_more: false, items: [{ _id: 'project-1', name: 'Example Project', identifier: 'ABC', visibility: 1, private: 'must not persist' }] }));
  const directory = await mkdtemp(join(tmpdir(), 'dex-worktile-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new WorktileClient({}, { resolve: async () => ({ clientId: new SecretString('REDACTED_WORKTILE_CLIENT_ID'), clientSecret: new SecretString('REDACTED_WORKTILE_CLIENT_SECRET') }) }, {
    send: async request => {
      requests++;
      if (requests === 1) return { status: 503, headers: {}, body: new Uint8Array() };
      return request.url.includes('tenant-access-token') ? { status: 200, headers: {}, body: tokenBody } : { status: 200, headers: {}, body: projectBody };
    },
  });
  const flow = new WorktileInspectionFlow(newConnection(provider, { provider: 'worktile', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `worktile-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { pageIndex: 0, pageSize: 10 });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput()).toEqual({ pageIndex: 0, pageSize: 10, hasMore: false, projects: [{ id: 'project-1', name: 'Example Project', identifier: 'ABC', visibility: 1 }] });
    expect(requests).toBe(3);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 120_000);
