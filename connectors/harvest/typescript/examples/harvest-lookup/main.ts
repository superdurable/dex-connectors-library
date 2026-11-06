// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Registry, Worker, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../../src/index.js';
import { ConnectionName, HarvestLookupFlow } from './flow.js';

const stop = new AbortController();
const handleSignal = () => stop.abort();
process.once('SIGINT', handleSignal);
process.once('SIGTERM', handleSignal);

try {
  const project = await loadFromEnvironment();
  const flow = new HarvestLookupFlow(newProjectConnection(project, ConnectionName));
  const directory = await mkdtemp(join(tmpdir(), 'dex-harvest-worker-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(new Registry([flow]), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000, signal: stop.signal });
    await new Promise<void>(resolve => { if (stop.signal.aborted) resolve(); else stop.signal.addEventListener('abort', () => resolve(), { once: true }); });
  } finally {
    await worker.close();
    cache.close();
    await rm(directory, { recursive: true, force: true });
  }
} catch {
  console.error('Harvest lookup Worker could not start or stopped with an error.');
  process.exitCode = 1;
} finally {
  process.off('SIGINT', handleSignal);
  process.off('SIGTERM', handleSignal);
}
