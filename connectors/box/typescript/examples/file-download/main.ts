// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Registry, Worker, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../../src/index.js';
import { BoxDownloadFileFlow, ConnectionName } from './flow.js';

const stop = new AbortController();
process.once('SIGINT', () => stop.abort());
process.once('SIGTERM', () => stop.abort());
const directory = await mkdtemp(join(tmpdir(), 'dex-box-ts-worker-'));
const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
let worker: Worker | undefined;
try {
  const project = await loadFromEnvironment();
  const connection = newProjectConnection(project, ConnectionName);
  worker = new Worker(new Registry([new BoxDownloadFileFlow(connection)]), cache,
    { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
  await worker.start();
  await worker.waitPullRegistered({ timeoutMs: 30_000, signal: stop.signal });
  if (!stop.signal.aborted) await new Promise<void>(resolve => stop.signal.addEventListener('abort', () => resolve(), { once: true }));
} finally {
  await worker?.close();
  cache.close();
  await rm(directory, { recursive: true, force: true });
}
