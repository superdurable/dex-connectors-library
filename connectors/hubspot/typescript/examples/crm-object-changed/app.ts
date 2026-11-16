// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { TriggerDeliveryAbortedError, newDexFlowTriggerTarget, newTriggerContext, type TriggerLogger } from '@superdurable/connectors-sdk';
import type { Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectCRMObjectTriggerRunner, newProjectConnection, type HubSpotClientOptions } from '@superdurable/connector-hubspot';
import { CRMObjectChangeFlow, CRMObjectChangedTriggerBinding, ConnectionName, DeliveredProperties, acceptEveryCRMObjectChange, mapToFlowInput, resolveFlowId } from './flow/workflow.js';

/** Settings of one crm-object-changed application process. */
export interface CRMObjectChangeWatcherOptions {
  /** Dex gRPC address. */ serverAddress: string;
  /** Destination of every Trigger record. */ logger: TriggerLogger;
  /** HubSpot client options for tests, such as a clock. */ hubspotOptions?: HubSpotClientOptions;
  /** Called once the Worker is registered and the poll runner is running. */ onReady?(ready: { client: Client; flow: CRMObjectChangeFlow; runner: Runner }): void;
}

/**
 * Run the Worker for the example Flow and poll the binding's saved object type until signal aborts or the runner
 * fails, starting one Flow per created or changed record.
 */
export async function runCRMObjectChangeWatcher(project: LoadedProject, options: CRMObjectChangeWatcherOptions, signal: AbortSignal): Promise<void> {
  const hubspotOptions = { ...options.hubspotOptions, logger: options.logger };
  const flow = new CRMObjectChangeFlow(newProjectConnection(project, ConnectionName, hubspotOptions));
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-hubspot-crm-object-changed-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const client = new Client(registry, cache, { serverAddress: options.serverAddress });
  const worker = new Worker(registry, cache, { serverAddress: options.serverAddress });
  // Each change is recorded in the binding's project Trigger inbox before the search cursor moves past it.
  const runner = newProjectCRMObjectTriggerRunner(project, ConnectionName, { crmObjectChangedRoutes: [{
    bindingName: CRMObjectChangedTriggerBinding, properties: DeliveredProperties,
    target: newDexFlowTriggerTarget(client, flow, acceptEveryCRMObjectChange, resolveFlowId, mapToFlowInput, { logger: options.logger }),
  }] }, hubspotOptions);
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
