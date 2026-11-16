// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client as DexClient, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { TriggerDeliveryAbortedError, newTriggerContext } from '@superdurable/connectors-sdk';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../../src/index.js';
import { ConnectionName, RecordChangedIntakeFlow } from './flow/workflow.js';
import { newRecordChangedIntakeRunner } from './runner.js';

// Runs the Worker for the record-changed intake Flow and polls the picked table, one Flow per change; each
// Flow reads its record back through the same project connection.
const stop = new AbortController();
process.once('SIGINT', () => stop.abort());
process.once('SIGTERM', () => stop.abort());
const serverAddress = process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801';
const directory = await mkdtemp(join(tmpdir(), 'dex-airtable-record-changed-'));
const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
let worker: Worker | undefined;
let client: DexClient | undefined;
try {
  const project = await loadFromEnvironment();
  const flow = new RecordChangedIntakeFlow(newProjectConnection(project, ConnectionName));
  const registry = new Registry([flow]);
  worker = new Worker(registry, cache, { serverAddress });
  client = new DexClient(registry, cache, { serverAddress });
  await worker.start();
  await worker.waitPullRegistered({ timeoutMs: 30_000, signal: stop.signal });
  await newRecordChangedIntakeRunner(project, client, flow).run(newTriggerContext(stop.signal)).catch((error: unknown) => {
    if (!(error instanceof TriggerDeliveryAbortedError)) throw error;
  });
} catch (error) {
  console.error('Airtable record-changed intake Worker stopped:', error instanceof Error ? error.message : 'unknown error');
  process.exitCode = 1;
} finally {
  await client?.close();
  await worker?.close();
  cache.close();
  await rm(directory, { recursive: true, force: true });
}
