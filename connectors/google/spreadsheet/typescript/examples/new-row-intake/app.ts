// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { TriggerDeliveryAbortedError, newDexFlowTriggerTarget, newTriggerContext, withTriggerLoggerAttributes, type TriggerLogger, type TriggerTarget } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ConnectorID, RowAddedTriggerDefinition, newProjectConnection, newProjectRowAddedTriggerRunner, type RowAddedEvent, type SpreadsheetClientOptions } from '@superdurable/connector-google-sheets';
import { ConnectionName, NewRowIntakeFlow, NewRowTriggerBinding, acceptAddedRow, mapToFlowInput, resolveFlowId } from './flow/workflow.js';

/** Settings of one new-row-intake application process. */
export interface NewRowIntakeOptions {
  /** Dex gRPC address. */ serverAddress: string;
  /** Destination of every Trigger record. */ logger: TriggerLogger;
  /** Google Sheets client options for tests, such as a clock. */ spreadsheetOptions?: SpreadsheetClientOptions;
}

/** Start one Flow per added row, with the Trigger event ID as the Flow ID suffix and start request ID. */
export function newAddedRowTarget(client: Client, flow: NewRowIntakeFlow, logger: TriggerLogger): TriggerTarget<RowAddedEvent> {
  const bindingLogger = withTriggerLoggerAttributes(logger, { connector: ConnectorID, connection: ConnectionName, trigger: RowAddedTriggerDefinition.trigger.triggerName, binding: NewRowTriggerBinding });
  return newDexFlowTriggerTarget(client, flow, acceptAddedRow, resolveFlowId, mapToFlowInput, { logger: bindingLogger });
}

/**
 * Run the Worker and the durable rowAdded runner until signal aborts or the runner fails. The runner seeds
 * the binding's cursor, records each row in the binding's project inbox, and starts one Flow per row, which
 * reads the row back; it logs every failed poll and delivery.
 */
export async function runNewRowIntake(project: LoadedProject, options: NewRowIntakeOptions, signal: AbortSignal): Promise<void> {
  // The Flow reads each row back through the same stored connection the runner polls with.
  const flow = new NewRowIntakeFlow(newProjectConnection(project, ConnectionName, options.spreadsheetOptions));
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-google-sheets-new-row-intake-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const client = new Client(registry, cache, { serverAddress: options.serverAddress });
  const worker = new Worker(registry, cache, { serverAddress: options.serverAddress });
  const runner = newProjectRowAddedTriggerRunner(project, ConnectionName, [{ bindingName: NewRowTriggerBinding, target: newAddedRowTarget(client, flow, options.logger) }], { ...options.spreadsheetOptions, triggerLogger: options.logger });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000, signal });
    try { await runner.run(newTriggerContext(signal)); }
    catch (error) { if (!(error instanceof TriggerDeliveryAbortedError && signal.aborted)) throw error; }
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}
