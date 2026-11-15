// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

/**
 * The databaseRowChanged poll Trigger: each row created or edited in one Notion data source, read by polling the
 * data source query sorted by last edited time, on the Connector SDK's durable poll Trigger kit, at parity with
 * the Go connector. newProjectDatabaseRowTriggerRunner is the durable path.
 */

import {
  mustTriggerBindingDefinition, newTrigger,
  type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerDefinition, type TriggerRunner, type TriggerTarget,
} from '@superdurable/connectors-sdk';
import { newBindingPoller, newBindingTriggerSource, newMemoryCursorStore, newRunner, type CursorStore, type Poller, type PollSchedule, type Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import {
  ObjectNotFoundError, decodeTriggerConfiguration, newDurableTriggerTarget, parseTriggerPayloadJSON,
  type LoadedProject, type TriggerInboxKey,
} from '@superdurable/connectors-sdk/projectconfig';
import { NotionClient, type NotionClientOptions } from './client.js';
import { databaseRowLookbackNanoseconds, maximumSeenDatabaseRows } from './database_row_cursor.js';
import { DatabaseRowFetcher } from './database_row_fetcher.js';
import {
  DatabaseRowChangedEventSchema, DatabaseRowChangedTriggerConfigurationSchema,
  type DatabaseRowChangedEvent, type DatabaseRowChangedTriggerConfiguration,
} from './database_row_models.js';
import type { Connection } from './generated/connection.js';
import { ConnectorID } from './generated/definitions.js';
import { parseID } from './operations.js';
import { newProjectConnection } from './project.js';

export {
  DatabaseRowChangeKind, DatabaseRowChangedEventSchema, DatabaseRowChangedTriggerConfigurationSchema,
  type DatabaseRowChangedEvent, type DatabaseRowChangedTriggerConfiguration,
} from './database_row_models.js';

/** How far before the newest last edited time each poll queries again, in milliseconds, so a row Notion indexes late, or stamps with a time rounded down, is still delivered once. */
export const DatabaseRowLookbackMs = Number(databaseRowLookbackNanoseconds / 1_000_000n);
/** At most this many row versions inside the lookback are remembered; an evicted one may repeat with the same Trigger event ID, which a Dex Flow target does not start twice. */
export const MaximumSeenDatabaseRows = maximumSeenDatabaseRows;

/** The Notion data source picker UI unit, which resolves a database link to the data source it saves. */ export const UIUnitDataSourcePicker = 'dataSourcePicker';
/** Data source picker output port: the data source ID. */ export const UIDataSourcePickerPortDataSourceID = 'dataSourceId';
/** Data source picker output port: the data source name. */ export const UIDataSourcePickerPortDataSourceName = 'dataSourceName';

/** Manifest definition of databaseRowChanged. */
export const DatabaseRowChangedTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: 'databaseRowChanged' },
  description: 'Receive each row created or edited in one Notion data source, read by polling the data source query sorted by last edited time; the first run records a baseline and delivers no existing rows, and trashed rows are not delivered.',
};

/** One static application binding of databaseRowChanged. */
export interface DatabaseRowChangedTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Connector Studio configuration units for the binding, such as the dataSourcePicker unit. */ configurationUI?: ConnectorConfigurationUI;
}

/** Declare a static databaseRowChanged binding for Flow Definition tooling and Dex Web. */
export function defineDatabaseRowChangedTriggerBinding(config: DatabaseRowChangedTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: DatabaseRowChangedTriggerDefinition, ...config });
}

/** Throw TypeError unless dataSourceId is a Notion ID or a Notion URL ending in one. */
export function validateDatabaseRowChangedTriggerConfiguration(configuration: DatabaseRowChangedTriggerConfiguration): void {
  if (!configuration?.dataSourceId?.trim()) throw new TypeError('dataSourceId is required; pick the watched Notion data source in Dex Web');
  try { parseID(configuration.dataSourceId); }
  catch (error) { throw new TypeError(`dataSourceId: ${error instanceof Error ? error.message : String(error)}`, { cause: error }); }
}

/** Decode a replayed databaseRowChanged payload from a project Trigger inbox written by Go, Python, or TypeScript. */
export function decodeDatabaseRowChangedEventPayload(_payloadJSON: string, payloadBytes: Uint8Array): DatabaseRowChangedEvent {
  return DatabaseRowChangedEventSchema.parse(parseTriggerPayloadJSON(payloadBytes));
}

/** Binds one databaseRowChanged configuration to its application target and cursor. */
export interface DatabaseRowChangedTriggerRoute {
  /** The stable application binding name, unique within the runner. */ bindingName: string;
  /** Selects the watched data source; its dataSourceId is required. */ configuration: DatabaseRowChangedTriggerConfiguration;
  /** Routes each change; a target from newDurableTriggerTarget records it before the cursor commits past it, any other target consumes it first. */ target: TriggerTarget<DatabaseRowChangedEvent>;
  /** Stores the binding's last-edited-time cursor, such as LoadedProject.pollCursorStore; omitted keeps it in memory, so every start records a new baseline and changes made while stopped are lost. */ cursor?: CursorStore;
}

/** Configures every Notion databaseRowChanged binding polled through one connection. */
export interface DatabaseRowTriggerRunnerConfig {
  /** A connection from newConnection with a NotionClient or newProjectConnection. */ connection: Connection;
  /** The databaseRowChanged bindings; at least one is required. */ databaseRowChangedRoutes: readonly DatabaseRowChangedTriggerRoute[];
}

/**
 * Create one poll runner for the configured databaseRowChanged bindings. run replays each durable target, records
 * a baseline for a binding without a cursor, then queries every binding's data source each pollInterval with
 * jitter, at most pollPageSize rows per request and ten requests per poll. A 429 rate_limited or 529
 * service_overload pauses every binding of the connection for Notion's Retry-After seconds, or backs off
 * exponentially without it; a stale start cursor restarts the query, a cursor for another data source records a
 * new baseline, and other failures, such as a data source that is no longer shared, back off per binding up to
 * 15 minutes. Delivery is at least once, and each row version keeps one Trigger event ID across refetches. The
 * cursor documents match the Go connector's byte for byte, so Go and TypeScript replicas can share one binding's
 * cursor and inbox. Throws TypeError for a connection without a NotionClient, no routes, a blank binding name, a
 * missing target, or an invalid configuration.
 */
export function newDatabaseRowTriggerRunner(config: DatabaseRowTriggerRunnerConfig): Runner {
  const client = notionClientOf(config.connection);
  if (!config.databaseRowChangedRoutes?.length) throw new TypeError('Notion databaseRowChanged routes are required');
  const pollers: Poller[] = config.databaseRowChangedRoutes.map(route => {
    if (!route.bindingName?.trim() || !route.target) throw new TypeError('Notion databaseRowChanged binding name and target are required');
    validateRouteConfiguration(route.configuration);
    return newBindingPoller<DatabaseRowChangedEvent>({
      definition: DatabaseRowChangedTriggerDefinition,
      binding: { connection: config.connection.reference, trigger: DatabaseRowChangedTriggerDefinition.trigger, name: route.bindingName },
      cursor: route.cursor ?? newMemoryCursorStore(),
      fetcher: newFetcher(client, config.connection, route.configuration),
      target: route.target,
    });
  });
  return newRunner({ connection: config.connection.reference, schedule: pollSchedule(client), pollers, ...(client.triggerLogger ? { logger: client.triggerLogger } : {}) });
}

/** Selects one databaseRowChanged binding saved in the project configuration. */
export interface ProjectDatabaseRowChangedTriggerRoute {
  /** The saved binding's name. */ bindingName: string;
  /** Routes each change into the application, usually newDexFlowTriggerTarget. */ target: TriggerTarget<DatabaseRowChangedEvent>;
}

/** Selects Notion databaseRowChanged bindings saved in the project configuration. */
export interface ProjectDatabaseRowTriggerRunnerConfig {
  /** The databaseRowChanged bindings; at least one is required. */ databaseRowChangedRoutes: readonly ProjectDatabaseRowChangedTriggerRoute[];
}

/**
 * Open connectionName from the loaded project, load each binding's saved data source (an unsaved binding is a
 * configuration error), and create a durable runner: each route records changes in its binding's project Trigger
 * inbox before its cursor commits in the binding's project poll cursor, so a restarted or replaced replica neither
 * misses nor repeats a change. The client options' logger also applies to those inboxes. Under dexcli dev the poll
 * cursor stays in process memory; hosted project storage keeps it.
 */
export function newProjectDatabaseRowTriggerRunner(project: LoadedProject, connectionName: string, config: ProjectDatabaseRowTriggerRunnerConfig, options: NotionClientOptions = {}): Runner {
  const connection = newProjectConnection(project, connectionName, options);
  const triggerName = DatabaseRowChangedTriggerDefinition.trigger.triggerName;
  const routes = config.databaseRowChangedRoutes.map((route): DatabaseRowChangedTriggerRoute => {
    if (!route.target) throw new TypeError(`Notion databaseRowChanged binding ${JSON.stringify(route.bindingName)} target is required`);
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName, triggerName, bindingName: route.bindingName };
    let configuration: DatabaseRowChangedTriggerConfiguration;
    try { configuration = decodeTriggerConfiguration(project.configuration, key, DatabaseRowChangedTriggerConfigurationSchema); }
    catch (error) {
      if (error instanceof ObjectNotFoundError) throw new TypeError(`notion databaseRowChanged binding ${JSON.stringify(route.bindingName)} has no saved data source; pick one in Dex Web and restart`, { cause: error });
      throw new TypeError(`notion databaseRowChanged binding ${JSON.stringify(route.bindingName)} configuration: ${error instanceof Error ? error.message : String(error)}`, { cause: error });
    }
    const target = newDurableTriggerTarget(project.triggerInbox(key), key, route.target, { ...(options.logger ? { logger: options.logger } : {}), decodePayload: decodeDatabaseRowChangedEventPayload });
    return { bindingName: route.bindingName, configuration, target, cursor: project.pollCursorStore(key) };
  });
  return newDatabaseRowTriggerRunner({ connection, databaseRowChangedRoutes: routes });
}

/** Configuration of one databaseRowChanged Trigger with its cursor in memory. */
export interface DatabaseRowChangedTriggerConfig {
  /** A connection from newConnection with a NotionClient or newProjectConnection. */ connection: Connection;
  /** Static connection name; must equal the connection's name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Selects the watched data source. */ configuration: DatabaseRowChangedTriggerConfiguration;
  /** Routes each change into the application. */ target: TriggerTarget<DatabaseRowChangedEvent>;
}

/**
 * Create one databaseRowChanged TriggerRunner that keeps its cursor in memory, so every start records a new
 * baseline and changes made while the process is stopped are lost; use it for tests and demonstrations and
 * newProjectDatabaseRowTriggerRunner otherwise. Throws as newDatabaseRowTriggerRunner does.
 */
export function newDatabaseRowChangedTrigger(config: DatabaseRowChangedTriggerConfig): TriggerRunner {
  if (config.connectionName !== config.connection.reference.name) throw new TypeError(`notion connector trigger connectionName "${config.connectionName}" must equal its connection's name "${config.connection.reference.name}"`);
  const client = notionClientOf(config.connection);
  if (!config.bindingName?.trim() || !config.target) throw new TypeError('Notion databaseRowChanged binding name and target are required');
  validateRouteConfiguration(config.configuration);
  const binding = { connection: config.connection.reference, trigger: DatabaseRowChangedTriggerDefinition.trigger, name: config.bindingName };
  const source = newBindingTriggerSource<DatabaseRowChangedEvent>({
    definition: DatabaseRowChangedTriggerDefinition, binding, cursor: newMemoryCursorStore(),
    fetcher: newFetcher(client, config.connection, config.configuration),
  }, pollSchedule(client), client.triggerLogger);
  return newTrigger({ definition: DatabaseRowChangedTriggerDefinition, binding, source, target: config.target, ...(client.triggerLogger ? { logger: client.triggerLogger } : {}) });
}

function newFetcher(client: NotionClient, connection: Connection, configuration: DatabaseRowChangedTriggerConfiguration): DatabaseRowFetcher {
  return new DatabaseRowFetcher(client, connection.reference, DatabaseRowChangedTriggerDefinition.trigger, parseID(configuration.dataSourceId ?? ''));
}

function validateRouteConfiguration(configuration: DatabaseRowChangedTriggerConfiguration): void {
  try { validateDatabaseRowChangedTriggerConfiguration(configuration ?? {}); }
  catch (error) { throw new TypeError(`notion databaseRowChanged configuration: ${error instanceof Error ? error.message : String(error)}`, { cause: error }); }
}

function pollSchedule(client: NotionClient): PollSchedule {
  // An omitted maxBackoffMs uses the kit's cap, which grows with a long poll interval.
  return { intervalMs: client.pollIntervalMs };
}

function notionClientOf(connection: Connection): NotionClient {
  if (!(connection?.operations instanceof NotionClient)) throw new TypeError('Notion Triggers require a connection from newProjectConnection or newConnection with a NotionClient');
  return connection.operations;
}
