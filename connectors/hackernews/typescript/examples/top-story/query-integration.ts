// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, type Flow, Registry, Worker, openBlobCache } from '@superdurable/dex';

/** Serve one Flow, wait for its result and close the Worker and Client. */
export async function readOnce<Request, Output>(flow: Flow<Request>, request: Request): Promise<Output> {
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-hackernews-query-'));
  const cache = openBlobCache({ directory, maxBytes: 1 << 24 });
  const serverAddress = process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801';
  const worker = new Worker(registry, cache, { serverAddress });
  const client = new Client(registry, cache, { serverAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 15_000 });
    const id = `${flow.getFlowType()}-${randomUUID()}`;
    await client.startFlow(flow, id, request);
    const result = await client.waitForFlow(id, 30_000);
    assert.equal(result.status, 'completed', result.errorMessage);
    return result.singleOutput<Output>();
  } finally {
    await client.close();
    await worker.close();
    cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}
