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
import { ConnectorID, TaskEventTriggerDefinition, newProjectTaskEventEndpointRunner, type ClickUpClientOptions } from '@superdurable/connector-clickup';
import { BindingName, ConnectionName, TaskEventFlow, acceptStatusChange, mapToFlowInput, resolveFlowId } from './flow/workflow.js';

/** Public callback path registered with ClickUp. */ export const WebhookPath = '/webhooks/clickup';
/** Local readiness path. */ export const ReadinessPath = '/readyz';

/** Settings for one task event Worker and HTTP endpoint. */
export interface TaskEventAppOptions {
  /** Dex gRPC address. */ serverAddress: string;
  /** Bind host. */ webhookHost: string;
  /** Bind port; zero chooses a free port. */ webhookPort: number;
  /** Trigger record sink. */ logger: TriggerLogger;
  /** ClickUp client test dependencies. */ clickupOptions?: ClickUpClientOptions;
  /** Called after HTTP starts listening. */ onListening?(url: string): void;
  /** Called after the Dex Worker registers. */ onReady?(ready: { client: Client; flow: TaskEventFlow }): void;
}

/** Serve signed task webhooks with a durable inbox and a real Dex Worker. */
export async function runTaskEventApp(project: LoadedProject, options: TaskEventAppOptions, signal: AbortSignal): Promise<void> {
  const flow = new TaskEventFlow();
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-clickup-task-event-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const client = new Client(registry, cache, { serverAddress: options.serverAddress });
  const bindingLogger = withTriggerLoggerAttributes(options.logger, { connector: ConnectorID, connection: ConnectionName, trigger: TaskEventTriggerDefinition.trigger.triggerName, binding: BindingName });
  const runner = newProjectTaskEventEndpointRunner(project, ConnectionName, [{ bindingName: BindingName,
    target: newDexFlowTriggerTarget(client, flow, acceptStatusChange, resolveFlowId, mapToFlowInput, { logger: bindingLogger }),
  }], { ...options.clickupOptions, logger: options.logger });
  const server = createServer((request, response) => {
    const path = request.url?.split('?')[0];
    if (path === WebhookPath) { void runner.serveHTTP(request, response); return; }
    request.resume();
    if (path !== ReadinessPath) { response.writeHead(404).end(); return; }
    const isReady = runner.runningSourceCount() > 0;
    response.writeHead(isReady ? 200 : 503, { 'Content-Type': 'text/plain; charset=utf-8' }).end(isReady ? 'ready\n' : 'ClickUp binding is not receiving yet\n');
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
    if (!address || typeof address === 'string') throw new Error('webhook server address is unavailable');
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

async function startWorker(registry: Registry, cache: ReturnType<typeof openBlobCache>, options: TaskEventAppOptions, signal: AbortSignal): Promise<Worker> {
  for (let attempt = 1, delayMs = 250; ; attempt++, delayMs = Math.min(delayMs * 2, 30_000)) {
    const worker = new Worker(registry, cache, { serverAddress: options.serverAddress });
    try { await worker.start(); return worker; }
    catch (error) {
      await worker.close();
      if (signal.aborted) throw new TriggerDeliveryAbortedError();
      options.logger.log('warn', 'Dex server unavailable; retrying', { attempt, delay_ms: delayMs, error_type: error instanceof Error ? error.name : 'unknown' });
    }
    await waitForTriggerDelay(signal, delayMs);
  }
}
