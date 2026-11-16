// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newTrigger, type ConnectionRef, type TriggerRunner, type TriggerTarget } from '@superdurable/connectors-sdk';
import { newBindingPoller, newBindingTriggerSource, newMemoryCursorStore, newRunner, type CursorStore, type Poller, type Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { ObjectNotFoundError, decodeTriggerConfiguration, newDurableTriggerTarget, parseTriggerPayloadJSON, type LoadedProject, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { ZohoDeskClient, type ZohoDeskClientOptions } from './client.js';
import { newProjectConnection } from './project.js';
import type { Connection } from './generated/connection.js';
import { ConnectorID } from './generated/definitions.js';
import { TicketChangeFetcher, TicketSnapshotOptions } from './ticket_change_fetcher.js';
import {
  TicketChangedEventSchema, TicketChangedTriggerConfigurationSchema, TicketChangedTriggerDefinition, validateTicketChangedTriggerConfiguration,
  type TicketChangedEvent, type TicketChangedTriggerConfiguration,
} from './ticket_changed.js';

/** Binds one ticketChanged configuration to its application target and cursor. */
export interface TicketChangedTriggerRoute {
  /** The stable application binding name, unique within the runner. */ bindingName: string;
  /** Selects the watched department; omitted watches every department the connection's agent can see. */ configuration?: TicketChangedTriggerConfiguration | undefined;
  /**
   * Routes each change into the application. A target that prepares deliveries, such as newDurableTriggerTarget,
   * records each change before the cursor commits past it; any other target consumes it first.
   */
  target: TriggerTarget<TicketChangedEvent>;
  /**
   * Stores the binding's search position and recent ticket versions, such as LoadedProject.pollCursorStore.
   * Omitted keeps it in memory, so every process start records a new baseline and changes made while stopped are lost.
   */
  cursor?: CursorStore | undefined;
}

/** Configures every Zoho Desk Trigger binding polled through one connection. */
export interface TicketTriggerRunnerConfig {
  /** A connection from newProjectConnection or newConnection with a ZohoDeskClient. */ connection: Connection;
  /** The ticketChanged bindings; at least one is required. */ ticketChangedRoutes: readonly TicketChangedTriggerRoute[];
}

/**
 * Create one poll runner for the configured ticketChanged bindings. run replays each durable target, records the
 * tickets modified in the last TicketSearchIndexLookbackMs as a baseline for a binding without a cursor, then polls
 * every binding each pollInterval with jitter. Each poll reads Zoho Desk ticket search in ascending modified-time
 * order, at most 100 tickets per request and 10 requests per poll. A 429 pauses every binding of the connection for
 * Retry-After, or backs off exponentially without it; other failures back off per binding up to 15 minutes. A
 * search that rejects the cursor's time range or offset, or a cursor for another department, records a new baseline
 * and logs a WARN record. Delivery is at least once, and each ticket version keeps one Trigger event ID across
 * refetches. Throws for a mock connection, no routes, a blank or duplicated binding name, a missing target, or an
 * invalid department.
 */
export function newTicketTriggerRunner(config: TicketTriggerRunnerConfig): Runner {
  const client = zohoDeskClientOf(config.connection);
  if (!config.ticketChangedRoutes?.length) throw new TypeError('Zoho Desk ticketChanged routes are required');
  const pollers = config.ticketChangedRoutes.map(route => newTicketChangedPoller(client, config.connection.reference, route));
  return newRunner({ connection: config.connection.reference, schedule: { intervalMs: client.pollIntervalMs }, pollers, logger: client.triggerLogger });
}

/** Selects one ticketChanged binding saved in the project configuration. */
export interface ProjectTicketChangedTriggerRoute {
  /** Names the saved binding. */ bindingName: string;
  /** Routes each change into the application, usually newDexFlowTriggerTarget. */ target: TriggerTarget<TicketChangedEvent>;
}

/** Selects Zoho Desk Trigger bindings saved in the project configuration. */
export interface ProjectTicketTriggerRunnerConfig {
  /** The ticketChanged bindings; at least one is required. */ ticketChangedRoutes: readonly ProjectTicketChangedTriggerRoute[];
}

/**
 * Open connectionName from the loaded project, load each binding's saved department (an unsaved binding watches
 * every department), and create a durable runner: each route records changes in its binding's project Trigger inbox
 * before its search position commits in the binding's project poll cursor, so a restarted or replaced replica
 * neither misses nor repeats a change. The options' logger also applies to those inboxes. Throws as
 * newTicketTriggerRunner does and for an invalid saved configuration.
 */
export function newProjectTicketTriggerRunner(project: LoadedProject, connectionName: string, config: ProjectTicketTriggerRunnerConfig, options: ZohoDeskClientOptions = {}): Runner {
  if (!project) throw new TypeError('Zoho Desk ticketChanged Triggers require a loaded project');
  const connection = newProjectConnection(project, connectionName, options);
  const client = zohoDeskClientOf(connection);
  const triggerName = TicketChangedTriggerDefinition.trigger.triggerName;
  const routes = (config.ticketChangedRoutes ?? []).map((route): TicketChangedTriggerRoute => {
    if (!route?.target) throw new TypeError(`Zoho Desk ticketChanged binding "${route?.bindingName}" target is required`);
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName: connection.reference.name, triggerName, bindingName: route.bindingName };
    let configuration: TicketChangedTriggerConfiguration;
    try { configuration = decodeTriggerConfiguration(project.configuration, key, TicketChangedTriggerConfigurationSchema); }
    catch (error) {
      // An unsaved binding watches every department, as a blank picker does.
      if (error instanceof ObjectNotFoundError) configuration = {};
      else throw new Error(`zoho-desk ticketChanged binding "${route.bindingName}" configuration: ${error instanceof Error ? error.message : String(error)}`, { cause: error });
    }
    const target = newDurableTriggerTarget(project.triggerInbox(key), key, route.target, {
      logger: client.triggerLogger, decodePayload: (_payloadJSON, payloadBytes) => TicketChangedEventSchema.parse(parseTriggerPayloadJSON(payloadBytes)),
    });
    return { bindingName: route.bindingName, configuration, target, cursor: project.pollCursorStore(key) };
  });
  return newTicketTriggerRunner({ connection, ticketChangedRoutes: routes });
}

/** Configuration of one ticketChanged Trigger built by hand, as Go's generated NewTicketChangedTrigger takes. */
export interface TicketChangedTriggerConfig {
  /** A connection from newProjectConnection or newConnection with a ZohoDeskClient. */ connection: Connection;
  /** Static connection name; must equal the connection's name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** The watched department; omitted watches every department. */ configuration?: TicketChangedTriggerConfiguration | undefined;
  /** Routes each change into the application. */ target: TriggerTarget<TicketChangedEvent>;
}

/**
 * Create one ticketChanged Trigger polled every pollInterval. It has no project storage, so it keeps the cursor in
 * memory and records a new baseline on every start; use newProjectTicketTriggerRunner for durable polling. Throws
 * for an invalid department or a connection name that differs from the connection's.
 */
export function newTicketChangedTrigger(config: TicketChangedTriggerConfig): TriggerRunner {
  const client = zohoDeskClientOf(config.connection);
  const reference = config.connection.reference;
  if (config.connectionName !== reference.name) throw new TypeError(`zoho-desk connector trigger connectionName "${config.connectionName}" must equal its connection's name "${reference.name}"`);
  const configuration = config.configuration ?? {};
  validateConfiguration(configuration);
  const definition = TicketChangedTriggerDefinition;
  const source = newBindingTriggerSource<TicketChangedEvent>({
    definition, binding: { connection: reference, trigger: definition.trigger, name: definition.trigger.triggerName },
    cursor: newMemoryCursorStore(), fetcher: new TicketChangeFetcher(client, reference, configuration), snapshot: TicketSnapshotOptions,
  }, { intervalMs: client.pollIntervalMs }, client.triggerLogger);
  return newTrigger({ definition, binding: { connection: reference, trigger: definition.trigger, name: config.bindingName }, source, target: config.target, logger: client.triggerLogger });
}

function newTicketChangedPoller(client: ZohoDeskClient, connection: ConnectionRef, route: TicketChangedTriggerRoute): Poller {
  if (!route?.bindingName?.trim() || !route.target) throw new TypeError('Zoho Desk ticketChanged binding name and target are required');
  const configuration = route.configuration ?? {};
  validateConfiguration(configuration);
  const definition = TicketChangedTriggerDefinition;
  return newBindingPoller<TicketChangedEvent>({
    definition, binding: { connection, trigger: definition.trigger, name: route.bindingName },
    cursor: route.cursor ?? newMemoryCursorStore(), fetcher: new TicketChangeFetcher(client, connection, configuration),
    target: route.target, snapshot: TicketSnapshotOptions,
  });
}

function validateConfiguration(configuration: TicketChangedTriggerConfiguration): void {
  try { validateTicketChangedTriggerConfiguration(configuration); }
  catch (error) { throw new Error(`zoho-desk ticketChanged configuration: ${error instanceof Error ? error.message : String(error)}`, { cause: error }); }
}

function zohoDeskClientOf(connection: Connection): ZohoDeskClient {
  if (!(connection?.operations instanceof ZohoDeskClient)) throw new TypeError('Zoho Desk Triggers require a connection from newProjectConnection or newConnection with a ZohoDeskClient, not a mock');
  return connection.operations;
}
