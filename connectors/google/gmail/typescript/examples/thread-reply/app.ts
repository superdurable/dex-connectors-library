// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { TriggerDeliveryAbortedError, newDexFlowTriggerTarget, newDexRPCTriggerTarget, newTriggerContext, withTriggerLoggerAttributes, type TriggerLogger } from '@superdurable/connectors-sdk';
import { decodeTriggerConfiguration, type LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import {
  ConnectorID, MessageReceivedTriggerConfigurationSchema, MessageReceivedTriggerDefinition, ReplyReceivedTriggerConfigurationSchema, ReplyReceivedTriggerDefinition,
  newProjectConnection, newProjectMessageTriggerRunner, type GmailClientOptions,
} from '@superdurable/connector-google-gmail';
import {
  ConnectionName, GmailThreadReplyFlow, ReplyTriggerBinding, StartTriggerBinding, loadReplyMessageConfiguration, mapToFlowInput, mapToReceiveEmailReplyInput, newReplyTriggerFilter, newStartTriggerFilter, resolveFlowId,
} from './flow/workflow.js';

/** Settings of one thread-reply application process. */
export interface ThreadReplyApplicationOptions {
  /** Dex gRPC address. */ serverAddress: string;
  /** Destination of every Trigger record. */ logger: TriggerLogger;
  /** Gmail client options for tests, such as a fake provider transport. */ gmailOptions?: GmailClientOptions;
  /** Called once the Worker is registered, before the runner polls. */ onReady?(ready: { client: Client; flow: GmailThreadReplyFlow }): void;
}

/** Run the Worker and the shared Gmail message Trigger runner until signal aborts or the runner fails. */
export async function runThreadReplyApplication(project: LoadedProject, options: ThreadReplyApplicationOptions, signal: AbortSignal): Promise<void> {
  const connection = newProjectConnection(project, ConnectionName, options.gmailOptions);
  // Like the Go example, the process refuses to start without the completion reply saved in Dex Web.
  const flow = new GmailThreadReplyFlow(connection, loadReplyMessageConfiguration(project.configuration));
  const startConfiguration = decodeTriggerConfiguration(project.configuration, { connectorId: ConnectorID, connectionName: ConnectionName, triggerName: MessageReceivedTriggerDefinition.trigger.triggerName, bindingName: StartTriggerBinding }, MessageReceivedTriggerConfigurationSchema);
  const replyConfiguration = decodeTriggerConfiguration(project.configuration, { connectorId: ConnectorID, connectionName: ConnectionName, triggerName: ReplyReceivedTriggerDefinition.trigger.triggerName, bindingName: ReplyTriggerBinding }, ReplyReceivedTriggerConfigurationSchema);
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-gmail-thread-reply-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(registry, cache, { serverAddress: options.serverAddress });
  const client = new Client(registry, cache, { serverAddress: options.serverAddress });
  const bindingLogger = (trigger: string, binding: string) => withTriggerLoggerAttributes(options.logger, { connector: ConnectorID, connection: ConnectionName, trigger, binding });
  // One ordered runner delivers every root before any reply. The runner, its durable inboxes, and the
  // Dex targets log every skipped message and every retry.
  const runner = newProjectMessageTriggerRunner(project, ConnectionName, {
    messageReceivedRoutes: [{ bindingName: StartTriggerBinding, target: newDexFlowTriggerTarget(client, flow, newStartTriggerFilter(startConfiguration), resolveFlowId, mapToFlowInput,
      { logger: bindingLogger(MessageReceivedTriggerDefinition.trigger.triggerName, StartTriggerBinding) }) }],
    replyReceivedRoutes: [{ bindingName: ReplyTriggerBinding, target: newDexRPCTriggerTarget(client, flow.receiveEmailReply, newReplyTriggerFilter(replyConfiguration), resolveFlowId, mapToReceiveEmailReplyInput,
      { logger: bindingLogger(ReplyReceivedTriggerDefinition.trigger.triggerName, ReplyTriggerBinding), rpcName: 'receiveEmailReply' }) }],
  }, { ...options.gmailOptions, logger: options.logger });
  const stop = new AbortController();
  const onAbort = () => stop.abort();
  if (signal.aborted) stop.abort();
  signal.addEventListener('abort', onAbort, { once: true });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000, signal: stop.signal });
    options.onReady?.({ client, flow });
    try { await runner.run(newTriggerContext(stop.signal)); }
    catch (error) { if (!(error instanceof TriggerDeliveryAbortedError && signal.aborted)) throw error; }
  } finally {
    signal.removeEventListener('abort', onAbort);
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}
