// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  mustTriggerBindingDefinition, newTrigger,
  type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerDefinition, type TriggerLogger, type TriggerRunner, type TriggerTarget,
} from '@superdurable/connectors-sdk';
import { newBindingPoller, newBindingTriggerSource, newMemoryCursorStore, newRunner, type CursorStore, type PollSchedule, type Poller, type Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { ObjectNotFoundError, decodeTriggerConfiguration, newDurableTriggerTarget, type LoadedProject, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { MondayClient } from './client.js';
import { ConnectorID } from './generated/definitions.js';
import type { Connection } from './generated/connection.js';
import { ItemChangeFetcher, ItemChangedSnapshotOptions } from './item_changed_fetcher.js';
import { ItemChangedEventSchema, ItemChangedTriggerConfigurationSchema, type ItemChangedEvent, type ItemChangedTriggerConfiguration } from './item_snapshot.js';

/** Manifest definition of itemChanged. */
export const ItemChangedTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: 'itemChanged' },
  description: "Receive each active item created or changed on one monday.com board, read by polling the board's items by monday.com's last-updated time; the first poll records a baseline and delivers no existing items.",
};

/** One static application binding of itemChanged. */
export interface ItemChangedTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Connector Studio units, usually the boardIdInput unit bound to /boardId. */ configurationUI?: ConnectorConfigurationUI;
}

/** Declare a static itemChanged binding for Flow Definition tooling and Dex Web. */
export function defineItemChangedTriggerBinding(config: ItemChangedTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: ItemChangedTriggerDefinition, ...config });
}

/** Binds one itemChanged configuration to its application target and cursor. */
export interface ItemChangedTriggerRoute {
  /** The stable application binding name, unique within the runner. */ bindingName: string;
  /** Selects the watched board. */ configuration: ItemChangedTriggerConfiguration;
  /**
   * Routes each change into the application. A target from newDurableTriggerTarget records each change before
   * the cursor commits past it; any other target consumes it first.
   */
  target: TriggerTarget<ItemChangedEvent>;
  /**
   * Stores the binding's position and snapshot, such as LoadedProject.pollCursorStore. Omitted keeps it in
   * memory, so every process start records a new baseline and changes made while stopped are lost.
   */
  cursor?: CursorStore;
}

/** Options of the itemChanged runners. */
export interface ItemTriggerRunnerOptions {
  /** Receives seed, reseed, poll failure, rate-limit, and inbox records, never payloads or credentials; omitted uses defaultTriggerLogger. */ logger?: TriggerLogger;
}

/**
 * Create one poll Runner for the connection's itemChanged bindings. Run replays each durable target, records a
 * baseline for a binding without a cursor, then polls every binding each pollInterval with jitter. A rate-limit
 * error pauses every binding of the connection for monday.com's Retry-After or retry_in_seconds, and
 * DAILY_LIMIT_EXCEEDED pauses it until midnight UTC; other failures back off per binding. More changes than one
 * 10,000-item scan can read record a new baseline and log a WARN record. Delivery is at least once, and each
 * item version keeps one Trigger event ID. Throws for a connection without a MondayClient, no routes, a blank
 * binding name, a missing target, or an invalid configuration.
 */
export function newItemTriggerRunner(connection: Connection, routes: readonly ItemChangedTriggerRoute[], options: ItemTriggerRunnerOptions = {}): Runner {
  const client = mondayClientOf(connection);
  if (!routes?.length) throw new TypeError('monday.com itemChanged routes are required');
  const pollers: Poller[] = routes.map(route => {
    if (!route?.bindingName?.trim() || !route.target) throw new TypeError('monday.com itemChanged binding name and target are required');
    const configuration = parseConfiguration(route.configuration);
    return newBindingPoller<ItemChangedEvent>({
      definition: ItemChangedTriggerDefinition,
      binding: { connection: connection.reference, trigger: ItemChangedTriggerDefinition.trigger, name: route.bindingName },
      cursor: route.cursor ?? newMemoryCursorStore(),
      fetcher: new ItemChangeFetcher(client, connection.reference, ItemChangedTriggerDefinition.trigger, configuration),
      target: route.target, snapshot: ItemChangedSnapshotOptions,
    });
  });
  return newRunner({ connection: connection.reference, schedule: pollSchedule(client), pollers, ...(options.logger ? { logger: options.logger } : {}) });
}

/** Selects one itemChanged binding saved in the project configuration. */
export interface ProjectItemChangedTriggerRoute {
  /** Names the saved binding. */ bindingName: string;
  /** Routes each change into the application, usually newDexFlowTriggerTarget. */ target: TriggerTarget<ItemChangedEvent>;
}

/**
 * Create a durable Runner for itemChanged bindings saved in the loaded project. connection is the one
 * newProjectConnection opened from project. Each route loads its binding's saved board and records changes in
 * the binding's project Trigger inbox before its position commits in the binding's project poll cursor, so a
 * restarted or replaced replica neither misses nor repeats a change. Inbox payloads use the Go connector's JSON
 * fields, so Go and TypeScript replicas share them. Throws for a binding without a saved board, an invalid
 * saved configuration, and as newItemTriggerRunner does.
 */
export function newProjectItemTriggerRunner(project: LoadedProject, connection: Connection, routes: readonly ProjectItemChangedTriggerRoute[], options: ItemTriggerRunnerOptions = {}): Runner {
  if (!project) throw new TypeError('monday.com itemChanged runner requires the loaded project configuration');
  const durableRoutes = (routes ?? []).map((route): ItemChangedTriggerRoute => {
    const name = JSON.stringify(route?.bindingName);
    if (!route?.target) throw new TypeError(`monday.com itemChanged binding ${name} target is required`);
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName: connection.reference.name, triggerName: ItemChangedTriggerDefinition.trigger.triggerName, bindingName: route.bindingName };
    let configuration: ItemChangedTriggerConfiguration;
    try { configuration = decodeTriggerConfiguration(project.configuration, key, ItemChangedTriggerConfigurationSchema); }
    catch (error) {
      if (error instanceof ObjectNotFoundError) throw new TypeError(`monday itemChanged binding ${name} has no saved board; enter its board ID in Dex Web and restart`);
      throw new TypeError(`monday itemChanged binding ${name} configuration: ${error instanceof Error ? error.message : String(error)}`);
    }
    const target = newDurableTriggerTarget(project.triggerInbox(key), key, route.target, { decodePayload: decodeItemChangedEventPayload, ...(options.logger ? { logger: options.logger } : {}) });
    return { bindingName: route.bindingName, configuration, target, cursor: project.pollCursorStore(key) };
  });
  return newItemTriggerRunner(connection, durableRoutes, options);
}

/** Configuration of one in-memory itemChanged Trigger. */
export interface ItemChangedTriggerConfig {
  /** monday.com connection from newProjectConnection or newConnection with a MondayClient. */ connection: Connection;
  /** Static binding name. */ bindingName: string;
  /** Selects the watched board. */ configuration: ItemChangedTriggerConfiguration;
  /** Routes each change into the application. */ target: TriggerTarget<ItemChangedEvent>;
  /** Receives the Trigger's records; omitted uses defaultTriggerLogger. */ logger?: TriggerLogger;
}

/**
 * Create a single itemChanged TriggerRunner, as Go's generated NewItemChangedTrigger does. It has no project
 * storage, so it keeps the cursor in memory and records a new baseline on every start; changes made while
 * stopped are lost. Use newProjectItemTriggerRunner for durable delivery. Throws as newItemTriggerRunner does.
 */
export function newItemChangedTrigger(config: ItemChangedTriggerConfig): TriggerRunner {
  const client = mondayClientOf(config.connection);
  if (!config.bindingName?.trim() || !config.target) throw new TypeError('monday.com itemChanged binding name and target are required');
  const configuration = parseConfiguration(config.configuration);
  const binding = { connection: config.connection.reference, trigger: ItemChangedTriggerDefinition.trigger, name: config.bindingName };
  const source = newBindingTriggerSource<ItemChangedEvent>({
    definition: ItemChangedTriggerDefinition, binding, cursor: newMemoryCursorStore(),
    fetcher: new ItemChangeFetcher(client, config.connection.reference, ItemChangedTriggerDefinition.trigger, configuration), snapshot: ItemChangedSnapshotOptions,
  }, pollSchedule(client), config.logger);
  return newTrigger({ definition: ItemChangedTriggerDefinition, binding, source, target: config.target, ...(config.logger ? { logger: config.logger } : {}) });
}

/** Decode a replayed inbox payload, written by a Go or TypeScript replica; throws for another payload. */
export function decodeItemChangedEventPayload(payloadJSON: string): ItemChangedEvent {
  const parsed = ItemChangedEventSchema.safeParse(JSON.parse(payloadJSON));
  if (!parsed.success) throw new TypeError('stored monday.com itemChanged payload is invalid');
  return parsed.data;
}

function parseConfiguration(configuration: ItemChangedTriggerConfiguration | undefined): ItemChangedTriggerConfiguration {
  const parsed = ItemChangedTriggerConfigurationSchema.safeParse(configuration ?? {});
  if (!parsed.success) throw new TypeError(`monday itemChanged configuration: ${parsed.error.issues.map(issue => issue.message).join('; ')}`);
  return parsed.data;
}

function mondayClientOf(connection: Connection): MondayClient {
  if (!(connection?.operations instanceof MondayClient)) throw new TypeError('monday.com itemChanged Trigger requires a connection from newProjectConnection or newConnection with a MondayClient');
  return connection.operations;
}

// Omitted maxBackoffMs uses the kit's cap, which grows with a long poll interval.
function pollSchedule(client: MondayClient): PollSchedule { return { intervalMs: client.pollIntervalMs }; }
