// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { TriggerDeliveryAbortedError, newDexFlowTriggerTarget, newTriggerContext, type TriggerEvent, type TriggerLogger, type TriggerTarget } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import type { Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { newProjectConnection, newProjectConversationTriggerRunner, type ConversationCreatedEvent, type ReamazeClientOptions } from '../../src/index.js';
import { ConnectionName, ConversationCreatedTriggerBinding, NewConversationFlow, acceptCustomerConversation, mapToFlowInput, resolveFlowId } from './flow/workflow.js';

/** Settings of one new-conversation process. */
export interface NewConversationWorkerOptions {
  /** Dex gRPC address. */ serverAddress: string;
  /** Destination of every Trigger record. */ logger?: TriggerLogger;
  /** Re:amaze client options, such as a local stand-in's base URL or sender. */ reamazeOptions?: ReamazeClientOptions;
  /** Called once the Worker is registered and the Trigger runs. */ onReady?(ready: { client: Client; runner: Runner }): void;
  /** Called after each conversation has started or found its Flow, or was skipped as spam. */ onDelivered?(event: TriggerEvent<ConversationCreatedEvent>): void;
}

/** Start one Flow per conversation that is not spam, with the Trigger event ID as the Flow ID suffix. */
export function newConversationTarget(client: Client, flow: NewConversationFlow, options: Pick<NewConversationWorkerOptions, 'logger' | 'onDelivered'> = {}): TriggerTarget<ConversationCreatedEvent> {
  const start = newDexFlowTriggerTarget(client, flow, acceptCustomerConversation, resolveFlowId, mapToFlowInput, options.logger ? { logger: options.logger } : {});
  return { handleTrigger: async (context, event) => { await start.handleTrigger(context, event); options.onDelivered?.(event); } };
}

/**
 * Serve the Flow and run the conversationCreated Trigger until signal aborts. The first start records the
 * existing conversations as a baseline; each later conversation is recorded in the binding's project Trigger
 * inbox before the cursor moves past it.
 */
export async function runNewConversationWorker(project: LoadedProject, options: NewConversationWorkerOptions, signal: AbortSignal): Promise<void> {
  const connection = newProjectConnection(project, ConnectionName, options.reamazeOptions);
  const flow = new NewConversationFlow(connection);
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-reamaze-new-conversation-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(registry, cache, { serverAddress: options.serverAddress });
  const client = new Client(registry, cache, { serverAddress: options.serverAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000, signal });
    const runner = newProjectConversationTriggerRunner(project, connection, [{ bindingName: ConversationCreatedTriggerBinding, target: newConversationTarget(client, flow, options) }], options.logger ? { logger: options.logger } : {});
    const running = runner.run(newTriggerContext(signal));
    options.onReady?.({ client, runner });
    try { await running; }
    catch (error) { if (!(error instanceof TriggerDeliveryAbortedError && signal.aborted)) throw error; }
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}
