// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { Registry, Worker, openBlobCache } from '@superdurable/dex';
import { newProjectConnection } from '../../src/index.js';
import { ConnectionName, MovieSearchFlow } from './flow.js';

const stop = new AbortController();
process.once('SIGINT', () => stop.abort());
process.once('SIGTERM', () => stop.abort());

try {
  const project = await loadFromEnvironment();
  const flow = new MovieSearchFlow(newProjectConnection(project, ConnectionName));
  const cache = openBlobCache({ directory: process.env.DEX_BLOB_CACHE_DIR || join(tmpdir(), 'dex-tmdb-movie-search'), maxBytes: 1 << 30 });
  const worker = new Worker(new Registry([flow]), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000, signal: stop.signal });
    if (!stop.signal.aborted) await new Promise<void>(resolve => stop.signal.addEventListener('abort', () => resolve(), { once: true }));
  } finally { await worker.close(); cache.close(); }
} catch (error) {
  console.error('TMDB Worker stopped', error instanceof Error ? error.name : 'unknown error');
  process.exitCode = 1;
}
