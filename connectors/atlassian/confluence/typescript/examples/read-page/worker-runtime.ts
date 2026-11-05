// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Registry, Worker, openBlobCache, type BlobCache, type Flow } from '@superdurable/dex';
import type { GetPageInput } from '@superdurable/connector-atlassian-confluence';

/** Run the Confluence read Worker until the host aborts. */
export async function runReadPageWorker(flow: Flow<GetPageInput>, signal: AbortSignal): Promise<void> {
  const directory = await mkdtemp(join(tmpdir(), 'dex-confluence-read-worker-'));
  let cache: BlobCache | undefined, worker: Worker | undefined;
  try {
    cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
    worker = new Worker(new Registry([flow]), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000, signal });
    await new Promise<void>(resolve => { if (signal.aborted) resolve(); else signal.addEventListener('abort', () => resolve(), { once: true }); });
  } finally {
    try { await worker?.close(); } finally { cache?.close(); await rm(directory, { recursive: true, force: true }); }
  }
}
