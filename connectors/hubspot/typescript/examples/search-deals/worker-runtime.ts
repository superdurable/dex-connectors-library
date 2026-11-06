// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Registry, Worker, openBlobCache, type BlobCache, type Flow } from '@superdurable/dex';
import type { SearchObjectsInput } from '@superdurable/connector-hubspot';

/** Run the HubSpot Flow Worker until aborted. */
export async function runSearchDealsWorker(flow: Flow<SearchObjectsInput>, signal: AbortSignal): Promise<void> {
  const directory = await mkdtemp(join(tmpdir(), 'dex-hubspot-search-worker-'));
  let cache: BlobCache | undefined;
  let worker: Worker | undefined;
  try {
    cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
    worker = new Worker(new Registry([flow]), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000, signal });
    await new Promise<void>(resolve => {
      if (signal.aborted) { resolve(); return; }
      signal.addEventListener('abort', () => resolve(), { once: true });
    });
  } finally {
    try { await worker?.close(); } finally { cache?.close(); await rm(directory, { recursive: true, force: true }); }
  }
}
