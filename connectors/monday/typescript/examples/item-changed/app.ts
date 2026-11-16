// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { TriggerDeliveryAbortedError, newDexFlowTriggerTarget, newTriggerContext, type TriggerEvent, type TriggerLogger, type TriggerTarget } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import type { Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { newProjectConnection, newProjectItemTriggerRunner, type ItemChangedEvent, type MondayClientOptions } from '@superdurable/connector-monday';
import { ConnectionName, ItemChangeFlow, ItemChangedTriggerBinding, acceptEveryItemChange, mapToFlowInput, resolveFlowId } from './flow.js';

/** Settings of one item-changed application process. */
export interface ItemChangeWorkerOptions {
  /** Dex gRPC address. */ serverAddress: string;
  /** Destination of every Trigger record. */ logger?: TriggerLogger;
  /** monday.com client options for tests, such as a stand-in API sender. */ mondayOptions?: MondayClientOptions;
  /** Called once the Worker is registered and the Trigger runs. */ onReady?(ready: { client: Client; runner: Runner }): void;
  /** Called after each change has started, or found, its Flow. */ onFlowStarted?(event: TriggerEvent<ItemChangedEvent>): void;
}

/** Start one ItemChangeFlow per change, named by its stable Trigger event ID. */
export function newItemChangeFlowTarget(client: Client, flow: ItemChangeFlow, options: Pick<ItemChangeWorkerOptions, 'logger' | 'onFlowStarted'> = {}): TriggerTarget<ItemChangedEvent> {
  const start = newDexFlowTriggerTarget(client, flow, acceptEveryItemChange, resolveFlowId, mapToFlowInput, options.logger ? { logger: options.logger } : {});
  return { handleTrigger: async (context, event) => { await start.handleTrigger(context, event); options.onFlowStarted?.(event); } };
}

/**
 * Serve the Flow and run the itemChanged Trigger until signal aborts. Each change is recorded in the binding's
 * project Trigger inbox before the board position moves past it; a binding without a saved board throws.
 */
export async function runItemChangeWorker(project: LoadedProject, options: ItemChangeWorkerOptions, signal: AbortSignal): Promise<void> {
  const connection = newProjectConnection(project, ConnectionName, options.mondayOptions);
  const flow = new ItemChangeFlow(connection);
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-monday-item-changed-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(registry, cache, { serverAddress: options.serverAddress });
  const client = new Client(registry, cache, { serverAddress: options.serverAddress });
  try {
    const target = newItemChangeFlowTarget(client, flow, options);
    const runner = newProjectItemTriggerRunner(project, connection, [{ bindingName: ItemChangedTriggerBinding, target }], options.logger ? { logger: options.logger } : {});
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000, signal });
    const running = runner.run(newTriggerContext(signal));
    options.onReady?.({ client, runner });
    try { await running; }
    catch (error) { if (!(error instanceof TriggerDeliveryAbortedError && signal.aborted)) throw error; }
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}
