// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client as DexClient, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { TriggerDeliveryAbortedError, newDexFlowTriggerTarget, newTriggerContext } from '@superdurable/connectors-sdk';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection, newProjectDatabaseRowTriggerRunner } from '../../src/index.js';
import { ConnectionName, DatabaseRowChangeFlow, DatabaseRowChangedTriggerBinding, acceptEveryRowChange, mapToFlowInput, resolveFlowId } from './flow/workflow.js';

// Runs the Worker for the databaseRowChanged example Flow and polls the picked data source, one Flow per row version.
const stop = new AbortController();
process.once('SIGINT', () => stop.abort());
process.once('SIGTERM', () => stop.abort());
const serverAddress = process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801';
const directory = await mkdtemp(join(tmpdir(), 'dex-notion-row-changed-'));
const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
let worker: Worker | undefined;
let client: DexClient | undefined;
try {
  const project = await loadFromEnvironment();
  const flow = new DatabaseRowChangeFlow(newProjectConnection(project, ConnectionName));
  const registry = new Registry([flow]);
  worker = new Worker(registry, cache, { serverAddress });
  client = new DexClient(registry, cache, { serverAddress });
  await worker.start();
  await worker.waitPullRegistered({ timeoutMs: 30_000, signal: stop.signal });
  // Each row change is recorded in the binding's project Trigger inbox before the cursor moves past it.
  const runner = newProjectDatabaseRowTriggerRunner(project, ConnectionName, {
    databaseRowChangedRoutes: [{ bindingName: DatabaseRowChangedTriggerBinding, target: newDexFlowTriggerTarget(client, flow, acceptEveryRowChange, resolveFlowId, mapToFlowInput) }],
  });
  await runner.run(newTriggerContext(stop.signal)).catch((error: unknown) => {
    if (!(error instanceof TriggerDeliveryAbortedError)) throw error;
  });
} catch (error) {
  console.error('Notion row change Worker stopped:', error instanceof Error ? error.message : 'unknown error');
  process.exitCode = 1;
} finally {
  await client?.close();
  await worker?.close();
  cache.close();
  await rm(directory, { recursive: true, force: true });
}
