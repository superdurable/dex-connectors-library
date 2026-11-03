// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Registry, Worker, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../../src/index.js';
import { ConnectionName, QueueConsumerFlow } from './flow.js';

const stop = new AbortController();
process.once('SIGINT', () => stop.abort());
process.once('SIGTERM', () => stop.abort());
const directory = await mkdtemp(join(tmpdir(), 'dex-sqs-consumer-'));
const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
try {
  const project = await loadFromEnvironment();
  const flow = new QueueConsumerFlow(newProjectConnection(project, ConnectionName));
  const worker = new Worker(new Registry([flow]), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000, signal: stop.signal });
    if (!stop.signal.aborted) await new Promise<void>(resolve => stop.signal.addEventListener('abort', () => resolve(), { once: true }));
  } finally { await worker.close(); }
} catch (error) {
  console.error('SQS queue consumer Worker stopped', error instanceof Error ? error.name : 'unknown error');
  process.exitCode = 1;
} finally { cache.close(); await rm(directory, { recursive: true, force: true }); }
