// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { once } from 'node:events';
import { createServer } from 'node:http';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { TriggerDeliveryAbortedError, newDexFlowTriggerTarget, newTriggerContext, waitForTriggerDelay, withTriggerLoggerAttributes, type TriggerLogger } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConnectorID, IssueEventReceivedTriggerDefinition, newProjectIssueEventReceivedEndpointRunner, type LinearClientOptions } from '@superdurable/connector-linear';
import { ConnectionName, IssueCreatedBinding, LinearIssueEventIntakeFlow, acceptNewIssue, mapToFlowInput, resolveFlowId } from './flow.js';

/** Mounted callback path configured in Linear. */ export const WebhookPath = '/webhooks/linear';
/** Readiness path for the webhook binding. */ export const ReadinessPath = '/readyz';
/** Process settings for the Linear issue intake. */
export interface IssueIntakeOptions {
  /** Dex gRPC address. */ serverAddress: string;
  /** Local webhook listen host. */ webhookHost: string;
  /** Local webhook listen port; zero chooses an unused port. */ webhookPort: number;
  /** Secret-safe Trigger logging destination. */ logger: TriggerLogger;
  /** Optional provider clock and sender overrides for tests. */ linearOptions?: LinearClientOptions;
  /** Called once the HTTP listener is available. */ onListening?(url: string): void;
  /** Called once the Worker is registered. */ onReady?(ready: { client: Client; flow: LinearIssueEventIntakeFlow }): void;
}
/** Serve signed deliveries immediately and start a Worker when Dex is reachable. */
export async function runLinearIssueIntake(project: LoadedProject, options: IssueIntakeOptions, signal: AbortSignal): Promise<void> {
  const flow = new LinearIssueEventIntakeFlow();
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-linear-issue-intake-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const client = new Client(registry, cache, { serverAddress: options.serverAddress });
  const logger = withTriggerLoggerAttributes(options.logger, { connector: ConnectorID, connection: ConnectionName, trigger: IssueEventReceivedTriggerDefinition.trigger.triggerName, binding: IssueCreatedBinding });
  const runner = newProjectIssueEventReceivedEndpointRunner(project, ConnectionName, [{ bindingName: IssueCreatedBinding,
    target: newDexFlowTriggerTarget(client, flow, acceptNewIssue, resolveFlowId, mapToFlowInput, { logger }),
  }], { ...options.linearOptions, logger: options.logger });
  const server = createServer((request, response) => {
    const path = request.url?.split('?')[0];
    if (path === WebhookPath) { void runner.serveHTTP(request, response); return; }
    request.resume();
    if (path !== ReadinessPath) { response.writeHead(404).end(); return; }
    const ready = runner.runningSourceCount() > 0;
    response.writeHead(ready ? 200 : 503).end(ready ? 'ready\n' : 'binding unavailable\n');
  });
  const stop = new AbortController();
  const onAbort = () => stop.abort();
  signal.addEventListener('abort', onAbort, { once: true });
  const running = runner.run(newTriggerContext(stop.signal));
  running.catch(() => stop.abort());
  let worker: Worker | undefined;
  try {
    server.listen(options.webhookPort, options.webhookHost);
    await once(server, 'listening');
    const address = server.address();
    if (!address || typeof address === 'string') throw new Error('webhook listener address is unavailable');
    options.onListening?.(`http://${options.webhookHost}:${address.port}${WebhookPath}`);
    worker = await startWorker(registry, cache, options, stop.signal);
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
async function startWorker(registry: Registry, cache: ReturnType<typeof openBlobCache>, options: IssueIntakeOptions, signal: AbortSignal): Promise<Worker> {
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
