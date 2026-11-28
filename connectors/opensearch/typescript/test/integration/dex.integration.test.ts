// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { Mock } from '../../src/mock.js';
import { OpenSearchClient, newConnection, type RetrievedDocument, type DeletedDocument, type IndexedDocument, type SearchPage } from '../../src/index.js';
import { ConnectionName, DocumentIndexFlow } from '../../examples/provider-operations/document-index-flow.js';
import { DocumentDeleteFlow } from '../../examples/provider-operations/document-delete-flow.js';
import { DocumentGetFlow } from '../../examples/provider-operations/document-get-flow.js';
import { DocumentSearchFlow } from '../../examples/provider-operations/document-search-flow.js';

it('runs all OpenSearch document operations through real Dex Workers and Clients', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  const directory = await mkdtemp(join(tmpdir(), 'dex-opensearch-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const mock = new Mock(ConnectionName);
  const indexFlow = new DocumentIndexFlow(mock.connection());
  const deleteFlow = new DocumentDeleteFlow(mock.connection());
  const searchFlow = new DocumentSearchFlow(mock.connection());
  let requests = 0;
  const provider = new OpenSearchClient({clusterUrl: 'http://127.0.0.1:12345'},
    {resolve: async () => ({username: 'reader', password: new SecretString('fixture')})},
    {send: async request => {
      expect(request.method).toBe('GET');
      expect(new URL(request.url).pathname).toBe('/products/_doc/sku&%21%2F42');
      requests++;
      const source = { _index: 'products', _id: 'sku&!/42', _version: 3, found: true, _source: {title: 'Meridian'} };
      return {status: requests === 1 ? 503 : 200, headers: {}, body: Buffer.from(JSON.stringify(source))};
    }});
  const getFlow = new DocumentGetFlow(newConnection(provider, { provider: 'opensearch', name: ConnectionName }));
  const registry = new Registry([indexFlow, deleteFlow, searchFlow, getFlow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });

    const indexId = `opensearch-typescript-index-${randomUUID()}`;
    await client.startFlow(indexFlow, indexId, {
      index: 'products', documentId: 'sku-42', source: { title: 'Meridian' },
    });
    const indexed = await client.waitForFlow(indexId, 60_000);
    expect(indexed.status, indexed.errorMessage).toBe('completed');
    expect(indexed.singleOutput<IndexedDocument>()).toMatchObject({ id: 'sku-42' });
    expect(mock.indexDocument().calls()).toHaveLength(1);

    const deleteId = `opensearch-typescript-delete-${randomUUID()}`;
    await client.startFlow(deleteFlow, deleteId, { index: 'products', documentId: 'sku-42' });
    const deleted = await client.waitForFlow(deleteId, 60_000);
    expect(deleted.status, deleted.errorMessage).toBe('completed');
    expect(deleted.singleOutput<DeletedDocument>()).toMatchObject({ wasDeleted: true });
    expect(mock.deleteDocument().calls()).toHaveLength(1);

    const searchId = `opensearch-typescript-search-${randomUUID()}`;
    await client.startFlow(searchFlow, searchId, { index: 'products', query: 'title:Meridian' });
    const searched = await client.waitForFlow(searchId, 60_000);
    expect(searched.status, searched.errorMessage).toBe('completed');
    expect(searched.singleOutput<SearchPage>()).toMatchObject({ total: 1 });
    expect(mock.searchDocuments().calls()).toHaveLength(1);
    const getId = `opensearch-typescript-get-${randomUUID()}`;
    await client.startFlow(getFlow, getId, { index: 'products', documentId: 'sku&!/42' });
    const retrieved = await client.waitForFlow(getId, 30_000);
    expect(retrieved.status, retrieved.errorMessage).toBe('completed');
    expect(retrieved.singleOutput<RetrievedDocument>()).toEqual({ index: 'products', id: 'sku&!/42', source: {title: 'Meridian'}, version: 3 });
    expect(requests).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 120_000);
