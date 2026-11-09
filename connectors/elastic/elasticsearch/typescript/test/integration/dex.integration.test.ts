// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { expect, it } from 'vitest';
import { Mock } from '../../src/mock.js';
import type { DeletedDocument, IndexedDocument, SearchPage } from '../../src/index.js';
import { ConnectionName, DocumentIndexFlow } from '../../examples/provider-operations/document-index-flow.js';
import { DocumentDeleteFlow } from '../../examples/provider-operations/document-delete-flow.js';
import { DocumentSearchFlow } from '../../examples/provider-operations/document-search-flow.js';

it('runs all Elasticsearch document operations through real Dex Workers and Clients', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  const directory = await mkdtemp(join(tmpdir(), 'dex-elasticsearch-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const mock = new Mock(ConnectionName);
  const indexFlow = new DocumentIndexFlow(mock.connection());
  const deleteFlow = new DocumentDeleteFlow(mock.connection());
  const searchFlow = new DocumentSearchFlow(mock.connection());
  const registry = new Registry([indexFlow, deleteFlow, searchFlow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });

    const indexId = `elasticsearch-typescript-index-${randomUUID()}`;
    await client.startFlow(indexFlow, indexId, {
      index: 'products', documentId: 'sku-42', source: { title: 'Meridian' },
    });
    const indexed = await client.waitForFlow(indexId, 60_000);
    expect(indexed.status, indexed.errorMessage).toBe('completed');
    expect(indexed.singleOutput<IndexedDocument>()).toMatchObject({ id: 'sku-42' });
    expect(mock.indexDocument().calls()).toHaveLength(1);

    const deleteId = `elasticsearch-typescript-delete-${randomUUID()}`;
    await client.startFlow(deleteFlow, deleteId, { index: 'products', documentId: 'sku-42' });
    const deleted = await client.waitForFlow(deleteId, 60_000);
    expect(deleted.status, deleted.errorMessage).toBe('completed');
    expect(deleted.singleOutput<DeletedDocument>()).toMatchObject({ wasDeleted: true });
    expect(mock.deleteDocument().calls()).toHaveLength(1);

    const searchId = `elasticsearch-typescript-search-${randomUUID()}`;
    await client.startFlow(searchFlow, searchId, { index: 'products', query: 'title:Meridian' });
    const searched = await client.waitForFlow(searchId, 60_000);
    expect(searched.status, searched.errorMessage).toBe('completed');
    expect(searched.singleOutput<SearchPage>()).toMatchObject({ total: 1 });
    expect(mock.searchDocuments().calls()).toHaveLength(1);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 120_000);
