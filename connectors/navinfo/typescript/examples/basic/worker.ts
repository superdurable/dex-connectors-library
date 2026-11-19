// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Registry, Worker, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../../src/index.js';
import { ConnectionName, NavInfoInspectionFlow } from './flow.js';

/** Serve the NavInfo example Flow. */
export async function runWorker(signal: AbortSignal): Promise<void> {
  const project = await loadFromEnvironment();
  const connection = newProjectConnection(project, ConnectionName);
  const registry = new Registry([new NavInfoInspectionFlow(connection)]);
  const blobCache = openBlobCache({ directory: process.env.DEX_BLOB_CACHE_DIR || join(tmpdir(), 'dex-navinfo-ts-blobs'), maxBytes: 1 << 30 });
  const worker = new Worker(registry, blobCache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000, signal });
    if (!signal.aborted) await new Promise<void>(resolve => signal.addEventListener('abort', () => resolve(), { once: true }));
  } finally { await worker.close(); blobCache.close(); }
}

if (process.argv[1]?.endsWith('/worker.ts')) {
  const shutdown = new AbortController();
  process.once('SIGINT', () => shutdown.abort());
  process.once('SIGTERM', () => shutdown.abort());
  runWorker(shutdown.signal).catch(error => {
    console.error('NavInfo Worker stopped', error instanceof Error ? error.name : 'unknown error');
    process.exitCode = 1;
  });
}
