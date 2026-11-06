// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { GeneratedMock, listItemsListed, listItemsRetry } from '../../src/mock.js';
import { ConnectionName, ListItemsFlow } from './flow.js';

/** Exercise the checked-in Flow across real Dex Worker, Client, persistence, and retry boundaries. */
export async function verifyListItemsFlow(serverAddress: string): Promise<void> {
  const mock = new GeneratedMock(ConnectionName);
  const flow = new ListItemsFlow(mock.connection());
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-monday-list-integration-'));
  const blobCache = openBlobCache({ directory, maxBytes: 1 << 30 });
  const worker = new Worker(registry, blobCache, { serverAddress });
  const client = new Client(registry, blobCache, { serverAddress });
  try {
    await worker.start(); await worker.waitPullRegistered({ timeoutMs: 10_000 });
    const successID = `monday-list-${randomUUID()}`;
    mock.listItems().forFlow(successID).respond(listItemsListed({ boardId: '1234567890', items: [{ id: '9876543210', name: 'Fire drill', state: 'active', boardId: '1234567890' }] }));
    await client.startFlow(flow, successID, { boardId: '1234567890', limit: 2 });
    const success = await client.waitForFlow(successID, 30_000);
    assert.equal(success.status, 'completed', success.errorMessage);
    assert.equal(success.singleOutput<{ items: Array<{ id: string }> }>().items[0]?.id, '9876543210');
    const retryID = `monday-list-retry-${randomUUID()}`;
    mock.listItems().forFlow(retryID).respond(
      listItemsRetry({ kind: 'RATE_LIMIT', provider: 'monday', operation: 'listItems', message: 'monday.com rate limited the read' }, 10),
      listItemsListed({ boardId: '1234567890', items: [] }),
    );
    await client.startFlow(flow, retryID, { boardId: '1234567890' });
    const retried = await client.waitForFlow(retryID, 30_000);
    assert.equal(retried.status, 'completed', retried.errorMessage);
    assert.deepEqual(mock.listItems().calls().filter(call => call.flowId === retryID).map(call => call.retry), [true, undefined]);
    mock.assertNoFailures();
  } finally { await client.close(); await worker.close(); blobCache.close(); await rm(directory, { recursive: true, force: true }); }
}

if (process.argv[1]?.endsWith('/integration.ts')) {
  verifyListItemsFlow(process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801').catch(error => { console.error(error); process.exitCode = 1; });
}
