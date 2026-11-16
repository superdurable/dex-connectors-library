// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newTrigger, type TriggerContext, type TriggerLogger, type TriggerRunner, type TriggerTarget } from '@superdurable/connectors-sdk';
import { newBindingPoller, newBindingTriggerSource, newMemoryCursorStore, newRunner, type CursorStore, type PollSchedule, type Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { ObjectNotFoundError, decodeTriggerConfiguration, newDurableTriggerTarget, parseTriggerPayloadJSON, type LoadedProject, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { JiraClient, type JiraClientOptions } from './client.js';
import type { Connection } from './generated/connection.js';
import { ConnectorID } from './generated/definitions.js';
import { IssueChangeFetcher } from './issue_change_fetcher.js';
import { IssueChangedEventSchema, IssueChangedTriggerConfigurationSchema, IssueChangedTriggerDefinition, type IssueChangedEvent, type IssueChangedTriggerConfiguration } from './issue_changed.js';
import { newProjectConnection } from './project.js';

/** Binds one issueChanged configuration to its application target and cursor. */
export interface IssueChangedTriggerRoute {
  /** Stable application binding name, unique within the runner. */ bindingName: string;
  /** Selects the watched project; blank watches every project the account can browse. */ configuration: IssueChangedTriggerConfiguration;
  /**
   * Routes each change into the application. A target from newDurableTriggerTarget records each change before
   * the cursor commits past it; any other target consumes it first.
   */
  target: TriggerTarget<IssueChangedEvent>;
  /**
   * Stores the binding's position, such as LoadedProject.pollCursorStore. Omitted keeps it in memory, so every
   * process start records a new baseline and changes made while stopped are lost.
   */
  cursor?: CursorStore | undefined;
}

/** Configures every Jira Trigger binding polled through one connection. */
export interface IssueTriggerRunnerConfig {
  /** A connection from newProjectConnection or newConnection with a JiraClient. */ connection: Connection;
  /** The issueChanged bindings; at least one is required. */ issueChangedRoutes: readonly IssueChangedTriggerRoute[];
}

/**
 * Create one poll runner for the configured issueChanged bindings. run replays each durable target, records a
 * baseline for a binding without a cursor, then polls every binding each pollInterval with jitter. A 429,
 * including an exhausted hourly points quota, or a 503 with Retry-After pauses every binding of the connection
 * for Retry-After, or backs off exponentially without it; other failures back off per binding up to 15 minutes,
 * and none of them moves the cursor. Delivery is at least once, and each issue version keeps one Trigger event
 * ID across refetches. Records go to the client's triggerLogger. Throws for empty routes, a blank binding name
 * or missing target, an invalid configuration, or a connection without a JiraClient.
 */
export function newIssueTriggerRunner(config: IssueTriggerRunnerConfig): Runner {
  const client = jiraClientOf(config.connection);
  if (!config.issueChangedRoutes?.length) throw new TypeError('Jira issueChanged routes are required');
  const pollers = config.issueChangedRoutes.map(route => {
    if (!route.bindingName?.trim() || !route.target) throw new TypeError('Jira issueChanged binding name and target are required');
    const configuration = parseConfiguration(route.configuration);
    return newBindingPoller<IssueChangedEvent>({
      definition: IssueChangedTriggerDefinition,
      binding: { connection: config.connection.reference, trigger: IssueChangedTriggerDefinition.trigger, name: route.bindingName },
      cursor: route.cursor ?? newMemoryCursorStore(),
      fetcher: new IssueChangeFetcher(client, config.connection.reference, configuration),
      target: route.target,
    });
  });
  return newRunner({ connection: config.connection.reference, schedule: pollSchedule(client), pollers, logger: client.triggerLogger });
}

/** Configuration of one issueChanged Trigger without project storage. */
export interface IssueChangedTriggerConfig {
  /** Jira connection from newProjectConnection or newConnection with a JiraClient. */ connection: Connection;
  /** Static binding name. */ bindingName: string;
  /** The watched project. */ configuration: IssueChangedTriggerConfiguration;
  /** Routes changes into the application. */ target: TriggerTarget<IssueChangedEvent>;
}

/**
 * Create one issueChanged binding as a TriggerRunner polling on its own schedule. It has no project storage,
 * so it keeps the cursor in memory and records a new baseline on every start; use
 * newProjectIssueTriggerRunner for a durable cursor.
 */
export function newIssueChangedTrigger(config: IssueChangedTriggerConfig): TriggerRunner {
  const client = jiraClientOf(config.connection);
  const configuration = parseConfiguration(config.configuration);
  const binding = { connection: config.connection.reference, trigger: IssueChangedTriggerDefinition.trigger, name: config.bindingName };
  const source = newBindingTriggerSource<IssueChangedEvent>({
    definition: IssueChangedTriggerDefinition, binding, cursor: newMemoryCursorStore(), fetcher: new IssueChangeFetcher(client, config.connection.reference, configuration),
  }, pollSchedule(client), client.triggerLogger);
  return newTrigger({ definition: IssueChangedTriggerDefinition, binding, source, target: config.target, ...(client.triggerLogger ? { logger: client.triggerLogger } : {}) });
}

/** Selects one issueChanged binding saved in the project configuration. */
export interface ProjectIssueChangedTriggerRoute {
  /** The saved binding name. */ bindingName: string;
  /** Routes each change into the application, usually newDexFlowTriggerTarget. */ target: TriggerTarget<IssueChangedEvent>;
}

/** Polls the project's issueChanged bindings. Run it for the life of the Worker process. */
export class IssueTriggerRunner {
  /** @internal Use newProjectIssueTriggerRunner. */
  constructor(private readonly runner: Runner) {}
  /** Replay each binding's inbox, then poll until context.signal aborts; rejects with TriggerDeliveryAbortedError then. */
  run(context: TriggerContext): Promise<void> { return this.runner.run(context); }
  /** Make the named binding poll now, without shortening a rate-limit pause. */
  wakePoller(bindingName: string): void { this.runner.wakePoller(bindingName); }
}

/**
 * Open connectionName from the loaded project, load each route's saved project (an unsaved binding watches
 * every project the account can browse), and create a durable runner: each route records changes in its
 * binding's project Trigger inbox before its position commits in the binding's project poll cursor, so a
 * restarted or replaced replica neither misses nor repeats a change. The options' triggerLogger also applies
 * to the inboxes. Throws for empty routes, a blank or repeated binding name, or an invalid saved configuration.
 */
export function newProjectIssueTriggerRunner(project: LoadedProject, connectionName: string, routes: readonly ProjectIssueChangedTriggerRoute[], options: JiraClientOptions = {}): IssueTriggerRunner {
  if (!routes?.length) throw new TypeError('Jira issueChanged routes are required');
  const connection = newProjectConnection(project, connectionName, options);
  const logger: TriggerLogger | undefined = jiraClientOf(connection).triggerLogger;
  const triggerName = IssueChangedTriggerDefinition.trigger.triggerName;
  const bindingNames = new Set<string>();
  const durableRoutes = routes.map(route => {
    const bindingName = route.bindingName?.trim() ?? '';
    if (!bindingName || !route.target) throw new TypeError('Jira issueChanged binding name and target are required');
    if (bindingNames.has(bindingName)) throw new TypeError(`Jira issueChanged binding name "${bindingName}" is duplicated`);
    bindingNames.add(bindingName);
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName, triggerName, bindingName };
    let configuration: IssueChangedTriggerConfiguration = {};
    try { configuration = decodeTriggerConfiguration(project.configuration, key, IssueChangedTriggerConfigurationSchema); }
    catch (error) {
      if (!(error instanceof ObjectNotFoundError)) throw new Error(`jira issueChanged binding "${bindingName}" configuration: ${error instanceof Error ? error.message : String(error)}`, { cause: error });
    }
    const target = newDurableTriggerTarget(project.triggerInbox(key), key, route.target, { ...(logger ? { logger } : {}), decodePayload: decodeIssueChangedEventPayload });
    return { bindingName, configuration, target, cursor: project.pollCursorStore(key) };
  });
  return new IssueTriggerRunner(newIssueTriggerRunner({ connection, issueChangedRoutes: durableRoutes }));
}

/** Decode a replayed inbox payload written by this package or by the Go connector; throws for anything else. */
export function decodeIssueChangedEventPayload(_payloadJSON: string, payloadBytes: Uint8Array): IssueChangedEvent {
  return IssueChangedEventSchema.parse(parseTriggerPayloadJSON(payloadBytes));
}

function parseConfiguration(configuration: IssueChangedTriggerConfiguration): IssueChangedTriggerConfiguration {
  const parsed = IssueChangedTriggerConfigurationSchema.safeParse(configuration ?? {});
  if (!parsed.success) throw new TypeError(`jira issueChanged configuration: ${parsed.error.issues.map(issue => issue.message).join('; ')}`);
  return parsed.data;
}

function jiraClientOf(connection: Connection): JiraClient {
  const client: unknown = connection?.operations;
  if (!(client instanceof JiraClient)) throw new TypeError('Jira Triggers require a connection from newProjectConnection or newConnection with a JiraClient');
  return client;
}

/** Zero maxBackoffMs uses the kit's cap, which grows with a long poll interval. */
function pollSchedule(client: JiraClient): PollSchedule { return { intervalMs: client.pollIntervalMs }; }
