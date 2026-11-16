// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { TriggerDeliveryAbortedError, newDexFlowTriggerTarget, newTriggerContext, type TriggerLogger } from '@superdurable/connectors-sdk';
import type { Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectRecordTriggerRunner, newProjectConnection, type SalesforceClientOptions } from '@superdurable/connector-salesforce';
import { RecordChangeFlow, RecordModifiedTriggerBinding, ConnectionName, acceptEveryRecordChange, mapToFlowInput, resolveFlowId } from './flow/workflow.js';

/** Settings of one record-changes application process. */
export interface RecordChangeWatcherOptions {
  /** Dex gRPC address. */ serverAddress: string;
  /** Destination of every Trigger record. */ logger: TriggerLogger;
  /** Salesforce client options for tests, such as a provider sender or clock. */ salesforceOptions?: SalesforceClientOptions;
  /** Called once the Worker is registered and the poll runner is running. */ onReady?(ready: { client: Client; flow: RecordChangeFlow; runner: Runner }): void;
}

/**
 * Run the Worker for the example Flow and poll the binding's saved object until signal aborts or the runner
 * fails, starting one Flow per created or modified record.
 */
export async function runRecordChangeWatcher(project: LoadedProject, options: RecordChangeWatcherOptions, signal: AbortSignal): Promise<void> {
  const salesforceOptions = { ...options.salesforceOptions, logger: options.logger };
  const flow = new RecordChangeFlow(newProjectConnection(project, ConnectionName, salesforceOptions));
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-salesforce-record-changes-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const client = new Client(registry, cache, { serverAddress: options.serverAddress });
  const worker = new Worker(registry, cache, { serverAddress: options.serverAddress });
  // Each change is recorded in the binding's project Trigger inbox before the cursor moves past it.
  const runner = newProjectRecordTriggerRunner(project, ConnectionName, { recordModifiedPolledRoutes: [{
    bindingName: RecordModifiedTriggerBinding,
    target: newDexFlowTriggerTarget(client, flow, acceptEveryRecordChange, resolveFlowId, mapToFlowInput, { logger: options.logger }),
  }] }, salesforceOptions);
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
