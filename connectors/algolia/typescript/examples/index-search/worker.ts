// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Registry, Worker, openBlobCache } from '@superdurable/dex';
import { IndexSearchFlow, ReplaceObjectFlow } from './flow.js';

/** Run the example Worker until shutdown, closing owned Dex resources. */
export async function runIndexSearchWorker(flows: [IndexSearchFlow, ReplaceObjectFlow], signal: AbortSignal, serverAddress: string, blobCacheDirectory: string): Promise<void> {
  const registry = new Registry(flows);
  const blobCache = openBlobCache({ directory: blobCacheDirectory, maxBytes: 1 << 30 });
  try {
    const worker = new Worker(registry, blobCache, { serverAddress });
    try {
      await worker.start();
      await worker.waitPullRegistered({ timeoutMs: 10_000, signal });
      if (!signal.aborted) await new Promise<void>(resolve => signal.addEventListener('abort', () => resolve(), { once: true }));
    } finally { await worker.close(); }
  } finally { blobCache.close(); }
}
