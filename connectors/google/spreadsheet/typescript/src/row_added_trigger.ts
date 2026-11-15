// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newTrigger, type TriggerBindingRef, type TriggerContext, type TriggerRunner, type TriggerTarget } from '@superdurable/connectors-sdk';
import { newBindingPoller, newBindingTriggerSource, newMemoryCursorStore, newRunner, type CursorStore, type PollSchedule, type RequestLimiter, type Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { decodeTriggerConfiguration, newDurableTriggerTarget, parseTriggerPayloadJSON, type LoadedProject, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { SpreadsheetClient, type SpreadsheetClientOptions } from './client.js';
import type { Connection } from './generated/connection.js';
import { ConnectorID } from './generated/definitions.js';
import { newProjectConnection } from './project.js';
import { RowAddedFetcher } from './row_added_fetcher.js';
import { RowAddedEventSchema, RowAddedTriggerConfigurationSchema, RowAddedTriggerDefinition, goTrimSpace, validateRowAddedTriggerConfiguration, type RowAddedEvent, type RowAddedTriggerConfiguration } from './row_added_models.js';

const maxBackoffMs = 5 * 60 * 1000;

/** Return the schedule every rowAdded binding of the client's connection shares: pollIntervalMs, backing off up to five minutes. */
export function rowAddedPollSchedule(client: SpreadsheetClient): PollSchedule {
  return { intervalMs: client.pollIntervalMs, maxBackoffMs: Math.max(maxBackoffMs, client.pollIntervalMs) };
}

/** Configuration of one rowAdded Trigger whose cursor lives in process memory. */
export interface RowAddedTriggerConfig {
  /** Google Sheets connection from newProjectConnection or newConnection with a SpreadsheetClient. */ connection: Connection;
  /** Static connection name; must equal the connection's name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** The polled spreadsheet and tab. */ configuration: RowAddedTriggerConfiguration;
  /** Routes each added row into the application. */ target: TriggerTarget<RowAddedEvent>;
}

/**
 * Create one rowAdded Trigger whose cursor lives in process memory: a restart seeds again, so rows added while
 * the process was stopped are not delivered. Use it only in tests or applications without project storage,
 * and newProjectRowAddedTriggerRunner otherwise. Throws for a blank spreadsheet ID or tab title, a connection
 * name that differs from the connection, or a mock connection.
 */
export function newRowAddedTrigger(config: RowAddedTriggerConfig): TriggerRunner {
  if (config.connectionName !== config.connection?.reference.name) throw new TypeError(`Google Sheets rowAdded connectionName "${config.connectionName}" must equal its connection's name`);
  validateRowAddedTriggerConfiguration(config.configuration);
  const client = spreadsheetClientOf(config.connection);
  const definition = RowAddedTriggerDefinition;
  const binding: TriggerBindingRef = { connection: config.connection.reference, trigger: definition.trigger, name: config.bindingName };
  const fetcher = new RowAddedFetcher(client, binding, config.configuration, client.triggerLogger);
  const source = newBindingTriggerSource<RowAddedEvent>({ definition, binding, cursor: newMemoryCursorStore(), fetcher }, rowAddedPollSchedule(client), client.triggerLogger);
  return newTrigger({ definition, binding, source, target: config.target, ...(client.triggerLogger ? { logger: client.triggerLogger } : {}) });
}

/** Binds one rowAdded binding's configuration and cursor to its application target. */
export interface RowAddedTriggerRoute {
  /** The configured Trigger binding name, unique within one runner. */ bindingName: string;
  /** The polled spreadsheet and tab. */ configuration: RowAddedTriggerConfiguration;
  /**
   * Persists the last processed row, normally LoadedProject.pollCursorStore. Use newMemoryCursorStore only when
   * rows added while the process is stopped may be skipped.
   */
  cursor: CursorStore;
  /**
   * Receives each added row. A target wrapped by newDurableTriggerTarget records the row before the cursor
   * moves past it; any other target consumes it first.
   */
  target: TriggerTarget<RowAddedEvent>;
}

/** Configures every rowAdded binding of one Google Sheets connection. */
export interface RowAddedTriggerRunnerConfig {
  /** The connection whose pollInterval and pollPageSize settings and triggerLogger apply. */ connection: Connection;
  /** Polled in this order whenever several are due; at least one is required. */ routes: RowAddedTriggerRoute[];
  /** Optionally paces every poll request, for example to share a per-user Sheets quota. */ requestLimiter?: RequestLimiter;
}

/**
 * Polls each rowAdded binding's tab of one connection one poll at a time. Each binding seeds its cursor at the
 * tab's last row on its first poll, so existing rows never start Flows. Later polls read the header and up to
 * pollPageSize rows after the last processed row with one values:batchGet request and deliver each non-empty
 * row, in row order, before the cursor commits. The cursor stores the last processed row number and a SHA-256
 * of its cells: when rows above it are deleted or inserted, the runner finds that row again; when it was
 * edited in place, the runner keeps its position; when the tab now ends above it, the runner reseeds and logs
 * the gap. Delivery is at least once, and each event ID is a pure function of the tab, row number, and cells.
 */
export interface RowAddedTriggerRunner {
  /**
   * Replay pending rows, seed missing cursors, and poll every binding until context.signal aborts. A failed
   * poll backs off up to five minutes, and HTTP 429 pauses every binding of the connection for at least its
   * Retry-After. Rejects with TriggerDeliveryAbortedError after the signal aborts, or with an error caused by
   * PollCursorDocumentInvalidError for a stored cursor document that can never be decoded.
   */
  run(context: TriggerContext): Promise<void>;
  /** Make one binding due now; unknown names are ignored. */
  wakeBinding(bindingName: string): void;
}

/** Validate every route and build one poll runner for the connection; it makes no request and reads no storage. */
export function newRowAddedTriggerRunner(config: RowAddedTriggerRunnerConfig): RowAddedTriggerRunner {
  const client = spreadsheetClientOf(config?.connection);
  if (!config.routes?.length) throw new TypeError('Google Sheets rowAdded Trigger routes are required');
  const definition = RowAddedTriggerDefinition;
  const pollers = config.routes.map(route => {
    if (typeof route.bindingName !== 'string' || !goTrimSpace(route.bindingName) || !route.target || !route.cursor) throw new TypeError('Google Sheets rowAdded binding name, cursor, and target are required');
    const binding: TriggerBindingRef = { connection: config.connection.reference, trigger: definition.trigger, name: route.bindingName };
    try {
      return newBindingPoller<RowAddedEvent>({ definition, binding, cursor: route.cursor, fetcher: new RowAddedFetcher(client, binding, route.configuration, client.triggerLogger), target: route.target });
    } catch (error) { throw new TypeError(`Google Sheets rowAdded binding "${route.bindingName}": ${error instanceof Error ? error.message : String(error)}`, { cause: error }); }
  });
  const runner: Runner = newRunner({ connection: config.connection.reference, schedule: rowAddedPollSchedule(client), pollers, requestLimiter: config.requestLimiter, logger: client.triggerLogger });
  return { run: context => runner.run(context), wakeBinding: bindingName => runner.wakePoller(bindingName) };
}

/** Binds one rowAdded binding saved in the project configuration to its application target. */
export interface ProjectRowAddedTriggerRoute {
  /** The binding whose spreadsheet and tab Dex Web saved. */ bindingName: string;
  /** Receives each added row after the binding's durable project inbox records it, such as a newDexFlowTriggerTarget. */ target: TriggerTarget<RowAddedEvent>;
}

/**
 * Open connectionName from the loaded project and build a durable runner for its saved bindings. Each binding's
 * cursor lives in project storage beside its Trigger inbox, and every row is recorded in that inbox before the
 * cursor commits past it, so a restarted or replaced replica neither misses nor replays a row. Dex Web projects
 * keep cursors in process memory, as Go does, so a local restart reseeds. A route whose binding is not
 * configured or is invalid throws. The client options' triggerLogger also applies to the inboxes.
 */
export function newProjectRowAddedTriggerRunner(project: LoadedProject, connectionName: string, routes: readonly ProjectRowAddedTriggerRoute[], options: SpreadsheetClientOptions = {}): RowAddedTriggerRunner {
  const connection = newProjectConnection(project, connectionName, options);
  const triggerName = RowAddedTriggerDefinition.trigger.triggerName;
  const runnerRoutes = routes.map(route => {
    if (!route.target) throw new TypeError(`Google Sheets rowAdded binding "${route.bindingName}" target is required`);
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName, triggerName, bindingName: route.bindingName };
    let configuration: RowAddedTriggerConfiguration;
    try { configuration = decodeTriggerConfiguration(project.configuration, key, RowAddedTriggerConfigurationSchema); }
    catch (error) { throw new Error(`Google Sheets rowAdded binding "${route.bindingName}" configuration: ${error instanceof Error ? error.message : String(error)}`, { cause: error }); }
    const target = newDurableTriggerTarget(project.triggerInbox(key), key, route.target, {
      ...(options.triggerLogger ? { logger: options.triggerLogger } : {}),
      decodePayload: (_payloadJSON, payloadBytes) => RowAddedEventSchema.parse(parseTriggerPayloadJSON(payloadBytes)),
    });
    return { bindingName: route.bindingName, configuration, cursor: project.pollCursorStore(key), target };
  });
  return newRowAddedTriggerRunner({ connection, routes: runnerRoutes });
}

function spreadsheetClientOf(connection: Connection | undefined): SpreadsheetClient {
  if (!(connection?.operations instanceof SpreadsheetClient)) throw new TypeError('Google Sheets rowAdded requires a connection from newProjectConnection or newConnection with a SpreadsheetClient');
  return connection.operations;
}
