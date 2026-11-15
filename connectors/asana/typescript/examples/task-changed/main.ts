// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client as DexClient, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { TriggerDeliveryAbortedError, newDexFlowTriggerTarget, newTriggerContext } from '@superdurable/connectors-sdk';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection, newProjectTaskTriggerRunner } from '../../src/index.js';
import { ConnectionName, TaskChangedFlow, TaskChangedTriggerBinding, acceptEveryTaskChange, mapToFlowInput, resolveFlowId } from './flow/workflow.js';

// Runs the Worker for the taskChanged example Flow and polls the picked project, one Flow per task event.
const stop = new AbortController();
process.once('SIGINT', () => stop.abort());
process.once('SIGTERM', () => stop.abort());
const serverAddress = process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801';
const directory = await mkdtemp(join(tmpdir(), 'dex-asana-task-changed-'));
const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
let worker: Worker | undefined;
let client: DexClient | undefined;
try {
  const project = await loadFromEnvironment();
  const flow = new TaskChangedFlow(newProjectConnection(project, ConnectionName));
  const registry = new Registry([flow]);
  worker = new Worker(registry, cache, { serverAddress });
  client = new DexClient(registry, cache, { serverAddress });
  await worker.start();
  await worker.waitPullRegistered({ timeoutMs: 30_000, signal: stop.signal });
  // Each task event is recorded in the binding's project Trigger inbox before the sync token moves past it.
  const runner = newProjectTaskTriggerRunner(project, ConnectionName, {
    taskChangedRoutes: [{ bindingName: TaskChangedTriggerBinding, target: newDexFlowTriggerTarget(client, flow, acceptEveryTaskChange, resolveFlowId, mapToFlowInput) }],
  });
  await runner.run(newTriggerContext(stop.signal)).catch((error: unknown) => {
    if (!(error instanceof TriggerDeliveryAbortedError)) throw error;
  });
} catch (error) {
  console.error('Asana task change Worker stopped:', error instanceof Error ? error.message : 'unknown error');
  process.exitCode = 1;
} finally {
  await client?.close();
  await worker?.close();
  cache.close();
  await rm(directory, { recursive: true, force: true });
}
