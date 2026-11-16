// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { TriggerDeliveryAbortedError, newDexFlowTriggerTarget, newTriggerContext, type TriggerEvent, type TriggerLogger, type TriggerTarget } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import type { Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { newProjectConnection, newProjectEmployeeChangedTriggerRunner, type EmployeeChangedEvent, type BambooHRClientOptions } from '../../src/index.js';
import { ConnectionName, EmployeeChangeFlow, EmployeeChangedTriggerBinding, acceptEveryEmployeeChange, mapToFlowInput, resolveFlowId } from './flow.js';

/** Settings of one employee-changed application process. */
export interface EmployeeChangeWorkerOptions {
  /** Dex gRPC address. */ serverAddress: string;
  /** Destination of every Trigger record. */ logger?: TriggerLogger;
  /** BambooHR client options for tests, such as a stand-in API sender. */ bambooHROptions?: BambooHRClientOptions;
  /** Called once the Worker is registered and the Trigger runs. */ onReady?(ready: { client: Client; runner: Runner }): void;
  /** Called after each change has started, or found, its Flow. */ onFlowStarted?(event: TriggerEvent<EmployeeChangedEvent>): void;
}

/** Start one EmployeeChangeFlow per change, named by its stable Trigger event ID. */
export function newEmployeeChangeFlowTarget(client: Client, flow: EmployeeChangeFlow, options: Pick<EmployeeChangeWorkerOptions, 'logger' | 'onFlowStarted'> = {}): TriggerTarget<EmployeeChangedEvent> {
  const start = newDexFlowTriggerTarget(client, flow, acceptEveryEmployeeChange, resolveFlowId, mapToFlowInput, options.logger ? { logger: options.logger } : {});
  return { handleTrigger: async (context, event) => { await start.handleTrigger(context, event); options.onFlowStarted?.(event); } };
}

/**
 * Serve the Flow and run the employeeChanged Trigger until signal aborts. Each change is recorded in the
 * binding's project Trigger inbox before the cursor moves past it.
 */
export async function runEmployeeChangeWorker(project: LoadedProject, options: EmployeeChangeWorkerOptions, signal: AbortSignal): Promise<void> {
  const connection = newProjectConnection(project, ConnectionName, options.bambooHROptions);
  const flow = new EmployeeChangeFlow(connection);
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-bamboohr-employee-changed-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(registry, cache, { serverAddress: options.serverAddress });
  const client = new Client(registry, cache, { serverAddress: options.serverAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000, signal });
    const runner = newProjectEmployeeChangedTriggerRunner(project, connection, [{ bindingName: EmployeeChangedTriggerBinding, target: newEmployeeChangeFlowTarget(client, flow, options) }], options.logger ? { logger: options.logger } : {});
    const running = runner.run(newTriggerContext(signal));
    options.onReady?.({ client, runner });
    try { await running; }
    catch (error) { if (!(error instanceof TriggerDeliveryAbortedError && signal.aborted)) throw error; }
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}
