// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Registry, Worker, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../../src/index.js';
import { ConnectionName, OutboundLookupFlow } from './flow.js';

const stop = new AbortController();
const handleSignal = (): void => stop.abort();
process.once('SIGINT', handleSignal);
process.once('SIGTERM', handleSignal);

try {
  const project = await loadFromEnvironment();
  const flow = new OutboundLookupFlow(newProjectConnection(project, ConnectionName));
  const directory = await mkdtemp(join(tmpdir(), 'dex-postmark-worker-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(new Registry([flow]), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000, signal: stop.signal });
    if (!stop.signal.aborted) await new Promise<void>(resolve => stop.signal.addEventListener('abort', () => resolve(), { once: true }));
  } finally {
    await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
} catch {
  console.error('Postmark outbound lookup Worker stopped with an error.');
  process.exitCode = 1;
} finally {
  process.off('SIGINT', handleSignal);
  process.off('SIGTERM', handleSignal);
}
