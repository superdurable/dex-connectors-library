// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  mustTriggerBindingDefinition, newTrigger,
  type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerDefinition, type TriggerLogger, type TriggerRunner, type TriggerTarget,
} from '@superdurable/connectors-sdk';
import { newBindingPoller, newBindingTriggerSource, newMemoryCursorStore, newRunner, type CursorStore, type PollSchedule, type Poller, type Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { ObjectNotFoundError, decodeTriggerConfiguration, newDurableTriggerTarget, type LoadedProject, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { ReamazeClient } from './client.js';
import { ConversationCreatedEventSchema, ConversationCreatedTriggerConfigurationSchema, type ConversationCreatedEvent, type ConversationCreatedTriggerConfiguration } from './conversation_created_cursor.js';
import { ConversationCreatedFetcher } from './conversation_created_fetcher.js';
import type { Connection } from './generated/connection.js';
import { ConnectorID } from './generated/definitions.js';

/** Manifest definition of conversationCreated. */
export const ConversationCreatedTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: 'conversationCreated' },
  description: 'Receive each conversation created in the brand, or in one channel, read by polling GET /conversations with a durable creation-time cursor; the first run records a baseline and delivers no existing conversations.',
};
/** The configuration UI unit that enters the watched channel slug, Go's UIUnitChannelSlugEntry. */
export const UIUnitChannelSlugEntry = 'channelSlugEntry';
/** The channelSlugEntry port that outputs the slug, Go's UIChannelSlugEntryPortChannel. */
export const UIChannelSlugEntryPortChannel = 'channel';

/** One static application binding of conversationCreated. */
export interface ConversationCreatedTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Connector Studio units, usually the channelSlugEntry unit bound to /channel. */ configurationUI?: ConnectorConfigurationUI;
}

/** Declare a static conversationCreated binding for Flow Definition tooling and Dex Web. */
export function defineConversationCreatedTriggerBinding(config: ConversationCreatedTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: ConversationCreatedTriggerDefinition, ...config });
}

/** Binds one conversationCreated configuration to its application target and cursor. */
export interface ConversationCreatedTriggerRoute {
  /** The stable application binding name, unique within the runner. */ bindingName: string;
  /** Selects the watched channel; omitted or blank watches every channel of the brand. */ configuration?: ConversationCreatedTriggerConfiguration;
  /**
   * Routes each new conversation into the application. A target from newDurableTriggerTarget records each one
   * before the cursor commits past it; any other target consumes it first.
   */
  target: TriggerTarget<ConversationCreatedEvent>;
  /**
   * Stores the binding's creation-time cursor, such as LoadedProject.pollCursorStore. Omitted keeps it in
   * memory, so every process start records a new baseline and conversations created while stopped are lost.
   */
  cursor?: CursorStore;
}

/** Options of the conversationCreated runners. */
export interface ConversationTriggerRunnerOptions {
  /** Receives seed, reset, poll failure, rate-limit, backlog, and inbox records, never payloads or credentials; omitted uses defaultTriggerLogger. */ logger?: TriggerLogger;
}

/**
 * Create one poll Runner for the connection's conversationCreated bindings. Run replays each durable target,
 * records a baseline for a binding without a cursor, then polls every binding each pollInterval with jitter.
 * A cursor for another brand or channel records a new baseline and logs a WARN record. A 429 pauses every
 * binding of the connection for Retry-After, or for one minute, Re:amaze's documented limit window, without
 * it; other failures back off per binding up to 15 minutes. Delivery is at least once, and each conversation
 * keeps one Trigger event ID across refetches. Throws for a connection without a ReamazeClient, no routes, a
 * blank binding name, a missing target, or an invalid configuration.
 */
export function newConversationTriggerRunner(connection: Connection, routes: readonly ConversationCreatedTriggerRoute[], options: ConversationTriggerRunnerOptions = {}): Runner {
  const client = reamazeClientOf(connection);
  if (!routes?.length) throw new TypeError('Re:amaze conversationCreated routes are required');
  const trigger = ConversationCreatedTriggerDefinition.trigger;
  const pollers: Poller[] = routes.map(route => {
    if (!route?.bindingName?.trim() || !route.target) throw new TypeError('Re:amaze conversationCreated binding name and target are required');
    const configuration = parseConfiguration(route.configuration ?? {});
    return newBindingPoller<ConversationCreatedEvent>({
      definition: ConversationCreatedTriggerDefinition,
      binding: { connection: connection.reference, trigger, name: route.bindingName },
      cursor: route.cursor ?? newMemoryCursorStore(),
      fetcher: new ConversationCreatedFetcher(client, connection.reference, trigger, route.bindingName, configuration, options.logger),
      target: route.target,
    });
  });
  return newRunner({ connection: connection.reference, schedule: pollSchedule(client), pollers, ...(options.logger ? { logger: options.logger } : {}) });
}

/** Selects one conversationCreated binding saved in the project configuration. */
export interface ProjectConversationCreatedTriggerRoute {
  /** Names the saved binding. */ bindingName: string;
  /** Routes each new conversation into the application, usually newDexFlowTriggerTarget. */ target: TriggerTarget<ConversationCreatedEvent>;
}

/**
 * Create a durable Runner for conversationCreated bindings saved in the loaded project. connection is the one
 * newProjectConnection opened from project. Each route loads its binding's saved channel (an unsaved binding
 * watches every channel) and records new conversations in the binding's project Trigger inbox before its
 * cursor commits in the binding's project poll cursor, so a restarted or replaced replica neither misses nor
 * repeats a conversation created within ten minutes of the newest one seen. Cursor tokens match Go's byte for
 * byte and inbox payloads use the Go connector's JSON fields, so Go and TypeScript replicas share a binding.
 */
export function newProjectConversationTriggerRunner(project: LoadedProject, connection: Connection, routes: readonly ProjectConversationCreatedTriggerRoute[], options: ConversationTriggerRunnerOptions = {}): Runner {
  if (!project) throw new TypeError('Re:amaze conversationCreated runner requires the loaded project configuration');
  const durableRoutes = (routes ?? []).map((route): ConversationCreatedTriggerRoute => {
    if (!route?.target) throw new TypeError(`Re:amaze conversationCreated binding ${JSON.stringify(route?.bindingName)} target is required`);
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName: connection.reference.name, triggerName: ConversationCreatedTriggerDefinition.trigger.triggerName, bindingName: route.bindingName };
    let configuration: ConversationCreatedTriggerConfiguration = {};
    try { configuration = decodeTriggerConfiguration(project.configuration, key, ConversationCreatedTriggerConfigurationSchema); }
    catch (error) {
      // An unsaved binding watches every channel, as a blank entry does.
      if (!(error instanceof ObjectNotFoundError)) throw new TypeError(`reamaze conversationCreated binding ${JSON.stringify(route.bindingName)} configuration: ${error instanceof Error ? error.message : String(error)}`);
    }
    const target = newDurableTriggerTarget(project.triggerInbox(key), key, route.target, { decodePayload: decodeConversationCreatedEventPayload, ...(options.logger ? { logger: options.logger } : {}) });
    return { bindingName: route.bindingName, configuration, target, cursor: project.pollCursorStore(key) };
  });
  return newConversationTriggerRunner(connection, durableRoutes, options);
}

/** Configuration of one in-memory conversationCreated Trigger. */
export interface ConversationCreatedTriggerConfig {
  /** Re:amaze connection from newProjectConnection or newConnection with a ReamazeClient. */ connection: Connection;
  /** Selects the watched channel; omitted watches every channel of the brand. */ configuration?: ConversationCreatedTriggerConfiguration;
  /** Routes new conversations into the application. */ target: TriggerTarget<ConversationCreatedEvent>;
  /** Receives the Trigger's records; omitted uses defaultTriggerLogger. */ logger?: TriggerLogger;
}

/**
 * Create a single conversationCreated TriggerRunner, as Go's generated NewConversationCreatedTrigger does; its
 * binding is named after the Trigger. It has no project storage, so it keeps the cursor in memory and records
 * a new baseline on every start. Use newProjectConversationTriggerRunner for durable delivery.
 */
export function newConversationCreatedTrigger(config: ConversationCreatedTriggerConfig): TriggerRunner {
  const client = reamazeClientOf(config.connection);
  if (!config.target) throw new TypeError('Re:amaze conversationCreated target is required');
  const configuration = parseConfiguration(config.configuration ?? {});
  const trigger = ConversationCreatedTriggerDefinition.trigger;
  const binding = { connection: config.connection.reference, trigger, name: trigger.triggerName };
  const source = newBindingTriggerSource<ConversationCreatedEvent>({
    definition: ConversationCreatedTriggerDefinition, binding, cursor: newMemoryCursorStore(),
    fetcher: new ConversationCreatedFetcher(client, config.connection.reference, trigger, trigger.triggerName, configuration, config.logger),
  }, pollSchedule(client), config.logger);
  return newTrigger({ definition: ConversationCreatedTriggerDefinition, binding, source, target: config.target, ...(config.logger ? { logger: config.logger } : {}) });
}

/** Decode a replayed inbox payload, written by a Go or TypeScript replica; throws for another payload. */
export function decodeConversationCreatedEventPayload(payloadJSON: string): ConversationCreatedEvent {
  let value: unknown;
  try { value = JSON.parse(payloadJSON); } catch { throw new TypeError('stored Re:amaze conversationCreated payload is invalid'); }
  const parsed = ConversationCreatedEventSchema.safeParse(value);
  if (!parsed.success) throw new TypeError('stored Re:amaze conversationCreated payload is invalid');
  return parsed.data;
}

function parseConfiguration(configuration: ConversationCreatedTriggerConfiguration): ConversationCreatedTriggerConfiguration {
  const parsed = ConversationCreatedTriggerConfigurationSchema.safeParse(configuration);
  if (!parsed.success) throw new TypeError(`reamaze conversationCreated configuration: ${parsed.error.issues.map(issue => issue.message).join('; ')}`);
  return parsed.data;
}

function reamazeClientOf(connection: Connection): ReamazeClient {
  if (!(connection?.operations instanceof ReamazeClient)) throw new TypeError('Re:amaze conversationCreated Trigger requires a connection from newProjectConnection or newConnection with a ReamazeClient');
  return connection.operations;
}

// Omitted maxBackoffMs uses the kit's cap, which grows with a long poll interval.
function pollSchedule(client: ReamazeClient): PollSchedule { return { intervalMs: client.pollIntervalMs }; }
