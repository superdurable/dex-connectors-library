// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Registry, Worker, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '@superdurable/connector-google-gemini';
import { ConnectionName, GenerateSummaryFlow } from './flow/workflow.js';

const stop = new AbortController();
const handleSignal = (): void => { stop.abort(); };
process.once('SIGINT', handleSignal);
process.once('SIGTERM', handleSignal);
const directory = await mkdtemp(join(tmpdir(), 'dex-gemini-worker-'));
const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
let worker: Worker | undefined;
try {
  const project = await loadFromEnvironment();
  const flow = new GenerateSummaryFlow(newProjectConnection(project, ConnectionName));
  worker = new Worker(new Registry([flow]), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
  await worker.start();
  await worker.waitPullRegistered({ timeoutMs: 30_000, signal: stop.signal });
  await new Promise<void>(resolve => { if (stop.signal.aborted) resolve(); else stop.signal.addEventListener('abort', () => resolve(), { once: true }); });
} catch {
  console.error('Gemini Worker could not start or stopped with an error.');
  process.exitCode = 1;
} finally {
  await worker?.close(); cache.close(); await rm(directory, { recursive: true, force: true });
  process.off('SIGINT', handleSignal); process.off('SIGTERM', handleSignal);
}
