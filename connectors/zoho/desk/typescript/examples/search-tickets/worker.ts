// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Registry, Worker, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../../src/index.js';
import { ConnectionName, SearchTicketsFlow } from './flow/workflow.js';

const directory = await mkdtemp(join(tmpdir(), 'dex-zoho-desk-worker-'));
const project = await loadFromEnvironment();
const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
const worker = new Worker(new Registry([new SearchTicketsFlow(newProjectConnection(project, ConnectionName))]), cache,
  { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
const stop = new AbortController();
process.once('SIGINT', () => stop.abort());
process.once('SIGTERM', () => stop.abort());
try {
  await worker.start();
  await worker.waitPullRegistered({ timeoutMs: 30_000, signal: stop.signal });
  await new Promise<void>(resolve => {
    if (stop.signal.aborted) resolve();
    else stop.signal.addEventListener('abort', () => resolve(), { once: true });
  });
} finally {
  await worker.close();
  cache.close();
  await rm(directory, { recursive: true, force: true });
}
