// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  mustTriggerBindingDefinition, newTrigger,
  type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerDefinition, type TriggerLogger, type TriggerRunner, type TriggerTarget,
} from '@superdurable/connectors-sdk';
import { newBindingPoller, newBindingTriggerSource, newMemoryCursorStore, newRunner, type CursorStore, type PollSchedule, type Poller, type RequestLimiter, type Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { decodeTriggerConfiguration, newDurableTriggerTarget, type LoadedProject, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { HiverClient } from './client.js';
import {
  ConversationReceivedEventSchema, ConversationReceivedTriggerConfigurationSchema,
  type ConversationReceivedEvent, type ConversationReceivedTriggerConfiguration,
} from './conversation_received_cursor.js';
import { ConversationReceivedFetcher, ConversationReceivedMaxBackoffMs, ConversationSnapshotOptions, trimGoSpace } from './conversation_received_fetcher.js';
import { requireHiverID } from './decode.js';
import type { Connection } from './generated/connection.js';
import { ConnectorID } from './generated/definitions.js';

/** Manifest definition of conversationReceived. */
export const ConversationReceivedTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: 'conversationReceived' },
  description: "Receive each conversation that newly appears in one picked Hiver shared inbox, found by polling the inbox's conversation list; conversations already in the inbox when the binding first polls are never delivered.",
};

/** The release-owned Studio unit that lists the account's shared inboxes and stores one inbox ID. */
export const UIUnitInboxPicker = 'inboxPicker';
/** The inboxPicker output port holding the chosen Hiver inbox ID; bind it to /inboxId. */
export const UIInboxPickerPortInboxID = 'inboxId';

/** One static application binding of conversationReceived. */
export interface ConversationReceivedTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Connector Studio units, normally the inboxPicker unit bound to /inboxId. */ configurationUI?: ConnectorConfigurationUI;
}

/** Declare a static conversationReceived binding for Flow Definition tooling and Dex Web. */
export function defineConversationReceivedTriggerBinding(config: ConversationReceivedTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: ConversationReceivedTriggerDefinition, ...config });
}

/** Check that the binding names one Hiver shared inbox ID; throws otherwise. */
export function validateConversationReceivedTriggerConfiguration(configuration: ConversationReceivedTriggerConfiguration): void {
  try { requireHiverID('inboxId', trimGoSpace(configuration?.inboxId ?? '')); } catch (error) { throw new TypeError(`Hiver conversationReceived ${error instanceof Error ? error.message : String(error)}`); }
}

/** Binds one conversationReceived binding's configuration and cursor to its application target. */
export interface ConversationReceivedTriggerRoute {
  /** Names the configured Trigger binding; it is unique within one runner. */ bindingName: string;
  /** Selects the polled shared inbox. */ configuration: ConversationReceivedTriggerConfiguration;
  /**
   * Persists the tracked conversation IDs and sweep position, normally LoadedProject.pollCursorStore. Use
   * newMemoryCursorStore only when conversations that arrive while the process is stopped may be skipped.
   */
  cursor: CursorStore;
  /**
   * Receives each new conversation. A target from newDurableTriggerTarget records the conversation before the
   * cursor moves past it; any other target consumes it first.
   */
  target: TriggerTarget<ConversationReceivedEvent>;
}

/** Options of the conversationReceived runners. */
export interface ConversationReceivedTriggerRunnerOptions {
  /** Optionally gates every poll request in addition to the connection's request spacing. */ requestLimiter?: RequestLimiter;
  /** Receives poll records, never payloads, page tokens, or credentials; omitted uses defaultTriggerLogger. */ logger?: TriggerLogger;
}

/**
 * Validate every route and build one poll Runner for the connection's conversationReceived bindings. It makes
 * no provider request and reads no storage. Run replays each durable target and starts missing baselines: a
 * binding's first polls walk the inbox's whole conversation list, 10 pages of 100 at a time, and record every
 * conversation ID without delivering it. Afterwards each poll, every pollIntervalMs with jitter, reads the
 * list's first page plus up to pollPagesPerPoll-1 pages of a sweep that walks the rest of the list across
 * polls, and delivers each untracked ID in list order before the cursor commits. IDs that two complete sweeps
 * in a row never list stop being tracked. A rejected page token (HTTP 400, 404, 410, or 422) restarts the
 * walk without a gap. HTTP 429 pauses every binding of the connection for at least its Retry-After; other
 * failures back off up to 30 minutes, or pollIntervalMs when longer. Delivery is at least once, and each event
 * ID, conversation-<inboxId>-<conversationId>, is a pure function of the conversation. An inbox listing more
 * than MaxTrackedConversations pauses its binding. Throws for a connection without a HiverClient, no routes, a
 * blank binding name, a missing cursor or target, or an invalid configuration.
 */
export function newConversationReceivedTriggerRunner(connection: Connection, routes: readonly ConversationReceivedTriggerRoute[], options: ConversationReceivedTriggerRunnerOptions = {}): Runner {
  const client = hiverClientOf(connection);
  if (!routes?.length) throw new TypeError('Hiver conversationReceived Trigger routes are required');
  const pollers: Poller[] = routes.map(route => {
    if (!route?.bindingName?.trim() || !route.target || !route.cursor) throw new TypeError('Hiver conversationReceived binding name, cursor, and target are required');
    validateRouteConfiguration(route.bindingName, route.configuration);
    const binding = { connection: connection.reference, trigger: ConversationReceivedTriggerDefinition.trigger, name: route.bindingName };
    return newBindingPoller<ConversationReceivedEvent>({
      definition: ConversationReceivedTriggerDefinition, binding, cursor: route.cursor,
      fetcher: new ConversationReceivedFetcher(client, binding, route.configuration, options.logger), target: route.target, snapshot: ConversationSnapshotOptions,
    });
  });
  return newRunner({
    connection: connection.reference, schedule: pollSchedule(client), pollers,
    ...(options.requestLimiter ? { requestLimiter: options.requestLimiter } : {}), ...(options.logger ? { logger: options.logger } : {}),
  });
}

/** Selects one conversationReceived binding saved in the project configuration. */
export interface ProjectConversationReceivedTriggerRoute {
  /** Names the binding whose shared inbox Dex Web saved. */ bindingName: string;
  /** Receives each new conversation after the binding's durable project inbox records it. */ target: TriggerTarget<ConversationReceivedEvent>;
}

/**
 * Create a durable Runner for conversationReceived bindings saved in the loaded project. connection is the one
 * newProjectConnection opened from project. Each route loads its binding's saved shared inbox, keeps its cursor
 * in project storage beside its Trigger inbox, and records every conversation in that inbox before the cursor
 * commits past it, so a restarted or replaced replica neither misses nor replays a conversation. Inbox payloads
 * are the Go connector's JSON, so Go and TypeScript replicas share them. Throws for an unsaved or invalid
 * binding and as newConversationReceivedTriggerRunner does.
 */
export function newProjectConversationReceivedTriggerRunner(project: LoadedProject, connection: Connection, routes: readonly ProjectConversationReceivedTriggerRoute[], options: ConversationReceivedTriggerRunnerOptions = {}): Runner {
  if (!project) throw new TypeError('Hiver conversationReceived runner requires the loaded project configuration');
  const durableRoutes = (routes ?? []).map((route): ConversationReceivedTriggerRoute => {
    if (!route?.target) throw new TypeError(`Hiver conversationReceived binding ${JSON.stringify(route?.bindingName)} target is required`);
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName: connection.reference.name, triggerName: ConversationReceivedTriggerDefinition.trigger.triggerName, bindingName: route.bindingName };
    let configuration: ConversationReceivedTriggerConfiguration;
    try { configuration = decodeTriggerConfiguration(project.configuration, key, ConversationReceivedTriggerConfigurationSchema); }
    catch (error) { throw new TypeError(`Hiver conversationReceived binding ${JSON.stringify(route.bindingName)} configuration: ${error instanceof Error ? error.message : String(error)}`); }
    const target = newDurableTriggerTarget(project.triggerInbox(key), key, route.target, { decodePayload: decodeConversationReceivedEventPayload, ...(options.logger ? { logger: options.logger } : {}) });
    return { bindingName: route.bindingName, configuration, cursor: project.pollCursorStore(key), target };
  });
  return newConversationReceivedTriggerRunner(connection, durableRoutes, options);
}

/** Configuration of one in-memory conversationReceived Trigger. */
export interface ConversationReceivedTriggerConfig {
  /** Hiver connection from newProjectConnection or newConnection with a HiverClient. */ connection: Connection;
  /** Static binding name. */ bindingName: string;
  /** Selects the polled shared inbox. */ configuration: ConversationReceivedTriggerConfiguration;
  /** Routes each new conversation into the application. */ target: TriggerTarget<ConversationReceivedEvent>;
  /** Receives the Trigger's records; omitted uses defaultTriggerLogger. */ logger?: TriggerLogger;
}

/**
 * Create a single conversationReceived TriggerRunner, as Go's generated NewConversationReceivedTrigger does. It
 * has no project storage, so it keeps the cursor in memory and records a new baseline on every start;
 * conversations that arrive while stopped are never delivered. Use newProjectConversationReceivedTriggerRunner
 * for durable delivery.
 */
export function newConversationReceivedTrigger(config: ConversationReceivedTriggerConfig): TriggerRunner {
  const client = hiverClientOf(config.connection);
  if (!config.bindingName?.trim() || !config.target) throw new TypeError('Hiver conversationReceived binding name and target are required');
  validateRouteConfiguration(config.bindingName, config.configuration);
  const binding = { connection: config.connection.reference, trigger: ConversationReceivedTriggerDefinition.trigger, name: config.bindingName };
  const source = newBindingTriggerSource<ConversationReceivedEvent>({
    definition: ConversationReceivedTriggerDefinition, binding, cursor: newMemoryCursorStore(),
    fetcher: new ConversationReceivedFetcher(client, binding, config.configuration, config.logger), snapshot: ConversationSnapshotOptions,
  }, pollSchedule(client), config.logger);
  return newTrigger({ definition: ConversationReceivedTriggerDefinition, binding, source, target: config.target, ...(config.logger ? { logger: config.logger } : {}) });
}

/** Decode a replayed inbox payload, written by a Go or TypeScript replica; throws for another payload. */
export function decodeConversationReceivedEventPayload(payloadJSON: string): ConversationReceivedEvent {
  const parsed = ConversationReceivedEventSchema.safeParse(JSON.parse(payloadJSON));
  if (!parsed.success || !parsed.data.conversation.id) throw new TypeError('stored Hiver conversationReceived payload is invalid');
  return parsed.data;
}

function validateRouteConfiguration(bindingName: string, configuration: ConversationReceivedTriggerConfiguration | undefined): void {
  if (!configuration) throw new TypeError(`Hiver conversationReceived binding ${JSON.stringify(bindingName)} configuration is required`);
  try { validateConversationReceivedTriggerConfiguration(configuration); }
  catch (error) { throw new TypeError(`Hiver conversationReceived binding ${JSON.stringify(bindingName)}: ${error instanceof Error ? error.message : String(error)}`); }
}

function hiverClientOf(connection: Connection): HiverClient {
  if (!(connection?.operations instanceof HiverClient)) throw new TypeError('Hiver conversationReceived Trigger requires a connection from newProjectConnection or newConnection with a HiverClient');
  return connection.operations;
}

/** Exported for tests: the schedule every conversationReceived runner of client uses. */
export function conversationReceivedPollSchedule(client: HiverClient): PollSchedule {
  return { intervalMs: client.pollIntervalMs, maxBackoffMs: Math.max(ConversationReceivedMaxBackoffMs, client.pollIntervalMs) };
}

const pollSchedule = conversationReceivedPollSchedule;
