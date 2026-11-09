// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { expect, it } from 'vitest';
import { Mock } from '../../src/mock.js';
import type { DocumentSet } from '../../src/index.js';
import { ConnectionName, DocumentDiscoveryFlow } from '../../examples/document-discovery/flow.js';

it('persists the manifest mock through a real Dex Worker and Client', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  const directory = await mkdtemp(join(tmpdir(), 'dex-mongodb-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const mock = new Mock(ConnectionName);
  const flow = new DocumentDiscoveryFlow(mock.connection());
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `mongodb-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { collection: 'orders', filter: { status: 'active' } });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<DocumentSet>()).toMatchObject({ documents: [{ status: 'active' }] });
    expect(mock.findDocuments().calls()).toHaveLength(1);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 120_000);
