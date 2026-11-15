// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { TriggerDeliveryAbortedError, newDexFlowTriggerTarget, newTriggerContext, type TriggerEvent, type TriggerLogger, type TriggerTarget } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import type { Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { newProjectConnection, newProjectConversationReceivedTriggerRunner, type ConversationReceivedEvent, type HiverClientOptions } from '../../src/index.js';
import { ConnectionName, ConversationReceivedTriggerBinding, NewConversationIntakeFlow, acceptEveryConversation, mapToFlowInput, resolveFlowId } from './flow/workflow.js';

/** Settings of one new-conversation intake process. */
export interface ConversationIntakeWorkerOptions {
  /** Dex gRPC address. */ serverAddress: string;
  /** Destination of every Trigger record. */ logger?: TriggerLogger;
  /** Hiver client options, such as a local stand-in's base URL or sender. */ hiverOptions?: HiverClientOptions;
  /** Called once the Worker is registered and the Trigger runs. */ onReady?(ready: { client: Client; runner: Runner }): void;
  /** Called after each conversation has started, or found, its Flow. */ onFlowStarted?(event: TriggerEvent<ConversationReceivedEvent>): void;
}

/** Start one intake Flow per new conversation, with the Trigger event ID as the Flow ID suffix. */
export function newConversationTarget(client: Client, flow: NewConversationIntakeFlow, options: Pick<ConversationIntakeWorkerOptions, 'logger' | 'onFlowStarted'> = {}): TriggerTarget<ConversationReceivedEvent> {
  const start = newDexFlowTriggerTarget(client, flow, acceptEveryConversation, resolveFlowId, mapToFlowInput, options.logger ? { logger: options.logger } : {});
  return { handleTrigger: async (context, event) => { await start.handleTrigger(context, event); options.onFlowStarted?.(event); } };
}

/**
 * Serve the Flow and run the conversationReceived Trigger until signal aborts. The first start records the
 * inbox's existing conversations as a baseline; each later conversation is recorded in the binding's
 * project Trigger inbox before the cursor moves past it.
 */
export async function runConversationIntakeWorker(project: LoadedProject, options: ConversationIntakeWorkerOptions, signal: AbortSignal): Promise<void> {
  const connection = newProjectConnection(project, ConnectionName, options.hiverOptions);
  const flow = new NewConversationIntakeFlow(connection);
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-hiver-new-conversation-intake-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(registry, cache, { serverAddress: options.serverAddress });
  const client = new Client(registry, cache, { serverAddress: options.serverAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000, signal });
    const runner = newProjectConversationReceivedTriggerRunner(project, connection, [{ bindingName: ConversationReceivedTriggerBinding, target: newConversationTarget(client, flow, options) }], options.logger ? { logger: options.logger } : {});
    const running = runner.run(newTriggerContext(signal));
    options.onReady?.({ client, runner });
    try { await running; }
    catch (error) { if (!(error instanceof TriggerDeliveryAbortedError && signal.aborted)) throw error; }
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}
