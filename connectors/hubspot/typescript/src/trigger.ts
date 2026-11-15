// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newTrigger, type ConnectionRef, type TriggerRunner, type TriggerTarget } from '@superdurable/connectors-sdk';
import { newBindingPoller, newBindingTriggerSource, newMemoryCursorStore, newRunner, type CursorStore, type Poller, type Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { ObjectNotFoundError, decodeTriggerConfiguration, newDurableTriggerTarget, parseTriggerPayloadJSON, type LoadedProject, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { HubSpotClient, type HubSpotClientOptions } from './client.js';
import { ConnectorID } from './generated/definitions.js';
import type { Connection } from './generated/connection.js';
import { newProjectConnection } from './project.js';
import { CRMObjectChangeFetcher } from './crm_object_change_fetcher.js';
import {
  CRMObjectChangedEventSchema, CRMObjectChangedTriggerConfigurationSchema, CRMObjectChangedTriggerDefinition, validateCRMObjectChangedTriggerConfiguration,
  type CRMObjectChangedEvent, type CRMObjectChangedTriggerConfiguration,
} from './crm_object_changed.js';

/** Binds one crmObjectChanged configuration to its application target and cursor. */
export interface CRMObjectChangedTriggerRoute {
  /** The stable application binding name, unique within the runner. */ bindingName: string;
  /** Selects the watched object type and the returned properties. */ configuration: CRMObjectChangedTriggerConfiguration;
  /**
   * Routes each change into the application. A target that prepares deliveries, such as newDurableTriggerTarget,
   * records each change before the cursor commits past it; any other target consumes it first.
   */
  target: TriggerTarget<CRMObjectChangedEvent>;
  /**
   * Stores the binding's search position, such as LoadedProject.pollCursorStore. Omitted keeps it in memory, so
   * every process start records a new baseline and changes made while stopped are lost.
   */
  cursor?: CursorStore | undefined;
}

/** Configures every HubSpot Trigger binding polled through one connection. */
export interface CRMObjectTriggerRunnerConfig {
  /** A connection from newProjectConnection or newConnection with a HubSpotClient. */ connection: Connection;
  /** The crmObjectChanged bindings; at least one is required. */ crmObjectChangedRoutes: readonly CRMObjectChangedTriggerRoute[];
}

/**
 * Create one poll runner for the configured crmObjectChanged bindings. run replays each durable target, records a
 * baseline for a binding without a cursor, then polls every binding each pollInterval with jitter, in
 * configuration order, one search request at a time. A 429 or 423 pauses every binding of the connection for
 * Retry-After or X-HubSpot-RateLimit-Interval-Milliseconds, and an exhausted daily limit backs off exponentially up
 * to 15 minutes; other failures back off per binding. A paging offset HubSpot rejects records a new baseline and
 * logs a WARN record. Delivery is at least once, and each record version keeps one Trigger event ID across
 * refetches. Throws for invalid wiring or configuration.
 */
export function newCRMObjectTriggerRunner(config: CRMObjectTriggerRunnerConfig): Runner {
  const client = hubSpotClientOf(config.connection);
  if (!config.crmObjectChangedRoutes?.length) throw new TypeError('HubSpot crmObjectChanged routes are required');
  const pollers = config.crmObjectChangedRoutes.map(route => newCRMObjectChangedPoller(client, config.connection.reference, route));
  return newRunner({ connection: config.connection.reference, schedule: { intervalMs: client.pollIntervalMs }, pollers, logger: client.triggerLogger });
}

/** Selects one crmObjectChanged binding saved in the project configuration. */
export interface ProjectCRMObjectChangedTriggerRoute {
  /** Names the saved binding. */ bindingName: string;
  /** Property names to return on each delivered record, as the configuration's properties; applies when the saved binding names none. */ properties?: string[] | undefined;
  /** Routes each change into the application, usually newDexFlowTriggerTarget. */ target: TriggerTarget<CRMObjectChangedEvent>;
}

/** Selects HubSpot Trigger bindings saved in the project configuration. */
export interface ProjectCRMObjectTriggerRunnerConfig {
  /** The crmObjectChanged bindings; at least one is required. */ crmObjectChangedRoutes: readonly ProjectCRMObjectChangedTriggerRoute[];
}

/**
 * Open connectionName from the loaded project, load each binding's saved object type, and create a durable runner:
 * each route records changes in its binding's project Trigger inbox before its search position commits in the
 * binding's project poll cursor, so a restarted or replaced replica neither misses nor repeats a change. A binding
 * without a saved object type throws. The options' logger also applies to those inboxes.
 */
export function newProjectCRMObjectTriggerRunner(project: LoadedProject, connectionName: string, config: ProjectCRMObjectTriggerRunnerConfig, options: HubSpotClientOptions = {}): Runner {
  const connection = newProjectConnection(project, connectionName, options);
  const client = hubSpotClientOf(connection);
  const triggerName = CRMObjectChangedTriggerDefinition.trigger.triggerName;
  const routes = (config.crmObjectChangedRoutes ?? []).map((route): CRMObjectChangedTriggerRoute => {
    if (!route.target) throw new TypeError(`HubSpot crmObjectChanged binding "${route.bindingName}" target is required`);
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName: connection.reference.name, triggerName, bindingName: route.bindingName };
    let configuration: CRMObjectChangedTriggerConfiguration;
    try { configuration = decodeTriggerConfiguration(project.configuration, key, CRMObjectChangedTriggerConfigurationSchema); }
    catch (error) {
      if (error instanceof ObjectNotFoundError) throw new Error(`hubspot crmObjectChanged binding "${route.bindingName}" has no saved object type; pick one in Dex Web`);
      throw new Error(`hubspot crmObjectChanged binding "${route.bindingName}" configuration: ${error instanceof Error ? error.message : String(error)}`, { cause: error });
    }
    if (!configuration.properties?.length && route.properties) configuration = { ...configuration, properties: route.properties };
    const target = newDurableTriggerTarget(project.triggerInbox(key), key, route.target, {
      logger: client.triggerLogger, decodePayload: (_payloadJSON, payloadBytes) => CRMObjectChangedEventSchema.parse(parseTriggerPayloadJSON(payloadBytes)),
    });
    return { bindingName: route.bindingName, configuration, target, cursor: project.pollCursorStore(key) };
  });
  return newCRMObjectTriggerRunner({ connection, crmObjectChangedRoutes: routes });
}

/** Configuration of one crmObjectChanged Trigger built by hand. */
export interface CRMObjectChangedTriggerConfig {
  /** A connection from newProjectConnection or newConnection with a HubSpotClient. */ connection: Connection;
  /** Static connection name; must equal the connection's name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** The watched object type and returned properties. */ configuration: CRMObjectChangedTriggerConfiguration;
  /** Routes each change into the application. */ target: TriggerTarget<CRMObjectChangedEvent>;
}

/**
 * Create one crmObjectChanged Trigger polled every pollInterval. It has no project storage, so it keeps the cursor
 * in memory and records a new baseline on every start; use newProjectCRMObjectTriggerRunner for durable polling.
 * Throws for an invalid configuration or a connection name that differs from the connection's.
 */
export function newCRMObjectChangedTrigger(config: CRMObjectChangedTriggerConfig): TriggerRunner {
  const client = hubSpotClientOf(config.connection);
  const reference = config.connection.reference;
  if (config.connectionName !== reference.name) throw new TypeError(`hubspot connector trigger connectionName "${config.connectionName}" must equal its connection's name "${reference.name}"`);
  validateConfiguration(config.configuration);
  const definition = CRMObjectChangedTriggerDefinition;
  const source = newBindingTriggerSource<CRMObjectChangedEvent>({
    definition, binding: { connection: reference, trigger: definition.trigger, name: definition.trigger.triggerName },
    cursor: newMemoryCursorStore(), fetcher: new CRMObjectChangeFetcher(client, reference, config.configuration),
  }, { intervalMs: client.pollIntervalMs }, client.triggerLogger);
  return newTrigger({ definition, binding: { connection: reference, trigger: definition.trigger, name: config.bindingName }, source, target: config.target, logger: client.triggerLogger });
}

function newCRMObjectChangedPoller(client: HubSpotClient, connection: ConnectionRef, route: CRMObjectChangedTriggerRoute): Poller {
  if (!route.bindingName?.trim() || !route.target) throw new TypeError('HubSpot crmObjectChanged binding name and target are required');
  validateConfiguration(route.configuration);
  const definition = CRMObjectChangedTriggerDefinition;
  return newBindingPoller<CRMObjectChangedEvent>({
    definition, binding: { connection, trigger: definition.trigger, name: route.bindingName },
    cursor: route.cursor ?? newMemoryCursorStore(), fetcher: new CRMObjectChangeFetcher(client, connection, route.configuration), target: route.target,
  });
}

function validateConfiguration(configuration: CRMObjectChangedTriggerConfiguration): void {
  try { validateCRMObjectChangedTriggerConfiguration(configuration); }
  catch (error) { throw new Error(`hubspot crmObjectChanged configuration: ${error instanceof Error ? error.message : String(error)}`, { cause: error }); }
}

function hubSpotClientOf(connection: Connection): HubSpotClient {
  if (!(connection?.operations instanceof HubSpotClient)) throw new TypeError('HubSpot Triggers require a connection from newProjectConnection or newConnection with a HubSpotClient');
  return connection.operations;
}
