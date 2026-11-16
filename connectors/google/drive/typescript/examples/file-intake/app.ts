// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { TriggerDeliveryAbortedError, newDexFlowTriggerTarget, newTriggerContext, type TriggerLogger, type TriggerTarget } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection, newProjectFileTriggerRunner, type DriveClientOptions, type FileEvent, type FileTriggerRunner } from '@superdurable/connector-google-drive';
import { ConnectionName, FileIntakeFlow, NewFileTriggerBinding, acceptCreatedFile, mapToFlowInput, resolveFlowId } from './flow/workflow.js';

/** Start one file intake Flow per created file; the event ID is the Flow-start request ID. */
export function newFileIntakeTarget(client: Client, flow: FileIntakeFlow, logger?: TriggerLogger): TriggerTarget<FileEvent> {
  return newDexFlowTriggerTarget(client, flow, acceptCreatedFile, resolveFlowId, mapToFlowInput, logger ? { logger } : {});
}

/** Build the durable runner of the saved fileCreated binding. */
export function newFileIntakeRunner(project: LoadedProject, target: TriggerTarget<FileEvent>, options: DriveClientOptions = {}): FileTriggerRunner {
  return newProjectFileTriggerRunner(project, ConnectionName, { fileCreatedRoutes: [{ bindingName: NewFileTriggerBinding, target }] }, options);
}

/** Settings of one file intake process. */
export interface FileIntakeOptions {
  /** Dex gRPC address. */ serverAddress: string;
  /** Destination of every Trigger record. */ logger: TriggerLogger;
}

/** Run the Worker and the durable Trigger runner until signal aborts or either fails. */
export async function runFileIntake(project: LoadedProject, options: FileIntakeOptions, signal: AbortSignal): Promise<void> {
  const flow = new FileIntakeFlow(newProjectConnection(project, ConnectionName, { triggerLogger: options.logger }));
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-google-drive-file-intake-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const client = new Client(registry, cache, { serverAddress: options.serverAddress });
  const worker = new Worker(registry, cache, { serverAddress: options.serverAddress });
  const runner = newFileIntakeRunner(project, newFileIntakeTarget(client, flow, options.logger), { triggerLogger: options.logger });
  const stop = new AbortController();
  const onAbort = () => stop.abort();
  signal.addEventListener('abort', onAbort, { once: true });
  try {
    await worker.start();
    try { await runner.run(newTriggerContext(stop.signal)); }
    catch (error) { if (!(error instanceof TriggerDeliveryAbortedError && signal.aborted)) throw error; }
  } finally {
    signal.removeEventListener('abort', onAbort);
    stop.abort();
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}
