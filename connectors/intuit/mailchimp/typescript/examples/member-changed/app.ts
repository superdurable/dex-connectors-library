// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { TriggerDeliveryAbortedError, newDexFlowTriggerTarget, newTriggerContext, type TriggerEvent, type TriggerLogger, type TriggerTarget } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import type { Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { newProjectConnection, newProjectMemberTriggerRunner, type MailchimpClientOptions, type MemberChangedEvent } from '@superdurable/connector-intuit-mailchimp';
import { ConnectionName, MemberChangeFlow, MemberChangedTriggerBinding, acceptEveryMemberChange, mapToFlowInput, resolveFlowId } from './flow.js';

/** Settings of one member-changed application process. */
export interface MemberChangeWorkerOptions {
  /** Dex gRPC address. */ serverAddress: string;
  /** Destination of every Trigger record. */ logger?: TriggerLogger;
  /** Mailchimp client options for tests, such as a stand-in API sender and base URL. */ mailchimpOptions?: MailchimpClientOptions;
  /** Called once the Worker is registered and the Trigger runs. */ onReady?(ready: { client: Client; runner: Runner }): void;
  /** Called after each change has started, or found, its Flow. */ onFlowStarted?(event: TriggerEvent<MemberChangedEvent>): void;
}

/** Start one MemberChangeFlow per change, named by its stable Trigger event ID. */
export function newMemberChangeFlowTarget(client: Client, flow: MemberChangeFlow, options: Pick<MemberChangeWorkerOptions, 'logger' | 'onFlowStarted'> = {}): TriggerTarget<MemberChangedEvent> {
  const start = newDexFlowTriggerTarget(client, flow, acceptEveryMemberChange, resolveFlowId, mapToFlowInput, options.logger ? { logger: options.logger } : {});
  return { handleTrigger: async (context, event) => { await start.handleTrigger(context, event); options.onFlowStarted?.(event); } };
}

/**
 * Serve the Flow and run the memberChanged Trigger until signal aborts. Each change is recorded in the
 * binding's project Trigger inbox before the cursor moves past it.
 */
export async function runMemberChangeWorker(project: LoadedProject, options: MemberChangeWorkerOptions, signal: AbortSignal): Promise<void> {
  const connection = newProjectConnection(project, ConnectionName, options.mailchimpOptions);
  const flow = new MemberChangeFlow(connection);
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-mailchimp-member-changed-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(registry, cache, { serverAddress: options.serverAddress });
  const client = new Client(registry, cache, { serverAddress: options.serverAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000, signal });
    const runner = newProjectMemberTriggerRunner(project, connection, [{ bindingName: MemberChangedTriggerBinding, target: newMemberChangeFlowTarget(client, flow, options) }], options.logger ? { logger: options.logger } : {});
    const running = runner.run(newTriggerContext(signal));
    options.onReady?.({ client, runner });
    try { await running; }
    catch (error) { if (!(error instanceof TriggerDeliveryAbortedError && signal.aborted)) throw error; }
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}
