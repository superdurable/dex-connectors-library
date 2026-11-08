// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Registry, Worker, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../../src/index.js';
import { ConnectionName, TaskListsInspectionFlow } from './lists-flow.js';
import { TasksInspectionFlow } from './tasks-flow.js';

/** Serve both To Do inspection Flows through one project connection. */
export async function runTodoWorker(signal: AbortSignal): Promise<void> {
  const project = await loadFromEnvironment();
  const connection = newProjectConnection(project, ConnectionName);
  const registry = new Registry([new TaskListsInspectionFlow(connection), new TasksInspectionFlow(connection)]);
  const blobCache = openBlobCache({ directory: process.env.DEX_BLOB_CACHE_DIR || join(tmpdir(), 'dex-microsoft-todo-ts-blobs'), maxBytes: 1 << 30 });
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
  runTodoWorker(shutdown.signal).catch(error => {
    console.error('Microsoft To Do Worker stopped', error instanceof Error ? error.name : 'unknown error');
    process.exitCode = 1;
  });
}
