// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { TriggerDeliveryAbortedError, newDexFlowTriggerTarget, newTriggerContext, type TriggerLogger } from '@superdurable/connectors-sdk';
import type { Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection, newProjectTicketTriggerRunner, type Connection, type GorgiasClientOptions } from '@superdurable/connector-gorgias';
import { ConnectionName, TicketChangedFlow, TicketChangedTriggerBinding, acceptEveryTicketChange, mapToFlowInput, resolveFlowId } from './flow/workflow.js';

/** Settings of one ticket-changed application process. */
export interface TicketChangedOptions {
  /** Dex gRPC address. */ serverAddress: string;
  /** Destination of every Trigger record. */ logger: TriggerLogger;
  /** Gorgias client options, such as a local apiBaseURL for verification. */ gorgiasOptions?: GorgiasClientOptions;
}

/**
 * Poll the saved ticketChanged binding and start one Flow per ticket event. Each event is recorded in the binding's
 * project Trigger inbox before the event log position moves past it.
 */
export function newTicketChangeTriggerRunner(project: LoadedProject, connection: Connection, client: Client, flow: TicketChangedFlow): Runner {
  return newProjectTicketTriggerRunner(project, connection, {
    ticketChangedRoutes: [{ bindingName: TicketChangedTriggerBinding, target: newDexFlowTriggerTarget(client, flow, acceptEveryTicketChange, resolveFlowId, mapToFlowInput) }],
  });
}

/** Run the Worker and the poll runner until signal aborts or the runner fails. */
export async function runTicketChanged(project: LoadedProject, options: TicketChangedOptions, signal: AbortSignal): Promise<void> {
  const connection = newProjectConnection(project, ConnectionName, { ...options.gorgiasOptions, logger: options.logger });
  const flow = new TicketChangedFlow(connection);
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-gorgias-ticket-changed-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(registry, cache, { serverAddress: options.serverAddress });
  const client = new Client(registry, cache, { serverAddress: options.serverAddress });
  try {
    const runner = newTicketChangeTriggerRunner(project, connection, client, flow);
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000, signal });
    try { await runner.run(newTriggerContext(signal)); }
    catch (error) { if (!(error instanceof TriggerDeliveryAbortedError && signal.aborted)) throw error; }
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}
