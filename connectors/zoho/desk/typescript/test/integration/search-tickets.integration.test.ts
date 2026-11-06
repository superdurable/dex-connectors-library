// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { expect, it } from 'vitest';
import { GeneratedMock, searchTicketsSearched } from '../../src/mock.js';
import type { SearchTicketsOutput } from '../../src/index.js';
import { ConnectionName, SearchTicketsFlow } from '../../examples/search-tickets/flow/workflow.js';

it('searches through a real Dex Worker and Client', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const mock = new GeneratedMock(ConnectionName);
  mock.searchTickets().default(searchTicketsSearched({ tickets: [], totalMatched: 51 }));
  const flow = new SearchTicketsFlow(mock.connection());
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-zoho-desk-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `zoho-desk-search-${randomUUID()}`;
    await client.startFlow(flow, flowId, { statuses: ['Open'] });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<SearchTicketsOutput>().totalMatched).toBe(51);
    mock.assertNoFailures();
  } finally {
    await client.close();
    await worker.close();
    cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 90_000);
