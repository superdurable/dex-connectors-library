// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { TriggerDeliveryAbortedError, newDexFlowTriggerTarget, newTriggerContext, type TriggerLogger } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection, newProjectPageTriggerRunner, type ConfluenceClientOptions } from '@superdurable/connector-atlassian-confluence';
import { ConnectionName, PageChangeFlow, PageChangedTriggerBinding, acceptEveryPageChange, mapToFlowInput, resolveFlowId } from './flow/workflow.js';

/** Settings of one page-changed application process. */
export interface PageChangedAppOptions {
  /** Dex gRPC address. */ serverAddress: string;
  /** Destination of every Trigger record. */ logger: TriggerLogger;
  /** Confluence client options for tests, such as a stand-in API sender. */ confluenceOptions?: ConfluenceClientOptions;
  /** Called once the Worker is registered and polling starts. */ onReady?(ready: { client: Client; flow: PageChangeFlow; wakePoller(): void }): void;
}

/** Run the Worker and poll the saved pageChanged binding until signal aborts or the runner fails. */
export async function runPageChangedApp(project: LoadedProject, options: PageChangedAppOptions, signal: AbortSignal): Promise<void> {
  const confluenceOptions = { ...options.confluenceOptions, triggerLogger: options.logger };
  const flow = new PageChangeFlow(newProjectConnection(project, ConnectionName, confluenceOptions));
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-confluence-page-changed-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const client = new Client(registry, cache, { serverAddress: options.serverAddress });
  const worker = new Worker(registry, cache, { serverAddress: options.serverAddress });
  // Each change is recorded in the binding's project Trigger inbox before the poll cursor moves past it.
  const runner = newProjectPageTriggerRunner(project, ConnectionName, [{
    bindingName: PageChangedTriggerBinding, target: newDexFlowTriggerTarget(client, flow, acceptEveryPageChange, resolveFlowId, mapToFlowInput, { logger: options.logger }),
  }], confluenceOptions);
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000, signal });
    const running = runner.run(newTriggerContext(signal));
    options.onReady?.({ client, flow, wakePoller: () => runner.wakePoller(PageChangedTriggerBinding) });
    try { await running; }
    catch (error) { if (!(error instanceof TriggerDeliveryAbortedError && signal.aborted)) throw error; }
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}
