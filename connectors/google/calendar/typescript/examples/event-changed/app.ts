// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { TriggerDeliveryAbortedError, newDexFlowTriggerTarget, newTriggerContext, type TriggerLogger } from '@superdurable/connectors-sdk';
import type { Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection, newProjectEventTriggerRunner, type CalendarClientOptions } from '@superdurable/connector-google-calendar';
import { ConnectionName, EventChangeFlow, EventChangedTriggerBinding, acceptEveryEventChange, mapToFlowInput, resolveFlowId } from './flow/workflow.js';

/** Settings of one event-changed application process. */
export interface EventChangeWatcherOptions {
  /** Dex gRPC address. */ serverAddress: string;
  /** Destination of every Trigger record. */ logger: TriggerLogger;
  /** Google Calendar client options for tests, such as a stand-in API sender. */ calendarOptions?: CalendarClientOptions;
  /** Called once the Worker is registered, with the poll runner, for example to wake its binding. */ onReady?(ready: { client: Client; flow: EventChangeFlow; runner: Runner }): void;
}

/**
 * Register the Flow's Worker, then poll the saved calendar until signal aborts, starting one Flow per change.
 * Each change is recorded in the binding's project Trigger inbox before the sync token moves past it.
 */
export async function runEventChangeWatcher(project: LoadedProject, options: EventChangeWatcherOptions, signal: AbortSignal): Promise<void> {
  const connection = newProjectConnection(project, ConnectionName, options.calendarOptions);
  const flow = new EventChangeFlow(connection);
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-google-calendar-event-changed-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const client = new Client(registry, cache, { serverAddress: options.serverAddress });
  const worker = new Worker(registry, cache, { serverAddress: options.serverAddress });
  // The Trigger event ID is the Flow-start request ID, so a redelivered change starts no second Flow.
  const runner = newProjectEventTriggerRunner(project, connection, [{
    bindingName: EventChangedTriggerBinding, target: newDexFlowTriggerTarget(client, flow, acceptEveryEventChange, resolveFlowId, mapToFlowInput, { logger: options.logger }),
  }], options.logger);
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000, signal });
    options.onReady?.({ client, flow, runner });
    try { await runner.run(newTriggerContext(signal)); }
    catch (error) { if (!(error instanceof TriggerDeliveryAbortedError && signal.aborted)) throw error; }
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}
