// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Registry, Worker, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../../src/index.js';
import { ConnectionName, SmartsheetLookupFlow } from './flow.js';

const shutdown = new AbortController();
process.once('SIGINT', () => shutdown.abort());
process.once('SIGTERM', () => shutdown.abort());
const project = await loadFromEnvironment();
const registry = new Registry([new SmartsheetLookupFlow(newProjectConnection(project, ConnectionName))]);
const cache = openBlobCache({ directory: process.env.DEX_BLOB_CACHE_DIR || join(tmpdir(), 'dex-smartsheet-lookup-blobs'), maxBytes: 1 << 30 });
const worker = new Worker(registry, cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
try {
  await worker.start();
  await worker.waitPullRegistered({ timeoutMs: 10_000, signal: shutdown.signal });
  if (!shutdown.signal.aborted) await new Promise<void>(resolve => shutdown.signal.addEventListener('abort', () => resolve(), { once: true }));
} finally {
  await worker.close();
  cache.close();
}
