// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { TriggerDeliveryAbortedError, newDexFlowTriggerTarget, newTriggerContext, type TriggerLogger } from '@superdurable/connectors-sdk';
import type { Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection, newProjectTicketTriggerRunner, type ZohoDeskClientOptions } from '../../src/index.js';
import { ConnectionName, TicketChangeFlow, TicketChangedTriggerBinding, acceptEveryTicketChange, mapToFlowInput, resolveFlowId } from './flow/workflow.js';

/** Settings of one ticket-changed application process. */
export interface TicketChangeWatcherOptions {
  /** Dex gRPC address. */ serverAddress: string;
  /** Destination of every Trigger record. */ logger: TriggerLogger;
  /** Zoho Desk client options, such as a local API base URL for verification. */ zohoDeskOptions?: ZohoDeskClientOptions;
  /** Called once the Worker is registered and the poll runner is running. */ onReady?(ready: { client: Client; flow: TicketChangeFlow; runner: Runner }): void;
}

/**
 * Run the Worker for the example Flow and poll the binding's saved department until signal aborts or the runner
 * fails, starting one Flow per created or changed ticket version.
 */
export async function runTicketChangeWatcher(project: LoadedProject, options: TicketChangeWatcherOptions, signal: AbortSignal): Promise<void> {
  const zohoDeskOptions = { ...options.zohoDeskOptions, logger: options.logger };
  const flow = new TicketChangeFlow(newProjectConnection(project, ConnectionName, zohoDeskOptions));
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-zoho-desk-ticket-changed-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const client = new Client(registry, cache, { serverAddress: options.serverAddress });
  const worker = new Worker(registry, cache, { serverAddress: options.serverAddress });
  // Each change is recorded in the binding's project Trigger inbox before the search position moves past it.
  const runner = newProjectTicketTriggerRunner(project, ConnectionName, { ticketChangedRoutes: [{
    bindingName: TicketChangedTriggerBinding,
    target: newDexFlowTriggerTarget(client, flow, acceptEveryTicketChange, resolveFlowId, mapToFlowInput, { logger: options.logger }),
  }] }, zohoDeskOptions);
  const stop = new AbortController();
  const onAbort = () => stop.abort();
  signal.addEventListener('abort', onAbort, { once: true });
  let running: Promise<void> | undefined;
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000, signal: stop.signal });
    running = runner.run(newTriggerContext(stop.signal));
    options.onReady?.({ client, flow, runner });
    try { await running; }
    catch (error) { if (!(error instanceof TriggerDeliveryAbortedError && signal.aborted)) throw error; }
  } finally {
    signal.removeEventListener('abort', onAbort);
    stop.abort();
    await running?.catch(() => undefined);
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}
