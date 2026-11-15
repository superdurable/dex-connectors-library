// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { TriggerDeliveryAbortedError, newDexRPCTriggerTarget, newTriggerContext, withTriggerLoggerAttributes, type TriggerLogger } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConnectorID, EnvelopeEventReceivedTriggerDefinition, newProjectConnection, newProjectEnvelopeEventReceivedEndpointRunner, type DocuSignClientOptions } from '@superdurable/connector-docusign';
import { ConnectionName, EnvelopeOutcomeFlow, EnvelopeOutcomeTriggerBinding, acceptEnvelopeEvent, mapToEnvelopeEventInput, resolveFlowId } from './flow/workflow.js';

/** The Connect configuration's URL to Publish path; expose it through HTTPS. */
export const ConnectPath = '/docusign/connect';
/** Answers 200 once the binding receives deliveries. */
export const ReadinessPath = '/readyz';

/** Settings of one envelope-outcome application process. */
export interface EnvelopeOutcomeApplicationOptions {
  /** Dex gRPC address. */ serverAddress: string;
  /** Host and port of the Connect HTTP server; port 0 picks a free port. */ connectHost: string; connectPort: number;
  /** Destination of every Trigger record. */ logger: TriggerLogger;
  /** DocuSign client options for tests, such as a local transport. */ docuSignOptions?: DocuSignClientOptions;
  /** Called once the Worker is registered and the Connect endpoint listens. */ onReady?(ready: { client: Client; flow: EnvelopeOutcomeFlow; connectURL: string }): void;
}

/**
 * Serve the Connect endpoint at once, so deliveries are recorded in the durable inbox even while Dex is
 * unreachable, then run the Worker and the endpoint runner until signal aborts or the runner fails.
 */
export async function runEnvelopeOutcomeApplication(project: LoadedProject, options: EnvelopeOutcomeApplicationOptions, signal: AbortSignal): Promise<void> {
  const docuSignOptions: DocuSignClientOptions = { ...options.docuSignOptions, logger: options.logger };
  const flow = new EnvelopeOutcomeFlow(newProjectConnection(project, ConnectionName, docuSignOptions));
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-docusign-envelope-outcome-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(registry, cache, { serverAddress: options.serverAddress });
  const client = new Client(registry, cache, { serverAddress: options.serverAddress });
  const bindingLogger = withTriggerLoggerAttributes(options.logger, { connector: ConnectorID, connection: ConnectionName, trigger: EnvelopeEventReceivedTriggerDefinition.trigger.triggerName, binding: EnvelopeOutcomeTriggerBinding });
  const runner = newProjectEnvelopeEventReceivedEndpointRunner(project, ConnectionName, [{
    bindingName: EnvelopeOutcomeTriggerBinding,
    target: newDexRPCTriggerTarget(client, flow.receiveEnvelopeEvent, acceptEnvelopeEvent, resolveFlowId, mapToEnvelopeEventInput, { logger: bindingLogger, rpcName: 'receiveEnvelopeEvent' }),
  }], docuSignOptions);
  const server = createServer((request, response) => {
    const path = request.url?.split('?')[0];
    if (path === ConnectPath) { void runner.serveHTTP(request, response); return; }
    request.resume();
    if (path === ReadinessPath) { response.writeHead(runner.runningSourceCount() === 0 ? 503 : 200).end(); return; }
    response.writeHead(404).end();
  });
  const stop = new AbortController();
  const onAbort = () => stop.abort();
  signal.addEventListener('abort', onAbort, { once: true });
  try {
    server.listen(options.connectPort, options.connectHost);
    await once(server, 'listening');
    const address = server.address();
    if (!address || typeof address === 'string') throw new Error('Connect server address is unavailable');
    const running = runner.run(newTriggerContext(stop.signal));
    // A runner failure stops the Worker wait below instead of leaving it pending.
    running.catch(() => stop.abort());
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000, signal: stop.signal });
    options.onReady?.({ client, flow, connectURL: `http://${options.connectHost}:${address.port}${ConnectPath}` });
    try { await running; }
    catch (error) { if (!(error instanceof TriggerDeliveryAbortedError && signal.aborted)) throw error; }
  } finally {
    signal.removeEventListener('abort', onAbort);
    stop.abort();
    server.closeAllConnections();
    await new Promise<void>(resolve => server.close(() => resolve()));
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}
