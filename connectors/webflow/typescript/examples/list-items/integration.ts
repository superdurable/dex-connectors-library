// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { GeneratedMock, listCollectionItemsRetry, listCollectionItemsDefault } from '../../src/mock.js';
import { ConnectionName, ListItemsFlow } from './flow.js';

/** Verify the checked-in Flow through a real Dex Worker and Client. */
export async function verifyListItemsFlow(serverAddress: string): Promise<void> {
  const mock = new GeneratedMock(ConnectionName);
  const flow = new ListItemsFlow(mock.connection());
  const directory = await mkdtemp(join(tmpdir(), 'dex-webflow-integration-'));
  const blobCache = openBlobCache({ directory, maxBytes: 1 << 30 });
  const registry = new Registry([flow]);
  const worker = new Worker(registry, blobCache, { serverAddress });
  const client = new Client(registry, blobCache, { serverAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 10_000 });
    const flowId = `webflow-list-${randomUUID()}`;
    mock.listCollectionItems().forFlow(flowId).respond(listCollectionItemsRetry({ kind: 'RATE_LIMIT', provider: 'webflow', operation: 'listCollectionItems', message: 'rate limited' }, 10), listCollectionItemsDefault());
    await client.startFlow(flow, flowId, { collectionId: '62b720ef280c7a7a3be8cabe', limit: 10 });
    const result = await client.waitForFlow(flowId, 30_000);
    assert.equal(result.status, 'completed', result.errorMessage);
    assert.equal((result.singleOutput<{data: {items: unknown[]}}>().data.items).length, 1);
    assert.deepEqual(mock.listCollectionItems().calls().filter(call => call.flowId === flowId).map(call => call.retry), [true, undefined]);
    mock.assertNoFailures();
  } finally { await client.close(); await worker.close(); blobCache.close(); await rm(directory, { recursive: true, force: true }); }
}

if (process.argv[1]?.endsWith('/integration.ts')) verifyListItemsFlow(process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801').catch(error => { console.error(error); process.exitCode = 1; });
