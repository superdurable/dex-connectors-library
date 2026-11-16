// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newTrigger, type TriggerContext, type TriggerLogger, type TriggerRunner, type TriggerTarget } from '@superdurable/connectors-sdk';
import { newBindingPoller, newBindingTriggerSource, newMemoryCursorStore, newRunner, type CursorStore, type PollSchedule, type Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { ObjectNotFoundError, decodeTriggerConfiguration, newDurableTriggerTarget, parseTriggerPayloadJSON, type LoadedProject, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { ConfluenceClient, type ConfluenceClientOptions } from './client.js';
import type { Connection } from './generated/connection.js';
import { ConnectorID } from './generated/definitions.js';
import { PageChangeFetcher, pageChangeSnapshotOptions } from './page_change_fetcher.js';
import { PageChangedEventSchema, PageChangedTriggerConfigurationSchema, PageChangedTriggerDefinition, type PageChangedEvent, type PageChangedTriggerConfiguration } from './page_changed.js';
import { newProjectConnection } from './project.js';

/** Binds one pageChanged configuration to its application target and cursor. */
export interface PageChangedTriggerRoute {
  /** Stable application binding name, unique within the runner. */ bindingName: string;
  /** Selects the watched space; blank watches every space the connection can view. */ configuration: PageChangedTriggerConfiguration;
  /**
   * Routes each change into the application. A target from newDurableTriggerTarget records each change before
   * the cursor commits past it; any other target consumes it first.
   */
  target: TriggerTarget<PageChangedEvent>;
  /**
   * Stores the binding's position, such as LoadedProject.pollCursorStore. Omitted keeps it in memory, so every
   * process start records a new baseline and changes made while stopped are lost.
   */
  cursor?: CursorStore | undefined;
}

/** Configures every Confluence Trigger binding polled through one connection. */
export interface PageTriggerRunnerConfig {
  /** A connection from newProjectConnection or newConnection with a ConfluenceClient. */ connection: Connection;
  /** The pageChanged bindings; at least one is required. */ pageChangedRoutes: readonly PageChangedTriggerRoute[];
}

/**
 * Create one poll runner for the configured pageChanged bindings. run replays each durable target, records a
 * baseline for a binding without a cursor, then polls every binding each pollInterval with jitter. Each poll
 * reads CQL search, newest modification first, back to ten minutes before the previous scan began, and delivers
 * each page version not delivered before. A 429, or a 503 with Retry-After, pauses every binding of the
 * connection for Retry-After, or backs off exponentially without it; other failures back off per binding up to
 * 15 minutes, and none of them moves the cursor. A cursor for another site or space records a new baseline and
 * logs a WARN record. Delivery is at least once, and each page version keeps one Trigger event ID across
 * refetches. Records go to the client's triggerLogger. Throws for empty routes, a blank binding name
 * or missing target, an invalid configuration, or a connection without a ConfluenceClient.
 */
export function newPageTriggerRunner(config: PageTriggerRunnerConfig): Runner {
  const client = confluenceClientOf(config.connection);
  if (!config.pageChangedRoutes?.length) throw new TypeError('Confluence pageChanged routes are required');
  const pollers = config.pageChangedRoutes.map(route => {
    if (!route.bindingName?.trim() || !route.target) throw new TypeError('Confluence pageChanged binding name and target are required');
    const configuration = parseConfiguration(route.configuration);
    return newBindingPoller<PageChangedEvent>({
      definition: PageChangedTriggerDefinition,
      binding: { connection: config.connection.reference, trigger: PageChangedTriggerDefinition.trigger, name: route.bindingName },
      cursor: route.cursor ?? newMemoryCursorStore(),
      fetcher: new PageChangeFetcher(client, config.connection.reference, configuration),
      target: route.target,
      snapshot: pageChangeSnapshotOptions,
    });
  });
  return newRunner({ connection: config.connection.reference, schedule: pollSchedule(client), pollers, logger: client.triggerLogger });
}

/** Configuration of one pageChanged Trigger without project storage. */
export interface PageChangedTriggerConfig {
  /** Confluence connection from newProjectConnection or newConnection with a ConfluenceClient. */ connection: Connection;
  /** Static binding name. */ bindingName: string;
  /** The watched space. */ configuration: PageChangedTriggerConfiguration;
  /** Routes changes into the application. */ target: TriggerTarget<PageChangedEvent>;
}

/**
 * Create one pageChanged binding as a TriggerRunner polling on its own schedule. It has no project storage,
 * so it keeps the cursor in memory and records a new baseline on every start; use
 * newProjectPageTriggerRunner for a durable cursor.
 */
export function newPageChangedTrigger(config: PageChangedTriggerConfig): TriggerRunner {
  const client = confluenceClientOf(config.connection);
  const configuration = parseConfiguration(config.configuration);
  const binding = { connection: config.connection.reference, trigger: PageChangedTriggerDefinition.trigger, name: config.bindingName };
  const source = newBindingTriggerSource<PageChangedEvent>({
    definition: PageChangedTriggerDefinition, binding, cursor: newMemoryCursorStore(), fetcher: new PageChangeFetcher(client, config.connection.reference, configuration), snapshot: pageChangeSnapshotOptions,
  }, pollSchedule(client), client.triggerLogger);
  return newTrigger({ definition: PageChangedTriggerDefinition, binding, source, target: config.target, ...(client.triggerLogger ? { logger: client.triggerLogger } : {}) });
}

/** Selects one pageChanged binding saved in the project configuration. */
export interface ProjectPageChangedTriggerRoute {
  /** The saved binding name. */ bindingName: string;
  /** Routes each change into the application, usually newDexFlowTriggerTarget. */ target: TriggerTarget<PageChangedEvent>;
}

/** Polls the project's pageChanged bindings. Run it for the life of the Worker process. */
export class PageTriggerRunner {
  /** @internal Use newProjectPageTriggerRunner. */
  constructor(private readonly runner: Runner) {}
  /** Replay each binding's inbox, then poll until context.signal aborts; rejects with TriggerDeliveryAbortedError then. */
  run(context: TriggerContext): Promise<void> { return this.runner.run(context); }
  /** Make the named binding poll now, without shortening a rate-limit pause. */
  wakePoller(bindingName: string): void { this.runner.wakePoller(bindingName); }
}

/**
 * Open connectionName from the loaded project, load each route's saved space (an unsaved binding watches
 * every space the connection can view), and create a durable runner: each route records changes in its
 * binding's project Trigger inbox before its position commits in the binding's project poll cursor, so a
 * restarted or replaced replica neither misses nor repeats a change. The options' triggerLogger also applies
 * to the inboxes. Throws for empty routes, a blank or repeated binding name, or an invalid saved configuration.
 */
export function newProjectPageTriggerRunner(project: LoadedProject, connectionName: string, routes: readonly ProjectPageChangedTriggerRoute[], options: ConfluenceClientOptions = {}): PageTriggerRunner {
  if (!routes?.length) throw new TypeError('Confluence pageChanged routes are required');
  const connection = newProjectConnection(project, connectionName, options);
  const logger: TriggerLogger | undefined = confluenceClientOf(connection).triggerLogger;
  const triggerName = PageChangedTriggerDefinition.trigger.triggerName;
  const bindingNames = new Set<string>();
  const durableRoutes = routes.map(route => {
    const bindingName = route.bindingName?.trim() ?? '';
    if (!bindingName || !route.target) throw new TypeError('Confluence pageChanged binding name and target are required');
    if (bindingNames.has(bindingName)) throw new TypeError(`Confluence pageChanged binding name "${bindingName}" is duplicated`);
    bindingNames.add(bindingName);
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName, triggerName, bindingName };
    let configuration: PageChangedTriggerConfiguration = {};
    try { configuration = decodeTriggerConfiguration(project.configuration, key, PageChangedTriggerConfigurationSchema); }
    catch (error) {
      if (!(error instanceof ObjectNotFoundError)) throw new Error(`confluence pageChanged binding "${bindingName}" configuration: ${error instanceof Error ? error.message : String(error)}`, { cause: error });
    }
    const target = newDurableTriggerTarget(project.triggerInbox(key), key, route.target, { ...(logger ? { logger } : {}), decodePayload: decodePageChangedEventPayload });
    return { bindingName, configuration, target, cursor: project.pollCursorStore(key) };
  });
  return new PageTriggerRunner(newPageTriggerRunner({ connection, pageChangedRoutes: durableRoutes }));
}

/** Decode a replayed inbox payload written by this package or by the Go connector; throws for anything else. */
export function decodePageChangedEventPayload(_payloadJSON: string, payloadBytes: Uint8Array): PageChangedEvent {
  return PageChangedEventSchema.parse(parseTriggerPayloadJSON(payloadBytes));
}

function parseConfiguration(configuration: PageChangedTriggerConfiguration): PageChangedTriggerConfiguration {
  const parsed = PageChangedTriggerConfigurationSchema.safeParse(configuration ?? {});
  if (!parsed.success) throw new TypeError(`confluence pageChanged configuration: ${parsed.error.issues.map(issue => issue.message).join('; ')}`);
  return parsed.data;
}

function confluenceClientOf(connection: Connection): ConfluenceClient {
  const client: unknown = connection?.operations;
  if (!(client instanceof ConfluenceClient)) throw new TypeError('Confluence Triggers require a connection from newProjectConnection or newConnection with a ConfluenceClient');
  return client;
}

/** Zero maxBackoffMs uses the kit's cap, which grows with a long poll interval. */
function pollSchedule(client: ConfluenceClient): PollSchedule { return { intervalMs: client.pollIntervalMs }; }
