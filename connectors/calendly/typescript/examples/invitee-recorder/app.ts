// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { TriggerDeliveryAbortedError, newDexFlowTriggerTarget, newTriggerContext, waitForTriggerDelay, withTriggerLoggerAttributes, type TriggerLogger } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConnectorID, InviteeEventReceivedTriggerDefinition, newProjectConnection, newProjectInviteeEventReceivedEndpointRunner, type CalendlyClientOptions } from '@superdurable/connector-calendly';
import { ConnectionName, InviteeCreatedTriggerBinding, InviteeRecorderFlow, acceptBooking, mapToFlowInput, resolveFlowId } from './flow/workflow.js';

/** The subscription's callback path; expose it through an HTTPS tunnel in local development. */
export const WebhookPath = '/webhooks/calendly';
/** Answers 200 once the binding receives deliveries. */
export const ReadinessPath = '/readyz';

/** Settings of one invitee-recorder application process. */
export interface InviteeRecorderOptions {
  /** Dex gRPC address. */ serverAddress: string;
  /** Host and port of the webhook HTTP server; port 0 picks a free port. */ webhookHost: string; webhookPort: number;
  /** Destination of every Trigger record. */ logger: TriggerLogger;
  /** Calendly client options for tests, such as a stand-in API sender or clock. */ calendlyOptions?: CalendlyClientOptions;
  /** Called once the webhook listens, with its URL; the Worker may still be waiting for Dex. */ onListening?(webhookURL: string): void;
  /** Called once the Worker is registered. */ onReady?(ready: { client: Client; flow: InviteeRecorderFlow }): void;
}

/**
 * Serve the webhook endpoint at once, so a delivery is recorded in the binding's durable project inbox
 * even while Dex is unreachable, then run the Worker until signal aborts or the endpoint runner fails.
 */
export async function runInviteeRecorder(project: LoadedProject, options: InviteeRecorderOptions, signal: AbortSignal): Promise<void> {
  const calendlyOptions = { ...options.calendlyOptions, logger: options.logger };
  const flow = new InviteeRecorderFlow(newProjectConnection(project, ConnectionName, calendlyOptions));
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-calendly-invitee-recorder-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  let worker: Worker | undefined;
  const client = new Client(registry, cache, { serverAddress: options.serverAddress });
  const bindingLogger = withTriggerLoggerAttributes(options.logger, { connector: ConnectorID, connection: ConnectionName, trigger: InviteeEventReceivedTriggerDefinition.trigger.triggerName, binding: InviteeCreatedTriggerBinding });
  // The event ID is the Flow-start request ID, so a redelivered webhook starts no second Flow.
  const runner = newProjectInviteeEventReceivedEndpointRunner(project, ConnectionName, [{
    bindingName: InviteeCreatedTriggerBinding, target: newDexFlowTriggerTarget(client, flow, acceptBooking, resolveFlowId, mapToFlowInput, { logger: bindingLogger }),
  }], calendlyOptions);
  const server = createServer((request, response) => {
    const path = request.url?.split('?')[0];
    if (path === WebhookPath) { void runner.serveHTTP(request, response); return; }
    request.resume();
    if (path !== ReadinessPath) { response.writeHead(404).end(); return; }
    const isReady = runner.runningSourceCount() > 0;
    response.writeHead(isReady ? 200 : 503, { 'Content-Type': 'text/plain; charset=utf-8' }).end(isReady ? 'ready\n' : 'Calendly binding is not receiving yet\n');
  });
  const stop = new AbortController();
  const onAbort = () => stop.abort();
  signal.addEventListener('abort', onAbort, { once: true });
  const running = runner.run(newTriggerContext(stop.signal));
  // A runner failure stops the Worker wait below.
  running.catch(() => stop.abort());
  try {
    server.listen(options.webhookPort, options.webhookHost);
    await once(server, 'listening');
    const address = server.address();
    if (!address || typeof address === 'string') throw new Error('webhook server address is unavailable');
    options.onListening?.(`http://${options.webhookHost}:${address.port}${WebhookPath}`);
    worker = await startWorkerWhenDexIsReachable(registry, cache, options, stop.signal);
    await worker.waitPullRegistered({ timeoutMs: 30_000, signal: stop.signal });
    options.onReady?.({ client, flow });
    try { await running; }
    catch (error) { if (!(error instanceof TriggerDeliveryAbortedError && signal.aborted)) throw error; }
  } finally {
    signal.removeEventListener('abort', onAbort);
    stop.abort();
    await running.catch(() => undefined);
    await new Promise<void>(resolve => server.close(() => resolve()));
    await client.close(); await worker?.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}

/** Start a Worker, retrying with delays from 250 milliseconds to 30 seconds while Dex is unreachable. */
async function startWorkerWhenDexIsReachable(registry: Registry, cache: ReturnType<typeof openBlobCache>, options: InviteeRecorderOptions, signal: AbortSignal): Promise<Worker> {
  for (let attempt = 1, delayMs = 250; ; attempt++, delayMs = Math.min(2 * delayMs, 30_000)) {
    const worker = new Worker(registry, cache, { serverAddress: options.serverAddress });
    try { await worker.start(); return worker; }
    catch (error) {
      await worker.close();
      if (signal.aborted) throw new TriggerDeliveryAbortedError();
      options.logger.log('warn', 'dex server unavailable; retrying', { attempt, delay_ms: delayMs, error_type: error instanceof Error ? error.name : 'unknown' });
    }
    await waitForTriggerDelay(signal, delayMs);
  }
}
