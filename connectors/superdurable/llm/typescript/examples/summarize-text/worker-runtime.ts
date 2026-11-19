// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Registry, Worker, openBlobCache, type Flow } from '@superdurable/dex';
import type { GenerateTextRequest } from '../../src/index.js';

/** Run the summarize-text Worker against a selected Dex dev server. */
export async function runWorker(flow: Flow<GenerateTextRequest>, signal: AbortSignal, serverAddress: string): Promise<void> {
  const directory = await mkdtemp(join(tmpdir(), 'dex-llm-worker-'));
  const blobCache = openBlobCache({ directory, maxBytes: 64 << 20 });
  const worker = new Worker(new Registry([flow]), blobCache, { serverAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000, signal });
    if (!signal.aborted) await new Promise<void>(resolve => signal.addEventListener('abort', () => resolve(), { once: true }));
  } finally {
    await worker.close();
    blobCache.close();
    await rm(directory, { recursive: true, force: true });
  }
}
