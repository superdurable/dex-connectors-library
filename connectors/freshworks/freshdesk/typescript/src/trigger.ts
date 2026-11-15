// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newTrigger, type TriggerRunner, type TriggerTarget } from '@superdurable/connectors-sdk';
import { newBindingPoller, newBindingTriggerSource, newMemoryCursorStore, newRunner, type CursorStore, type PollSchedule, type Poller, type Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { ObjectNotFoundError, decodeTriggerConfiguration, newDurableTriggerTarget, type LoadedProject, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { FreshdeskClient } from './client.js';
import type { Connection } from './generated/connection.js';
import { ConnectorID } from './generated/definitions.js';
import { TicketChangeFetcher } from './ticket_change_fetcher.js';
import { TicketChangedEventSchema, TicketChangedTriggerConfigurationSchema, TicketChangedTriggerDefinition, type TicketChangedEvent, type TicketChangedTriggerConfiguration } from './trigger_definition.js';

const triggerName = TicketChangedTriggerDefinition.trigger.triggerName;

/** Binds one ticketChanged configuration to its application target and cursor. */
export interface TicketChangedTriggerRoute {
  /** The stable application binding name, unique within the runner. */ bindingName: string;
  /** Selects the delivered change kinds; omitted delivers created and updated tickets. */ configuration?: TicketChangedTriggerConfiguration;
  /**
   * Routes each change into the application. A durable target from newDurableTriggerTarget records each change
   * before the cursor commits past it; any other target consumes it first.
   */
  target: TriggerTarget<TicketChangedEvent>;
  /**
   * Stores the binding's updated_at position, such as LoadedProject.pollCursorStore. Omitted keeps it in memory,
   * so every process start records a new baseline and changes made while stopped are lost.
   */
  cursor?: CursorStore;
}

/** Configures every Freshdesk Trigger binding polled through one connection. */
export interface TicketTriggerRunnerConfig {
  /** A connection from newConnection with a FreshdeskClient or from newProjectConnection; a mock has no client. */ connection: Connection;
  /** The ticketChanged bindings; at least one is required. */ ticketChangedRoutes: TicketChangedTriggerRoute[];
}

function freshdeskClientOf(connection: Connection): FreshdeskClient {
  if (!connection) throw new TypeError('Freshdesk ticketChanged connection is required');
  if (!(connection.operations instanceof FreshdeskClient)) throw new TypeError('Freshdesk ticketChanged Triggers require a FreshdeskClient connection, not a mock');
  return connection.operations;
}

// A zero maximum backoff uses the kit's cap, which grows with a long poll interval.
const pollSchedule = (client: FreshdeskClient): PollSchedule => ({ intervalMs: client.pollIntervalMs });

/**
 * Create one poll runner for the configured ticketChanged bindings. run replays each durable target, records a
 * baseline for a binding without a cursor, then polls every binding each pollInterval with jitter. A Freshdesk
 * 400 for the cursor's query or an undecodable cursor records a new baseline and logs a WARN record; changes
 * made between the last poll and the new baseline are not delivered. A 429 pauses every binding of the
 * connection for Retry-After, because Freshdesk's limit is per account, or backs off exponentially without it;
 * other failures back off per binding up to 15 minutes. Delivery is at least once, and each change keeps one
 * Trigger event ID across refetches. Throws for a missing or mock connection, no routes, a blank or duplicated
 * binding name, a missing target, or an invalid configuration.
 */
export function newTicketTriggerRunner(config: TicketTriggerRunnerConfig): Runner {
  const client = freshdeskClientOf(config.connection);
  if (!config.ticketChangedRoutes?.length) throw new TypeError('Freshdesk ticketChanged routes are required');
  const reference = config.connection.reference;
  const pollers: Poller[] = config.ticketChangedRoutes.map(route => {
    if (!route?.bindingName?.trim() || !route.target) throw new TypeError('Freshdesk ticketChanged binding name and target are required');
    const configuration = parseTicketChangedTriggerConfiguration(route.configuration ?? {});
    return newBindingPoller({
      definition: TicketChangedTriggerDefinition,
      binding: { connection: reference, trigger: TicketChangedTriggerDefinition.trigger, name: route.bindingName },
      cursor: route.cursor ?? newMemoryCursorStore(), fetcher: new TicketChangeFetcher(client, reference, configuration), target: route.target,
    });
  });
  return newRunner({ connection: reference, schedule: pollSchedule(client), pollers, logger: client.triggerLogger });
}

/** Validate a ticketChanged configuration; throws for an unknown field or an unknown or repeated change kind. */
export function parseTicketChangedTriggerConfiguration(configuration: unknown): TicketChangedTriggerConfiguration {
  const parsed = TicketChangedTriggerConfigurationSchema.safeParse(configuration);
  if (!parsed.success) throw new TypeError(`freshdesk ticketChanged configuration: ${parsed.error.issues.map(issue => issue.message).join('; ')}`);
  return parsed.data;
}

/** Decode a replayed Trigger inbox payload that the Go or TypeScript connector recorded. */
export function decodeTicketChangedEventPayload(payloadJSON: string): TicketChangedEvent {
  return TicketChangedEventSchema.parse(JSON.parse(payloadJSON));
}

/** Selects one ticketChanged binding saved in the project configuration. */
export interface ProjectTicketChangedTriggerRoute {
  /** The saved binding name. */ bindingName: string;
  /** Routes each change into the application, usually newDexFlowTriggerTarget. */ target: TriggerTarget<TicketChangedEvent>;
}

/** Selects Freshdesk Trigger bindings saved in the project configuration. */
export interface ProjectTicketTriggerRunnerConfig {
  /** The ticketChanged bindings; at least one is required. */ ticketChangedRoutes: ProjectTicketChangedTriggerRoute[];
}

/**
 * Create a durable runner for bindings saved in the loaded project. connection is the project connection from
 * newProjectConnection(project, name); its client options, such as logger, also apply to the durable inboxes.
 * Each binding's saved configuration is loaded (an unsaved binding delivers every change kind), and each route
 * records changes in its binding's project Trigger inbox before its updated_at position commits in the
 * binding's project poll cursor, so a restarted or replaced replica neither misses nor repeats a change.
 * Throws as newTicketTriggerRunner does and for an invalid saved configuration.
 */
export function newProjectTicketTriggerRunner(project: LoadedProject, connection: Connection, config: ProjectTicketTriggerRunnerConfig): Runner {
  if (!project) throw new TypeError('Freshdesk ticketChanged Triggers require a loaded project');
  const client = freshdeskClientOf(connection);
  const ticketChangedRoutes = (config.ticketChangedRoutes ?? []).map((route): TicketChangedTriggerRoute => {
    if (!route?.target) throw new TypeError('Freshdesk ticketChanged binding target is required');
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName: connection.reference.name, triggerName, bindingName: route.bindingName };
    let configuration: TicketChangedTriggerConfiguration;
    try { configuration = decodeTriggerConfiguration(project.configuration, key, TicketChangedTriggerConfigurationSchema); }
    catch (error) {
      // An unsaved binding delivers every change kind, as an empty configuration does.
      if (!(error instanceof ObjectNotFoundError)) throw new TypeError(`freshdesk ticketChanged binding ${JSON.stringify(route.bindingName)} configuration is invalid`, { cause: error });
      configuration = {};
    }
    const target = newDurableTriggerTarget(project.triggerInbox(key), key, route.target, { logger: client.triggerLogger, decodePayload: decodeTicketChangedEventPayload });
    return { bindingName: route.bindingName, configuration, target, cursor: project.pollCursorStore(key) };
  });
  return newTicketTriggerRunner({ connection, ticketChangedRoutes });
}

/** Configuration of one in-memory ticketChanged Trigger. */
export interface TicketChangedTriggerConfig {
  /** A connection with a FreshdeskClient. */ connection: Connection;
  /** Static connection name; must equal the connection's name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Selects the delivered change kinds; omitted delivers both. */ configuration?: TicketChangedTriggerConfiguration;
  /** Routes accepted changes into the application. */ target: TriggerTarget<TicketChangedEvent>;
}

/**
 * Create one ticketChanged Trigger with an in-memory cursor, as Go's generated NewTicketChangedTrigger. It has no
 * project storage, so it records a new baseline on every start and changes made while it is stopped are lost;
 * use newProjectTicketTriggerRunner for durable delivery. Throws when connectionName differs from the
 * connection's name, for a mock connection, or for an invalid configuration.
 */
export function newTicketChangedTrigger(config: TicketChangedTriggerConfig): TriggerRunner {
  const client = freshdeskClientOf(config.connection);
  const reference = config.connection.reference;
  if (config.connectionName !== reference.name) throw new TypeError(`freshdesk connector trigger connectionName ${JSON.stringify(config.connectionName)} must equal its connection's name ${JSON.stringify(reference.name)}`);
  const configuration = parseTicketChangedTriggerConfiguration(config.configuration ?? {});
  const binding = { connection: reference, trigger: TicketChangedTriggerDefinition.trigger, name: config.bindingName };
  const source = newBindingTriggerSource<TicketChangedEvent>({
    definition: TicketChangedTriggerDefinition, binding, cursor: newMemoryCursorStore(), fetcher: new TicketChangeFetcher(client, reference, configuration),
  }, pollSchedule(client), client.triggerLogger);
  return newTrigger({ definition: TicketChangedTriggerDefinition, binding, source, target: config.target, logger: client.triggerLogger });
}
