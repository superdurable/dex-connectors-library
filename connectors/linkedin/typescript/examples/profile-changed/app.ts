// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { TriggerDeliveryAbortedError, newDexFlowTriggerTarget, newTriggerContext, type TriggerEvent, type TriggerLogger, type TriggerTarget } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import type { Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { newProjectConnection, newProjectProfileTriggerRunner, type LinkedInClientOptions, type ProfileChangedEvent } from '@superdurable/connector-linkedin';
import { ConnectionName, ProfileChangeFlow, ProfileChangedBinding, acceptEveryProfileChange, mapToFlowInput, resolveFlowId } from './flow.js';

/** Worker and provider-test options. */
export interface ProfileChangeWorkerOptions {
  /** Dex gRPC address. */ serverAddress: string;
  /** Metadata logger. */ logger?: TriggerLogger;
  /** Provider test transport and clock. */ linkedinOptions?: LinkedInClientOptions;
  /** Called after Worker registration and Trigger start. */ onReady?(ready: { client: Client; runner: Runner }): void;
  /** Called after a change was routed into Dex. */ onFlowStarted?(event: TriggerEvent<ProfileChangedEvent>): void;
}

/** Wrap Dex Flow start with a delivery notification. */
export function newProfileChangeFlowTarget(client: Client, flow: ProfileChangeFlow, options: Pick<ProfileChangeWorkerOptions, 'logger' | 'onFlowStarted'> = {}): TriggerTarget<ProfileChangedEvent> {
  const target = newDexFlowTriggerTarget(client, flow, acceptEveryProfileChange, resolveFlowId, mapToFlowInput, options.logger ? { logger: options.logger } : {});
  return { handleTrigger: async (context, event) => { await target.handleTrigger(context, event); options.onFlowStarted?.(event); } };
}

/** Serve the Flow and durable profileChanged Trigger until the signal aborts. */
export async function runProfileChangeWorker(project: LoadedProject, options: ProfileChangeWorkerOptions, signal: AbortSignal): Promise<void> {
  const connection = newProjectConnection(project, ConnectionName, options.linkedinOptions);
  const flow = new ProfileChangeFlow(connection.reference.name);
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-linkedin-profile-changed-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(registry, cache, { serverAddress: options.serverAddress });
  const client = new Client(registry, cache, { serverAddress: options.serverAddress });
  try {
    const target = newProfileChangeFlowTarget(client, flow, options);
    const runner = newProjectProfileTriggerRunner(project, connection, [{ bindingName: ProfileChangedBinding, target }], options.logger ? { logger: options.logger } : {});
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000, signal });
    const running = runner.run(newTriggerContext(signal));
    options.onReady?.({ client, runner });
    try { await running; }
    catch (error) { if (!(error instanceof TriggerDeliveryAbortedError && signal.aborted)) throw error; }
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}
