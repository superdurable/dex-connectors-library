// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Registry, Worker, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../../src/index.js';
import { ConnectionName, TextMessageDeliveryFlow } from './flow.js';

const shutdown = new AbortController();
process.once('SIGINT', () => shutdown.abort());
process.once('SIGTERM', () => shutdown.abort());
try {
  const project = await loadFromEnvironment();
  const flow = new TextMessageDeliveryFlow(newProjectConnection(project, ConnectionName));
  const blobCache = openBlobCache({ directory: process.env.DEX_BLOB_CACHE_DIR || join(tmpdir(), 'dex-twilio-message-blobs'), maxBytes: 1 << 30 });
  const worker = new Worker(new Registry([flow]), blobCache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 10_000 });
    if (!shutdown.signal.aborted) await new Promise<void>(resolve => shutdown.signal.addEventListener('abort', () => resolve(), { once: true }));
  } finally { await worker.close(); blobCache.close(); }
} catch (error) {
  console.error('Twilio Messaging Worker stopped', error instanceof Error ? error.name : 'unknown error');
  process.exitCode = 1;
}
