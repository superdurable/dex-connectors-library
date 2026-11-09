// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Registry, Worker, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../../src/index.js';
import { ChatLookupFlow, ConnectionName } from './chat-lookup-flow.js';
import { MessageSendFlow } from './message-send-flow.js';

/** Run the send and chat lookup Flows with one Dex Web connection. */
export async function runTelegramWorker(signal: AbortSignal): Promise<void> {
  const project = await loadFromEnvironment();
  const connection = newProjectConnection(project, ConnectionName);
  const registry = new Registry([new MessageSendFlow(connection), new ChatLookupFlow(connection)]);
  const cache = openBlobCache({
    directory: process.env.DEX_BLOB_CACHE_DIR || join(tmpdir(), 'dex-telegram-ts-blobs'),
    maxBytes: 1 << 30,
  });
  const worker = new Worker(registry, cache, {
    serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801',
  });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000, signal });
    if (!signal.aborted) await new Promise<void>(resolve => signal.addEventListener('abort', () => resolve(), { once: true }));
  } finally { await worker.close(); cache.close(); }
}

if (process.argv[1]?.endsWith('/worker.ts')) {
  const shutdown = new AbortController();
  process.once('SIGINT', () => shutdown.abort());
  process.once('SIGTERM', () => shutdown.abort());
  runTelegramWorker(shutdown.signal).catch(error => {
    console.error('Telegram Worker stopped', error instanceof Error ? error.name : 'unknown error');
    process.exitCode = 1;
  });
}
