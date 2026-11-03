// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Registry, Worker, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../../src/index.js';
import { CohereSearchFlow, ConnectionName } from './workflow.js';

const shutdown = new AbortController();
process.once('SIGINT', () => shutdown.abort());
process.once('SIGTERM', () => shutdown.abort());

try {
  const project = await loadFromEnvironment();
  const flow = new CohereSearchFlow(newProjectConnection(project, ConnectionName));
  const cache = openBlobCache({ directory: process.env.DEX_BLOB_CACHE_DIR || join(tmpdir(), 'dex-cohere-search-ts'), maxBytes: 1 << 30 });
  try {
    const worker = new Worker(new Registry([flow]), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
    try {
      await worker.start();
      await worker.waitPullRegistered({ timeoutMs: 30_000, signal: shutdown.signal });
      if (!shutdown.signal.aborted) await new Promise<void>(resolve => shutdown.signal.addEventListener('abort', () => resolve(), { once: true }));
    } finally { await worker.close(); }
  } finally { cache.close(); }
} catch (error) {
  console.error('Cohere search Worker stopped', error instanceof Error ? error.name : 'unknown error');
  process.exitCode = 1;
}
