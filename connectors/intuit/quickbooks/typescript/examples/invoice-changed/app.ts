// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { TriggerDeliveryAbortedError, newDexFlowTriggerTarget, newTriggerContext, type TriggerLogger } from '@superdurable/connectors-sdk';
import type { Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection, newProjectInvoiceTriggerRunner, type Connection, type QuickBooksClientOptions } from '@superdurable/connector-intuit-quickbooks';
import { ConnectionName, InvoiceChangedFlow, InvoiceChangedTriggerBinding, acceptEveryInvoiceChange, mapToFlowInput, resolveFlowId } from './flow/workflow.js';

/** Settings of one invoice-changed application process. */
export interface InvoiceChangedOptions {
  /** Dex gRPC address. */ serverAddress: string;
  /** Destination of every Trigger record. */ logger: TriggerLogger;
  /** QuickBooks client options, such as a local provider URL for verification. */ quickBooksOptions?: QuickBooksClientOptions;
}

/**
 * Poll the company's invoices and start one Flow per invoice version. Each change is recorded in the binding's
 * project Trigger inbox before the watermark moves past it.
 */
export function newInvoiceChangeTriggerRunner(project: LoadedProject, connection: Connection, client: Client, flow: InvoiceChangedFlow): Runner {
  return newProjectInvoiceTriggerRunner(project, connection, {
    invoiceChangedRoutes: [{ bindingName: InvoiceChangedTriggerBinding, target: newDexFlowTriggerTarget(client, flow, acceptEveryInvoiceChange, resolveFlowId, mapToFlowInput) }],
  });
}

/** Run the Worker and the poll runner until signal aborts or the runner fails. */
export async function runInvoiceChanged(project: LoadedProject, options: InvoiceChangedOptions, signal: AbortSignal): Promise<void> {
  const connection = newProjectConnection(project, ConnectionName, { ...options.quickBooksOptions, logger: options.logger });
  const flow = new InvoiceChangedFlow(connection);
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-quickbooks-invoice-changed-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(registry, cache, { serverAddress: options.serverAddress });
  const client = new Client(registry, cache, { serverAddress: options.serverAddress });
  try {
    const runner = newInvoiceChangeTriggerRunner(project, connection, client, flow);
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000, signal });
    try { await runner.run(newTriggerContext(signal)); }
    catch (error) { if (!(error instanceof TriggerDeliveryAbortedError && signal.aborted)) throw error; }
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}
