// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { GeneratedMock, searchDefect, searchRetry, searchSearchedDefaultIndex } from '../../src/index.js';
import { ConnectionName, IndexSearchFlow, ReplaceObjectFlow } from './flow.js';

/** Exercise the checked-in Flow through a real Dex Worker, Client, and retry boundary. */
export async function verifyIndexSearchFlow(serverAddress: string): Promise<void> {
  const mock = new GeneratedMock(ConnectionName);
  const flow = new IndexSearchFlow(mock.connection());
  const replaceFlow = new ReplaceObjectFlow(mock.connection());
  const registry = new Registry([flow, replaceFlow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-algolia-integration-'));
  const blobCache = openBlobCache({ directory, maxBytes: 1 << 30 });
  const worker = new Worker(registry, blobCache, { serverAddress });
  const client = new Client(registry, blobCache, { serverAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 10_000 });

    const successId = `algolia-success-${randomUUID()}`;
    await client.startFlow(flow, successId, { indexName: 'products', query: 'notebook', hitsPerPage: 2 });
    const success = await client.waitForFlow(successId, 30_000);
    assert.equal(success.status, 'completed', success.errorMessage);
    assert.deepEqual(success.singleOutput(), { nbHits: 1, hits: [{ objectID: 'product-1', name: 'Meridian notebook' }] });
    assert.equal(mock.search().calls()[0]?.input.indexName, 'products');

    const replacementId = `algolia-replace-${randomUUID()}`;
    await client.startFlow(replaceFlow, replacementId, { indexName: 'products', objectID: 'product-1', attributes: { name: 'Meridian notebook' } });
    const replacement = await client.waitForFlow(replacementId, 30_000);
    assert.equal(replacement.status, 'completed', replacement.errorMessage);
    assert.deepEqual(replacement.singleOutput(), { objectID: 'product-1', taskID: 1514562690001 });
    assert.equal(mock.saveObject().calls()[0]?.input.objectID, 'product-1');

    const retryId = `algolia-retry-${randomUUID()}`;
    mock.search().forFlow(retryId).respond(searchRetry({ kind: 'AVAILABILITY', provider: 'algolia', operation: 'search', message: 'Algolia is temporarily unavailable' }, 10), searchSearchedDefaultIndex());
    await client.startFlow(flow, retryId, { indexName: 'products', query: 'notebook' });
    const retried = await client.waitForFlow(retryId, 30_000);
    assert.equal(retried.status, 'completed', retried.errorMessage);
    assert.deepEqual(mock.search().calls().filter(call => call.flowId === retryId).map(call => call.retry), [true, undefined]);

    const defectId = `algolia-defect-${randomUUID()}`;
    mock.search().forFlow(defectId).respond(searchDefect({ nbHits: 0, hits: null }, { kind: 'VALIDATION', provider: 'algolia', operation: 'search', message: 'invalid index' }));
    await client.startFlow(flow, defectId, { indexName: '../bad', query: 'notebook' });
    const defect = await client.waitForFlow(defectId, 30_000);
    assert.equal(defect.status, 'failed');
    assert.match(defect.errorMessage ?? '', /defect/);
    mock.assertNoFailures();
  } finally {
    await client.close();
    await worker.close();
    blobCache.close();
    await rm(directory, { recursive: true, force: true });
  }
}

if (process.argv[1]?.endsWith('/integration.ts')) {
  verifyIndexSearchFlow(process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801').catch(error => {
    console.error(error);
    process.exitCode = 1;
  });
}
