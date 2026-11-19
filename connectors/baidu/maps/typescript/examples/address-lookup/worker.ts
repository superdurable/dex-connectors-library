// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Registry, Worker, openBlobCache, type Flow } from '@superdurable/dex';
import type { GeocodeAddressInput } from '../../src/index.js';

/** Run the registered address-lookup Flow until shutdown, closing every owned Dex resource. */
export async function runAddressLookupWorker(
  flow: Flow<GeocodeAddressInput>,
  signal: AbortSignal,
  serverAddress: string,
  blobCacheDirectory: string,
): Promise<void> {
  const registry = new Registry([flow]);
  const blobCache = openBlobCache({ directory: blobCacheDirectory, maxBytes: 1 << 30 });
  try {
    const worker = new Worker(registry, blobCache, { serverAddress });
    try {
      await worker.start();
      await worker.waitPullRegistered({ timeoutMs: 10_000, signal });
      if (!signal.aborted) await new Promise<void>(resolve => signal.addEventListener('abort', () => resolve(), { once: true }));
    } finally {
      await worker.close();
    }
  } finally {
    blobCache.close();
  }
}
