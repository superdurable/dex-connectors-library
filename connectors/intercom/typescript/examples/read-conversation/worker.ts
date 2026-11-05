// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Registry, Worker, openBlobCache, type Flow } from "@superdurable/dex";
import type { GetConversationInput } from "../../src/index.js";

/** Run the registered Flow until shutdown, closing owned Dex resources. */
export async function runReadConversationWorker(
  flow: Flow<GetConversationInput>,
  signal: AbortSignal,
  serverAddress: string,
  blobCacheDirectory: string,
): Promise<void> {
  const registry = new Registry([flow]);
  const blobCache = openBlobCache({
    directory: blobCacheDirectory,
    maxBytes: 1 << 30,
  });
  try {
    const worker = new Worker(registry, blobCache, { serverAddress });
    try {
      await worker.start();
      await worker.waitPullRegistered({ timeoutMs: 10_000, signal });
      if (!signal.aborted)
        await new Promise<void>((resolve) =>
          signal.addEventListener("abort", () => resolve(), { once: true }),
        );
    } finally {
      await worker.close();
    }
  } finally {
    blobCache.close();
  }
}
