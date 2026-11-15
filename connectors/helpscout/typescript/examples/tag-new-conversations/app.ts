// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { TriggerDeliveryAbortedError, newDexFlowTriggerTarget, newTriggerContext, withTriggerLoggerAttributes, type TriggerLogger } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConnectorID, ConversationEventTriggerDefinition, newProjectConnection, newProjectConversationEventEndpointRunner, type HelpScoutClientOptions } from '@superdurable/connector-helpscout';
import { ConnectionName, NewConversationTriggerBinding, TagNewConversationFlow, acceptNewConversation, mapToFlowInput, resolveFlowId } from './flow/workflow.js';

/** The Help Scout webhook's callback path; expose it through an HTTPS tunnel in local development. */
export const WebhookPath = '/webhooks/helpscout';
/** Answers 200 once the binding receives deliveries. */
export const ReadinessPath = '/readyz';

/** Settings of one tagging application process. */
export interface TagNewConversationsApplicationOptions {
  /** Dex gRPC address. */ serverAddress: string;
  /** Host and port of the webhook HTTP server; port 0 picks a free port. */ webhookHost: string; webhookPort: number;
  /** Destination of every Trigger record. */ logger: TriggerLogger;
  /** Help Scout client options for tests, such as a local provider stand-in. */ helpScoutOptions?: HelpScoutClientOptions;
  /** Called once the webhook listens and the Worker is registered. */ onReady?(ready: { client: Client; webhookURL: string; readinessURL: string }): void;
}

/**
 * Serve the webhook endpoint at once, so deliveries are recorded in the binding's durable project inbox
 * even while Dex is unreachable, then run the Worker until signal aborts or the endpoint runner fails.
 */
export async function runTagNewConversationsApplication(project: LoadedProject, options: TagNewConversationsApplicationOptions, signal: AbortSignal): Promise<void> {
  const connection = newProjectConnection(project, ConnectionName, options.helpScoutOptions);
  const flow = new TagNewConversationFlow(connection);
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-helpscout-tag-new-conversations-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(registry, cache, { serverAddress: options.serverAddress });
  const client = new Client(registry, cache, { serverAddress: options.serverAddress });
  const bindingLogger = withTriggerLoggerAttributes(options.logger, {
    connector: ConnectorID, connection: ConnectionName, trigger: ConversationEventTriggerDefinition.trigger.triggerName, binding: NewConversationTriggerBinding,
  });
  const runner = newProjectConversationEventEndpointRunner(project, ConnectionName, [{
    bindingName: NewConversationTriggerBinding, target: newDexFlowTriggerTarget(client, flow, acceptNewConversation, resolveFlowId, mapToFlowInput, { logger: bindingLogger }),
  }], { ...options.helpScoutOptions, logger: options.logger });
  const server = createServer((request, response) => {
    const path = request.url?.split('?')[0];
    if (path === WebhookPath) { void runner.serveHTTP(request, response); return; }
    request.resume();
    if (path !== ReadinessPath) { response.writeHead(404).end(); return; }
    if (runner.runningSourceCount() === 0) { response.writeHead(503, { 'Content-Type': 'text/plain; charset=utf-8' }).end('Help Scout binding is not receiving yet\n'); return; }
    response.writeHead(200, { 'Content-Type': 'text/plain; charset=utf-8' }).end('ready\n');
  });
  const stop = new AbortController();
  const onAbort = () => stop.abort();
  signal.addEventListener('abort', onAbort, { once: true });
  try {
    server.listen(options.webhookPort, options.webhookHost);
    await once(server, 'listening');
    const address = server.address();
    if (!address || typeof address === 'string') throw new Error('webhook server address is unavailable');
    const running = runner.run(newTriggerContext(stop.signal));
    // A failed runner stops the process; the Worker still starts so recorded events reach Dex.
    running.catch(() => stop.abort());
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000, signal: stop.signal });
    const origin = `http://${options.webhookHost}:${address.port}`;
    options.onReady?.({ client, webhookURL: origin + WebhookPath, readinessURL: origin + ReadinessPath });
    try { await running; }
    catch (error) { if (!(error instanceof TriggerDeliveryAbortedError && signal.aborted)) throw error; }
  } finally {
    stop.abort();
    signal.removeEventListener('abort', onAbort);
    server.closeAllConnections();
    await new Promise<void>(resolve => server.close(() => resolve()));
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}
