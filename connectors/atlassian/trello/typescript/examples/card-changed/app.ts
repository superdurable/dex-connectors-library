// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { TriggerDeliveryAbortedError, newDexFlowTriggerTarget, newTriggerContext, type TriggerLogger } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection, newProjectCardTriggerRunner, type TrelloClientOptions } from '@superdurable/connector-atlassian-trello';
import { ConnectionName, CardChangeFlow, CardChangedTriggerBinding, acceptEveryCardChange, mapToFlowInput, resolveFlowId } from './flow/workflow.js';

/** Settings of one card-changed application process. */
export interface CardChangedAppOptions {
  /** Dex gRPC address. */ serverAddress: string;
  /** Destination of every Trigger record. */ logger: TriggerLogger;
  /** Trello client options for tests, such as a stand-in API sender. */ trelloOptions?: TrelloClientOptions;
  /** Called once the Worker is registered and polling starts. */ onReady?(ready: { client: Client; flow: CardChangeFlow; wakePoller(): void }): void;
}

/** Run the Worker and poll the saved cardChanged binding until signal aborts or the runner fails. */
export async function runCardChangedApp(project: LoadedProject, options: CardChangedAppOptions, signal: AbortSignal): Promise<void> {
  const trelloOptions = { ...options.trelloOptions, triggerLogger: options.logger };
  const flow = new CardChangeFlow(newProjectConnection(project, ConnectionName, trelloOptions));
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-trello-card-changed-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const client = new Client(registry, cache, { serverAddress: options.serverAddress });
  const worker = new Worker(registry, cache, { serverAddress: options.serverAddress });
  // Each card action is recorded in the binding's project Trigger inbox before the poll cursor moves past it.
  const runner = newProjectCardTriggerRunner(project, ConnectionName, [{
    bindingName: CardChangedTriggerBinding, target: newDexFlowTriggerTarget(client, flow, acceptEveryCardChange, resolveFlowId, mapToFlowInput, { logger: options.logger }),
  }], trelloOptions);
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000, signal });
    const running = runner.run(newTriggerContext(signal));
    options.onReady?.({ client, flow, wakePoller: () => runner.wakePoller(CardChangedTriggerBinding) });
    try { await running; }
    catch (error) { if (!(error instanceof TriggerDeliveryAbortedError && signal.aborted)) throw error; }
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}
