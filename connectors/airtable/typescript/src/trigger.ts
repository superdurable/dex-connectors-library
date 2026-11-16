// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

/**
 * The recordChanged poll Trigger: each record of one picked table that is created or changed after the last
 * processed change, as reported by a picked Last modified time field, on the Connector SDK's durable poll
 * Trigger kit at parity with the Go connector. Cursor documents and event IDs match Go's byte for byte, so
 * Go, Python, and TypeScript replicas share them. newProjectRecordChangedTriggerRunner is the durable path.
 */

import {
  mustTriggerBindingDefinition, newTrigger,
  type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerBindingRef, type TriggerDefinition, type TriggerRunner, type TriggerTarget,
} from '@superdurable/connectors-sdk';
import { newBindingPoller, newBindingTriggerSource, newMemoryCursorStore, newRunner, type CursorStore, type PollSchedule, type RequestLimiter, type Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import {
  ObjectNotFoundError, decodeTriggerConfiguration, newDurableTriggerTarget, parseTriggerPayloadJSON,
  type LoadedProject, type TriggerInboxKey,
} from '@superdurable/connectors-sdk/projectconfig';
import { AirtableClient, type AirtableClientOptions } from './client.js';
import type { Connection } from './generated/connection.js';
import { ConnectorID } from './generated/definitions.js';
import { newProjectConnection } from './project.js';
import { RecordChangedFetcher } from './record_changed_fetcher.js';
import { RecordChangedEventSchema, RecordChangedTriggerConfigurationSchema, type RecordChangedEvent, type RecordChangedTriggerConfiguration } from './record_changed_models.js';

export { RecordChangedEventSchema, RecordChangedTriggerConfigurationSchema, type RecordChangedEvent, type RecordChangedTriggerConfiguration } from './record_changed_models.js';

/** The base picker UI unit, which saves a base ID and name. */ export const UIUnitBasePicker = 'basePicker';
/** Base picker output port: the base ID. */ export const UIBasePickerPortBaseID = 'baseId';
/** Base picker output port: the base name. */ export const UIBasePickerPortBaseName = 'baseName';
/** The table picker UI unit, which saves a table ID and name. */ export const UIUnitTablePicker = 'tablePicker';
/** Table picker port: the base ID it lists tables of. */ export const UITablePickerPortBaseID = 'baseId';
/** Table picker output port: the table ID. */ export const UITablePickerPortTableID = 'tableId';
/** Table picker output port: the table name. */ export const UITablePickerPortTableName = 'tableName';
/** The UI unit that lists a table's Last modified time fields with a time of day. */ export const UIUnitLastModifiedTimeFieldPicker = 'lastModifiedTimeFieldPicker';
/** Field picker port: the base ID. */ export const UILastModifiedTimeFieldPickerPortBaseID = 'baseId';
/** Field picker port: the table ID. */ export const UILastModifiedTimeFieldPickerPortTableID = 'tableId';
/** Field picker output port: the field ID. */ export const UILastModifiedTimeFieldPickerPortFieldID = 'fieldId';
/** Field picker output port: the field name. */ export const UILastModifiedTimeFieldPickerPortFieldName = 'fieldName';

/** Caps the wait after consecutive failed polls, or the poll interval when that is longer. */
const recordChangedMaximumBackoffMs = 5 * 60_000;
const baseIdPattern = /^app[0-9A-Za-z]{14}$/;
const tableIdPattern = /^tbl[0-9A-Za-z]{14}$/;
const fieldIdPattern = /^fld[0-9A-Za-z]{14}$/;

/** Manifest definition of recordChanged. */
export const RecordChangedTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: 'recordChanged' },
  description: 'Receive each record of one picked table that is created or changed after the last processed change, as reported by a picked Last modified time field.',
};

/** One static application binding of recordChanged. */
export interface RecordChangedTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Connector Studio configuration units, such as the base, table, and Last modified time field pickers. */ configurationUI?: ConnectorConfigurationUI;
}

/** Declare a static recordChanged binding for Flow Definition tooling and Dex Web. */
export function defineRecordChangedTriggerBinding(config: RecordChangedTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: RecordChangedTriggerDefinition, ...config });
}

/** Require the base, table, and Last modified time field IDs; throws TypeError naming the first invalid one. */
export function validateRecordChangedTriggerConfiguration(configuration: RecordChangedTriggerConfiguration): void {
  if (!baseIdPattern.test(configuration?.baseId ?? '')) throw new TypeError('Airtable recordChanged baseId must be app followed by 14 letters and digits');
  if (!tableIdPattern.test(configuration.tableId ?? '')) throw new TypeError('Airtable recordChanged tableId must be tbl followed by 14 letters and digits');
  if (!fieldIdPattern.test(configuration.fieldId ?? '')) throw new TypeError('Airtable recordChanged fieldId must be fld followed by 14 letters and digits');
}

/** Decode a replayed recordChanged payload from a project Trigger inbox written by Go, Python, or TypeScript. */
export function decodeRecordChangedEventPayload(_payloadJSON: string, payloadBytes: Uint8Array): RecordChangedEvent {
  return RecordChangedEventSchema.parse(parseTriggerPayloadJSON(payloadBytes));
}

/** Binds one recordChanged binding's configuration and cursor to its application target. */
export interface RecordChangedTriggerRoute {
  /** Names the configured Trigger binding; it is unique within one runner. */ bindingName: string;
  /** Selects the polled table and its Last modified time field. */ configuration: RecordChangedTriggerConfiguration;
  /**
   * Persists the binding's last processed change, normally LoadedProject.pollCursorStore. Use
   * newMemoryCursorStore only when changes made while the process is stopped may be skipped.
   */
  cursor: CursorStore;
  /**
   * Receives each changed record. A target from newDurableTriggerTarget records it before the cursor moves
   * past it; any other target consumes it first.
   */
  target: TriggerTarget<RecordChangedEvent>;
}

/** Configures every recordChanged binding of one Airtable connection. */
export interface RecordChangedTriggerRunnerConfig {
  /** A connection from newConnection with an AirtableClient, or newProjectConnection; its pollInterval and pollPageSize apply. */ connection: Connection;
  /** Polled in this order whenever several are due; at least one is required. */ routes: readonly RecordChangedTriggerRoute[];
  /** Optionally paces every poll request, for example to share Airtable's 50 requests per second per token. */ requestLimiter?: RequestLimiter;
}

/**
 * Validate every route and build one poll runner for the connection; it sends and reads nothing. run replays
 * each durable target; each binding then reads the base schema to find the Last modified time field's
 * current name and seeds its cursor at the table's most recent change, so existing records never start
 * Flows. Later polls list up to pollPageSize records modified since the cursor, oldest change first, and
 * deliver each before the cursor commits. The cursor stores the latest delivered modification time and the
 * records delivered in the five seconds before it, so a change that becomes visible slightly late is still
 * delivered and a record is delivered once per change; a long scan resumes from its stored Airtable offset.
 * A failed poll backs off up to five minutes, and HTTP 429 pauses every binding of the connection for at
 * least Airtable's 30 seconds. Delivery is at least once, and each event ID is a pure function of the record
 * ID and its modification time. Records go to the client's logger option. Throws TypeError for a connection
 * without an AirtableClient, no routes, a blank or repeated binding name, a missing cursor or target, or an
 * invalid configuration.
 */
export function newRecordChangedTriggerRunner(config: RecordChangedTriggerRunnerConfig): Runner {
  const client = airtableClientOf(config.connection);
  if (!config.routes?.length) throw new TypeError('Airtable recordChanged Trigger routes are required');
  const pollers = config.routes.map(route => {
    if (!route.bindingName?.trim() || !route.target || !route.cursor) throw new TypeError('Airtable recordChanged binding name, cursor, and target are required');
    validateRouteConfiguration(route.bindingName, route.configuration);
    const binding = bindingRef(config.connection, route.bindingName);
    return newBindingPoller<RecordChangedEvent>({
      definition: RecordChangedTriggerDefinition, binding, cursor: route.cursor,
      fetcher: new RecordChangedFetcher(client, binding, route.configuration), target: route.target,
    });
  });
  return newRunner({
    connection: config.connection.reference, schedule: pollSchedule(client), pollers,
    ...(config.requestLimiter ? { requestLimiter: config.requestLimiter } : {}),
    ...(client.triggerLogger ? { logger: client.triggerLogger } : {}),
  });
}

/** Binds one recordChanged binding saved in the project configuration to its target. */
export interface ProjectRecordChangedTriggerRoute {
  /** Names the binding whose base, table, and field Dex Web saved. */ bindingName: string;
  /** Receives each changed record after the binding's durable project inbox records it, usually newDexFlowTriggerTarget. */ target: TriggerTarget<RecordChangedEvent>;
}

/**
 * Open connectionName from the loaded project, load each route's saved binding configuration, and build a
 * durable runner: each binding's cursor lives in project storage beside its Trigger inbox, and every change
 * is recorded in that inbox before the cursor commits past it, so a restarted or replaced replica neither
 * misses nor replays a change. options.logger also applies to the inboxes. Under dexcli dev the poll cursor
 * stays in process memory; hosted project storage keeps it. Throws for a missing target, a binding without a
 * saved configuration, or an invalid configuration.
 */
export function newProjectRecordChangedTriggerRunner(project: LoadedProject, connectionName: string, routes: readonly ProjectRecordChangedTriggerRoute[], options: AirtableClientOptions = {}): Runner {
  const connection = newProjectConnection(project, connectionName, options);
  const triggerName = RecordChangedTriggerDefinition.trigger.triggerName;
  const durableRoutes = routes.map((route): RecordChangedTriggerRoute => {
    const name = JSON.stringify(route.bindingName);
    if (!route.target) throw new TypeError(`Airtable recordChanged binding ${name} target is required`);
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName, triggerName, bindingName: route.bindingName };
    let configuration: RecordChangedTriggerConfiguration;
    try { configuration = decodeTriggerConfiguration(project.configuration, key, RecordChangedTriggerConfigurationSchema); }
    catch (error) {
      if (error instanceof ObjectNotFoundError) throw new TypeError(`Airtable recordChanged binding ${name} has no saved configuration; pick the base, table, and field in Dex Web and restart`, { cause: error });
      throw new TypeError(`Airtable recordChanged binding ${name} configuration: ${error instanceof Error ? error.message : String(error)}`, { cause: error });
    }
    const target = newDurableTriggerTarget(project.triggerInbox(key), key, route.target, { ...(options.logger ? { logger: options.logger } : {}), decodePayload: decodeRecordChangedEventPayload });
    return { bindingName: route.bindingName, configuration, cursor: project.pollCursorStore(key), target };
  });
  return newRecordChangedTriggerRunner({ connection, routes: durableRoutes });
}

/** One recordChanged Trigger with its cursor in memory. */
export interface RecordChangedTriggerConfig {
  /** A connection from newConnection with an AirtableClient, or newProjectConnection. */ connection: Connection;
  /** Static binding name. */ bindingName: string;
  /** Selects the polled table and field. */ configuration: RecordChangedTriggerConfiguration;
  /** Receives each changed record. */ target: TriggerTarget<RecordChangedEvent>;
}

/**
 * Create one recordChanged binding's TriggerRunner with its cursor in process memory, so a restart seeds
 * again and changes made while the process is stopped are not delivered; use it for tests and
 * demonstrations, and newProjectRecordChangedTriggerRunner otherwise. Throws as newRecordChangedTriggerRunner does.
 */
export function newRecordChangedTrigger(config: RecordChangedTriggerConfig): TriggerRunner {
  const client = airtableClientOf(config.connection);
  if (!config.bindingName?.trim() || !config.target) throw new TypeError('Airtable recordChanged binding name, cursor, and target are required');
  validateRouteConfiguration(config.bindingName, config.configuration);
  const binding = bindingRef(config.connection, config.bindingName);
  const source = newBindingTriggerSource<RecordChangedEvent>({
    definition: RecordChangedTriggerDefinition, binding, cursor: newMemoryCursorStore(),
    fetcher: new RecordChangedFetcher(client, binding, config.configuration),
  }, pollSchedule(client), client.triggerLogger);
  return newTrigger({ definition: RecordChangedTriggerDefinition, binding, source, target: config.target, ...(client.triggerLogger ? { logger: client.triggerLogger } : {}) });
}

function bindingRef(connection: Connection, bindingName: string): TriggerBindingRef {
  return { connection: connection.reference, trigger: RecordChangedTriggerDefinition.trigger, name: bindingName };
}

function validateRouteConfiguration(bindingName: string, configuration: RecordChangedTriggerConfiguration): void {
  try { validateRecordChangedTriggerConfiguration(configuration); }
  catch (error) { throw new TypeError(`Airtable recordChanged binding ${JSON.stringify(bindingName)}: ${error instanceof Error ? error.message : String(error)}`, { cause: error }); }
}

function pollSchedule(client: AirtableClient): PollSchedule {
  return { intervalMs: client.pollIntervalMs, maxBackoffMs: Math.max(recordChangedMaximumBackoffMs, client.pollIntervalMs) };
}

function airtableClientOf(connection: Connection): AirtableClient {
  if (!(connection?.operations instanceof AirtableClient)) throw new TypeError('Airtable recordChanged requires a connection from newProjectConnection or newConnection with an AirtableClient');
  return connection.operations;
}
