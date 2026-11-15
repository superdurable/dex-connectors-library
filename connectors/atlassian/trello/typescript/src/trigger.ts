// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newTrigger, type TriggerContext, type TriggerLogger, type TriggerRunner, type TriggerTarget } from '@superdurable/connectors-sdk';
import { newBindingPoller, newBindingTriggerSource, newMemoryCursorStore, newRunner, type CursorStore, type PollSchedule, type Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { decodeTriggerConfiguration, newDurableTriggerTarget, parseTriggerPayloadJSON, type LoadedProject, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { TrelloClient, type TrelloClientOptions } from './client.js';
import type { Connection } from './generated/connection.js';
import { ConnectorID } from './generated/definitions.js';
import { CardActionFetcher } from './card_action_fetcher.js';
import { CardChangedEventSchema, CardChangedTriggerConfigurationSchema, CardChangedTriggerDefinition, type CardChangedEvent, type CardChangedTriggerConfiguration } from './card_changed.js';
import { newProjectConnection } from './project.js';

/** Binds one cardChanged configuration to its application target and cursor. */
export interface CardChangedTriggerRoute {
  /** Stable application binding name, unique within the runner. */ bindingName: string;
  /** Selects the watched board by its 24-character ID or 8-character short link. */ configuration: CardChangedTriggerConfiguration;
  /**
   * Routes each card action into the application. A target from newDurableTriggerTarget records each action before
   * the cursor commits past it; any other target consumes it first.
   */
  target: TriggerTarget<CardChangedEvent>;
  /**
   * Stores the binding's position, such as LoadedProject.pollCursorStore. Omitted keeps it in memory, so every
   * process start records a new baseline and actions made while stopped are lost.
   */
  cursor?: CursorStore | undefined;
}

/** Configures every Trello Trigger binding polled through one connection. */
export interface CardTriggerRunnerConfig {
  /** A connection from newProjectConnection or newConnection with a TrelloClient. */ connection: Connection;
  /** The cardChanged bindings; at least one is required. */ cardChangedRoutes: readonly CardChangedTriggerRoute[];
}

/**
 * Create one poll runner for the configured cardChanged bindings. run replays each durable target, records a
 * baseline for a binding without a cursor, then polls every binding each pollInterval with jitter. One poll
 * re-reads the last minute of the board's action history, skips the action IDs it already delivered, and
 * delivers the rest oldest first, 500 per page. A backlog over 10,000 actions, or a 400 that rejects since or
 * before, records a new baseline; actions in that gap are not delivered. A 429 pauses every binding of the
 * connection for Retry-After, or one 10-second Trello rate-limit window without it; other failures back off per
 * binding up to 15 minutes. Delivery is at least once, and each action keeps one Trigger event ID across
 * refetches. Records go to the client's triggerLogger. Throws for empty routes, a blank binding name
 * or missing target, an invalid configuration, or a connection without a TrelloClient.
 */
export function newCardTriggerRunner(config: CardTriggerRunnerConfig): Runner {
  const client = trelloClientOf(config.connection);
  if (!config.cardChangedRoutes?.length) throw new TypeError('Trello cardChanged routes are required');
  const pollers = config.cardChangedRoutes.map(route => {
    if (!route.bindingName?.trim() || !route.target) throw new TypeError('Trello cardChanged binding name and target are required');
    const configuration = parseConfiguration(route.configuration);
    return newBindingPoller<CardChangedEvent>({
      definition: CardChangedTriggerDefinition,
      binding: { connection: config.connection.reference, trigger: CardChangedTriggerDefinition.trigger, name: route.bindingName },
      cursor: route.cursor ?? newMemoryCursorStore(),
      fetcher: new CardActionFetcher(client, config.connection.reference, configuration),
      target: route.target,
    });
  });
  return newRunner({ connection: config.connection.reference, schedule: pollSchedule(client), pollers, logger: client.triggerLogger });
}

/** Configuration of one cardChanged Trigger without project storage. */
export interface CardChangedTriggerConfig {
  /** Trello connection from newProjectConnection or newConnection with a TrelloClient. */ connection: Connection;
  /** Static binding name. */ bindingName: string;
  /** The watched board. */ configuration: CardChangedTriggerConfiguration;
  /** Routes card actions into the application. */ target: TriggerTarget<CardChangedEvent>;
}

/**
 * Create one cardChanged binding as a TriggerRunner polling on its own schedule. It has no project storage,
 * so it keeps the cursor in memory and records a new baseline on every start; use
 * newProjectCardTriggerRunner for a durable cursor.
 */
export function newCardChangedTrigger(config: CardChangedTriggerConfig): TriggerRunner {
  const client = trelloClientOf(config.connection);
  const configuration = parseConfiguration(config.configuration);
  const binding = { connection: config.connection.reference, trigger: CardChangedTriggerDefinition.trigger, name: config.bindingName };
  const source = newBindingTriggerSource<CardChangedEvent>({
    definition: CardChangedTriggerDefinition, binding, cursor: newMemoryCursorStore(), fetcher: new CardActionFetcher(client, config.connection.reference, configuration),
  }, pollSchedule(client), client.triggerLogger);
  return newTrigger({ definition: CardChangedTriggerDefinition, binding, source, target: config.target, ...(client.triggerLogger ? { logger: client.triggerLogger } : {}) });
}

/** Selects one cardChanged binding saved in the project configuration. */
export interface ProjectCardChangedTriggerRoute {
  /** The saved binding name. */ bindingName: string;
  /** Routes each card action into the application, usually newDexFlowTriggerTarget. */ target: TriggerTarget<CardChangedEvent>;
}

/** Polls the project's cardChanged bindings. Run it for the life of the Worker process. */
export class CardTriggerRunner {
  /** @internal Use newProjectCardTriggerRunner. */
  constructor(private readonly runner: Runner) {}
  /** Replay each binding's inbox, then poll until context.signal aborts; rejects with TriggerDeliveryAbortedError then. */
  run(context: TriggerContext): Promise<void> { return this.runner.run(context); }
  /** Make the named binding poll now, without shortening a rate-limit pause. */
  wakePoller(bindingName: string): void { this.runner.wakePoller(bindingName); }
}

/**
 * Open connectionName from the loaded project, load each route's saved board, and create a durable runner:
 * each route records card actions in its binding's project Trigger inbox before its position commits in the
 * binding's project poll cursor, so a restarted or replaced replica neither misses nor repeats an action. A
 * binding without a saved board is an error, as in the Go connector. The options' triggerLogger also applies
 * to the inboxes. Throws for empty routes, a blank or repeated binding name, or a missing or invalid saved board.
 */
export function newProjectCardTriggerRunner(project: LoadedProject, connectionName: string, routes: readonly ProjectCardChangedTriggerRoute[], options: TrelloClientOptions = {}): CardTriggerRunner {
  if (!routes?.length) throw new TypeError('Trello cardChanged routes are required');
  const connection = newProjectConnection(project, connectionName, options);
  const logger: TriggerLogger | undefined = trelloClientOf(connection).triggerLogger;
  const triggerName = CardChangedTriggerDefinition.trigger.triggerName;
  const bindingNames = new Set<string>();
  const durableRoutes = routes.map(route => {
    const bindingName = route.bindingName?.trim() ?? '';
    if (!bindingName || !route.target) throw new TypeError('Trello cardChanged binding name and target are required');
    if (bindingNames.has(bindingName)) throw new TypeError(`Trello cardChanged binding name "${bindingName}" is duplicated`);
    bindingNames.add(bindingName);
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName, triggerName, bindingName };
    let configuration: CardChangedTriggerConfiguration;
    try { configuration = decodeTriggerConfiguration(project.configuration, key, CardChangedTriggerConfigurationSchema); }
    catch (error) {
      throw new Error(`trello cardChanged binding "${bindingName}" configuration (save the watched board in Dex Web): ${error instanceof Error ? error.message : String(error)}`, { cause: error });
    }
    const target = newDurableTriggerTarget(project.triggerInbox(key), key, route.target, { ...(logger ? { logger } : {}), decodePayload: decodeCardChangedEventPayload });
    return { bindingName, configuration, target, cursor: project.pollCursorStore(key) };
  });
  return new CardTriggerRunner(newCardTriggerRunner({ connection, cardChangedRoutes: durableRoutes }));
}

/** Decode a replayed inbox payload written by this package or by the Go connector; throws for anything else. */
export function decodeCardChangedEventPayload(_payloadJSON: string, payloadBytes: Uint8Array): CardChangedEvent {
  return CardChangedEventSchema.parse(parseTriggerPayloadJSON(payloadBytes));
}

function parseConfiguration(configuration: CardChangedTriggerConfiguration): CardChangedTriggerConfiguration {
  const parsed = CardChangedTriggerConfigurationSchema.safeParse(configuration ?? {});
  if (!parsed.success) throw new TypeError(`trello cardChanged configuration: ${parsed.error.issues.map(issue => issue.message).join('; ')}`);
  return parsed.data;
}

function trelloClientOf(connection: Connection): TrelloClient {
  const client: unknown = connection?.operations;
  if (!(client instanceof TrelloClient)) throw new TypeError('Trello Triggers require a connection from newProjectConnection or newConnection with a TrelloClient');
  return client;
}

/** Zero maxBackoffMs uses the kit's cap, which grows with a long poll interval. */
function pollSchedule(client: TrelloClient): PollSchedule { return { intervalMs: client.pollIntervalMs }; }
