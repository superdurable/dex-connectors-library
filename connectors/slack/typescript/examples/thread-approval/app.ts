// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { TriggerDeliveryAbortedError, newDexFlowTriggerTarget, newDexRPCTriggerTarget, newTriggerContext, withTriggerLoggerAttributes, type TriggerLogger } from '@superdurable/connectors-sdk';
import { decodeTriggerConfiguration, type LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import {
  ChannelThreadCreatedTriggerConfigurationSchema, ChannelThreadCreatedTriggerDefinition, ConnectorID, ThreadReplyCreatedTriggerConfigurationSchema, ThreadReplyCreatedTriggerDefinition,
  newProjectConnection, newProjectMessageTriggerRunner, relayedTriggerPath, type SlackClientOptions,
} from '@superdurable/connector-slack';
import {
  ConnectionName, ReplyTriggerBinding, StartTriggerBinding, ThreadApprovalFlow, mapToFlowInput, mapToReceiveThreadReplyInput, newReplyTriggerFilter, newStartTriggerFilter, resolveFlowId,
} from './flow/workflow.js';

/** Settings of one thread-approval application process. */
export interface ThreadApprovalApplicationOptions {
  /** Dex gRPC address. */ serverAddress: string;
  /** Host and port of the relayed Trigger HTTP server; port 0 picks a free port. */ triggerHost: string; triggerPort: number;
  /** Destination of every Trigger record. */ logger: TriggerLogger;
  /** Slack client options for tests, such as a fake Socket Mode dialer. */ slackOptions?: SlackClientOptions;
  /** Called once the Worker is registered and the relayed route listens. */ onReady?(ready: { client: Client; flow: ThreadApprovalFlow; relayedTriggerURL: string }): void;
}

/**
 * Run the Worker, the shared Slack message Trigger runner, and the relayed Trigger route until signal
 * aborts or the runner fails. A host that relays Slack events for a connection without an app_token
 * posts them to relayedTriggerPath(ConnectionName).
 */
export async function runThreadApprovalApplication(project: LoadedProject, options: ThreadApprovalApplicationOptions, signal: AbortSignal): Promise<void> {
  const connection = newProjectConnection(project, ConnectionName, options.slackOptions);
  const flow = new ThreadApprovalFlow(connection);
  const startConfiguration = decodeTriggerConfiguration(project.configuration, { connectorId: ConnectorID, connectionName: ConnectionName, triggerName: ChannelThreadCreatedTriggerDefinition.trigger.triggerName, bindingName: StartTriggerBinding }, ChannelThreadCreatedTriggerConfigurationSchema);
  const replyConfiguration = decodeTriggerConfiguration(project.configuration, { connectorId: ConnectorID, connectionName: ConnectionName, triggerName: ThreadReplyCreatedTriggerDefinition.trigger.triggerName, bindingName: ReplyTriggerBinding }, ThreadReplyCreatedTriggerConfigurationSchema);
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-slack-thread-approval-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(registry, cache, { serverAddress: options.serverAddress });
  const client = new Client(registry, cache, { serverAddress: options.serverAddress });
  const bindingLogger = (trigger: string, binding: string) => withTriggerLoggerAttributes(options.logger, { connector: ConnectorID, connection: ConnectionName, trigger, binding });
  // The runner, its durable inboxes, and the Dex targets log every skipped event and every retry.
  const runner = newProjectMessageTriggerRunner(project, ConnectionName, {
    channelThreadCreatedRoutes: [{ bindingName: StartTriggerBinding, target: newDexFlowTriggerTarget(client, flow, newStartTriggerFilter(startConfiguration), resolveFlowId, mapToFlowInput,
      { logger: bindingLogger(ChannelThreadCreatedTriggerDefinition.trigger.triggerName, StartTriggerBinding) }) }],
    threadReplyCreatedRoutes: [{ bindingName: ReplyTriggerBinding, target: newDexRPCTriggerTarget(client, flow.receiveThreadReply, newReplyTriggerFilter(replyConfiguration), resolveFlowId, mapToReceiveThreadReplyInput,
      { logger: bindingLogger(ThreadReplyCreatedTriggerDefinition.trigger.triggerName, ReplyTriggerBinding) }) }],
  }, { ...options.slackOptions, logger: options.logger });
  const relayedPath = relayedTriggerPath(ConnectionName);
  const server = createServer((request, response) => {
    if (request.url?.split('?')[0] !== relayedPath) { request.resume(); response.writeHead(404).end(); return; }
    void runner.serveHTTP(request, response);
  });
  const stop = new AbortController();
  const onAbort = () => stop.abort();
  signal.addEventListener('abort', onAbort, { once: true });
  try {
    // The route answers 503 until the runner receives relayed events.
    server.listen(options.triggerPort, options.triggerHost);
    await once(server, 'listening');
    const address = server.address();
    if (!address || typeof address === 'string') throw new Error('relayed Trigger server address is unavailable');
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000, signal: stop.signal });
    options.onReady?.({ client, flow, relayedTriggerURL: `http://${options.triggerHost}:${address.port}${relayedPath}` });
    try { await runner.run(newTriggerContext(stop.signal)); }
    catch (error) { if (!(error instanceof TriggerDeliveryAbortedError && signal.aborted)) throw error; }
  } finally {
    signal.removeEventListener('abort', onAbort);
    await new Promise<void>(resolve => server.close(() => resolve()));
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}
