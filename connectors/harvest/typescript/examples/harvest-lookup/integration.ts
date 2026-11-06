// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { GeneratedMock } from '../../src/index.js';
import { ConnectionName, HarvestLookupFlow } from './flow.js';

/** Exercise the checked-in Flow with a real Dex Worker and Client. */
export async function verifyHarvestLookup(serverAddress: string): Promise<void> {
  const mock = new GeneratedMock(ConnectionName);
  const flow = new HarvestLookupFlow(mock.connection());
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-harvest-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 1 << 30 });
  const worker = new Worker(registry, cache, { serverAddress });
  const client = new Client(registry, cache, { serverAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 10_000 });
    const flowId = `harvest-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { perPage: 5 });
    const result = await client.waitForFlow(flowId, 30_000);
    assert.equal(result.status, 'completed', result.errorMessage);
    assert.equal(result.singleOutput<{ timeEntries: { id: number }[] }>().timeEntries[0]?.id, 4001);
    assert.equal(mock.listProjects().calls()[0]?.input.perPage, 5);
    assert.equal(mock.listTimeEntries().calls()[0]?.input.perPage, 10);
    mock.assertNoFailures();
  } finally {
    await client.close();
    await worker.close();
    cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}

if (process.argv[1]?.endsWith('/integration.ts')) {
  verifyHarvestLookup(process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801').catch(error => { console.error(error); process.exitCode = 1; });
}
