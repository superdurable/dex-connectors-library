// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newTrigger, type ConnectionRef, type TriggerRunner, type TriggerTarget } from '@superdurable/connectors-sdk';
import { newBindingPoller, newBindingTriggerSource, newMemoryCursorStore, newRunner, type CursorStore, type Poller, type Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { ObjectNotFoundError, decodeTriggerConfiguration, newDurableTriggerTarget, parseTriggerPayloadJSON, type LoadedProject, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { SalesforceClient, type SalesforceClientOptions } from './client.js';
import { ConnectorID } from './generated/definitions.js';
import type { Connection } from './generated/connection.js';
import { newProjectConnection } from './connection.js';
import { RecordPollFetcher } from './record_modified_fetcher.js';
import {
  RecordModifiedEventSchema, RecordModifiedPolledTriggerDefinition, RecordModifiedTriggerConfigurationSchema, validateRecordModifiedTriggerConfiguration,
  type RecordModifiedEvent, type RecordModifiedTriggerConfiguration,
} from './record_modified.js';

/** Binds one recordModifiedPolled configuration to its application target and cursor. */
export interface RecordModifiedPolledTriggerRoute {
  /** The stable application binding name, unique within the runner. */ bindingName: string;
  /** Selects the watched object and the extra fields read with each record. */ configuration: RecordModifiedTriggerConfiguration;
  /**
   * Routes each record change into the application. A target that prepares deliveries, such as newDurableTriggerTarget,
   * records each change before the cursor commits past it; any other target consumes it first.
   */
  target: TriggerTarget<RecordModifiedEvent>;
  /**
   * Stores the binding's (SystemModstamp, Id) position, such as LoadedProject.pollCursorStore. Omitted keeps it in memory, so
   * every process start records a new baseline and changes made while stopped are lost.
   */
  cursor?: CursorStore | undefined;
}

/** Configures every Salesforce Trigger binding polled through one connection. */
export interface RecordTriggerRunnerConfig {
  /** A connection from newProjectConnection or newConnection with a SalesforceClient. */ connection: Connection;
  /** The recordModifiedPolled bindings; at least one is required. */ recordModifiedPolledRoutes: readonly RecordModifiedPolledTriggerRoute[];
}

/**
 * Create one poll runner for the configured recordModifiedPolled bindings. run replays each durable target, records
 * a baseline for a binding without a cursor, then polls every binding each pollInterval with jitter, reading at
 * most pollPageSize records per request while a backlog remains. REQUEST_LIMIT_EXCEEDED or HTTP 429 pauses every
 * binding of the connection for Retry-After, or backs off exponentially without it; other failures, such as an
 * object or field the user cannot read, back off per binding. Delivery is at least once, and each record change
 * keeps one Trigger event ID across refetches. Throws for invalid wiring or configuration.
 */
export function newRecordTriggerRunner(config: RecordTriggerRunnerConfig): Runner {
  const client = salesforceClientOf(config.connection);
  if (!config.recordModifiedPolledRoutes?.length) throw new TypeError('Salesforce recordModifiedPolled routes are required');
  const pollers = config.recordModifiedPolledRoutes.map(route => newRecordModifiedPolledPoller(client, config.connection.reference, route));
  return newRunner({ connection: config.connection.reference, schedule: { intervalMs: client.pollIntervalMs }, pollers, logger: client.triggerLogger });
}

/** Selects one recordModifiedPolled binding saved in the project configuration. */
export interface ProjectRecordModifiedPolledTriggerRoute {
  /** Names the saved binding. */ bindingName: string;
  /** Routes each change into the application, usually newDexFlowTriggerTarget. */ target: TriggerTarget<RecordModifiedEvent>;
}

/** Selects Salesforce Trigger bindings saved in the project configuration. */
export interface ProjectRecordTriggerRunnerConfig {
  /** The recordModifiedPolled bindings; at least one is required. */ recordModifiedPolledRoutes: readonly ProjectRecordModifiedPolledTriggerRoute[];
}

/**
 * Open connectionName from the loaded project, load each binding's saved object and fields, and create a durable runner:
 * each route records changes in its binding's project Trigger inbox before its cursor commits in the
 * binding's project poll cursor, so a restarted or replaced replica neither misses nor repeats a change. A binding
 * without a saved object throws. The options' logger also applies to those inboxes.
 */
export function newProjectRecordTriggerRunner(project: LoadedProject, connectionName: string, config: ProjectRecordTriggerRunnerConfig, options: SalesforceClientOptions = {}): Runner {
  const connection = newProjectConnection(project, connectionName, options);
  const client = salesforceClientOf(connection);
  const triggerName = RecordModifiedPolledTriggerDefinition.trigger.triggerName;
  const routes = (config.recordModifiedPolledRoutes ?? []).map((route): RecordModifiedPolledTriggerRoute => {
    if (!route.target) throw new TypeError(`Salesforce recordModifiedPolled binding "${route.bindingName}" target is required`);
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName: connection.reference.name, triggerName, bindingName: route.bindingName };
    let configuration: RecordModifiedTriggerConfiguration;
    try { configuration = decodeTriggerConfiguration(project.configuration, key, RecordModifiedTriggerConfigurationSchema); }
    catch (error) {
      if (error instanceof ObjectNotFoundError) throw new Error(`salesforce recordModifiedPolled binding "${route.bindingName}" has no saved object; choose one in Dex Web Connections`);
      throw new Error(`salesforce recordModifiedPolled binding "${route.bindingName}" configuration: ${error instanceof Error ? error.message : String(error)}`, { cause: error });
    }
    const target = newDurableTriggerTarget(project.triggerInbox(key), key, route.target, {
      logger: client.triggerLogger, decodePayload: (_payloadJSON, payloadBytes) => RecordModifiedEventSchema.parse(parseTriggerPayloadJSON(payloadBytes)),
    });
    return { bindingName: route.bindingName, configuration, target, cursor: project.pollCursorStore(key) };
  });
  return newRecordTriggerRunner({ connection, recordModifiedPolledRoutes: routes });
}

/** Configuration of one recordModifiedPolled Trigger built by hand. */
export interface RecordModifiedPolledTriggerConfig {
  /** A connection from newProjectConnection or newConnection with a SalesforceClient. */ connection: Connection;
  /** Static connection name; must equal the connection's name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** The watched object and the extra fields read with each record. */ configuration: RecordModifiedTriggerConfiguration;
  /** Routes each record change into the application. */ target: TriggerTarget<RecordModifiedEvent>;
}

/**
 * Create one recordModifiedPolled Trigger polled every pollInterval. It has no project storage, so it keeps the cursor
 * in memory and records a new baseline on every start; use newProjectRecordTriggerRunner for durable polling.
 * Throws for an invalid configuration or a connection name that differs from the connection's.
 */
export function newRecordModifiedPolledTrigger(config: RecordModifiedPolledTriggerConfig): TriggerRunner {
  const client = salesforceClientOf(config.connection);
  const reference = config.connection.reference;
  if (config.connectionName !== reference.name) throw new TypeError(`salesforce connector trigger connectionName "${config.connectionName}" must equal its connection's name "${reference.name}"`);
  validateConfiguration(config.configuration);
  const definition = RecordModifiedPolledTriggerDefinition;
  const source = newBindingTriggerSource<RecordModifiedEvent>({
    definition, binding: { connection: reference, trigger: definition.trigger, name: definition.trigger.triggerName },
    cursor: newMemoryCursorStore(), fetcher: new RecordPollFetcher(client, reference, config.configuration),
  }, { intervalMs: client.pollIntervalMs }, client.triggerLogger);
  return newTrigger({ definition, binding: { connection: reference, trigger: definition.trigger, name: config.bindingName }, source, target: config.target, logger: client.triggerLogger });
}

function newRecordModifiedPolledPoller(client: SalesforceClient, connection: ConnectionRef, route: RecordModifiedPolledTriggerRoute): Poller {
  if (!route.bindingName?.trim() || !route.target) throw new TypeError('Salesforce recordModifiedPolled binding name and target are required');
  validateConfiguration(route.configuration);
  const definition = RecordModifiedPolledTriggerDefinition;
  return newBindingPoller<RecordModifiedEvent>({
    definition, binding: { connection, trigger: definition.trigger, name: route.bindingName },
    cursor: route.cursor ?? newMemoryCursorStore(), fetcher: new RecordPollFetcher(client, connection, route.configuration), target: route.target,
  });
}

function validateConfiguration(configuration: RecordModifiedTriggerConfiguration): void {
  try { validateRecordModifiedTriggerConfiguration(configuration); }
  catch (error) { throw new Error(`salesforce recordModifiedPolled configuration: ${error instanceof Error ? error.message : String(error)}`, { cause: error }); }
}

function salesforceClientOf(connection: Connection): SalesforceClient {
  if (!(connection?.operations instanceof SalesforceClient)) throw new TypeError('Salesforce Triggers require a connection from newProjectConnection or newConnection with a SalesforceClient');
  return connection.operations;
}
