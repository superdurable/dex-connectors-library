// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Registry, Worker, openBlobCache } from '@superdurable/dex';
import type { ListHiverInboxesFlow } from './flow.js';

/** Run the Hiver inbox Flow Worker until shutdown. */
export async function runListHiverInboxesWorker(flow: ListHiverInboxesFlow, signal: AbortSignal, serverAddress: string): Promise<void> {
  const directory = await mkdtemp(join(tmpdir(), 'dex-hiver-inboxes-worker-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(new Registry([flow]), cache, { serverAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000, signal });
    if (!signal.aborted) await new Promise<void>(resolve => signal.addEventListener('abort', () => resolve(), { once: true }));
  } finally {
    await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}
