// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

/**
 * The taskChanged poll Trigger: each task added to, changed in, removed from, deleted from, or restored to one
 * Asana project, read through the Asana Events API on the Connector SDK's durable poll Trigger kit, at
 * parity with the Go connector. newProjectTaskTriggerRunner is the durable path.
 */

import {
  mustTriggerBindingDefinition, newTrigger,
  type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerDefinition, type TriggerRunner, type TriggerTarget,
} from '@superdurable/connectors-sdk';
import { newBindingPoller, newBindingTriggerSource, newMemoryCursorStore, newRunner, type CursorStore, type Poller, type PollSchedule, type Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import {
  ObjectNotFoundError, decodeTriggerConfiguration, newDurableTriggerTarget, parseTriggerPayloadJSON,
  type LoadedProject, type TriggerInboxKey,
} from '@superdurable/connectors-sdk/projectconfig';
import { Client, type ClientOptions } from './client.js';
import { newProjectConnection } from './connection.js';
import type { Connection } from './generated/connection.js';
import { ConnectorID } from './generated/definitions.js';
import { TaskEventFetcher, isAsanaGID } from './task_event_fetcher.js';
import { TaskChangedEventSchema, TaskChangedTriggerConfigurationSchema, type TaskChangedEvent, type TaskChangedTriggerConfiguration } from './trigger_models.js';

export { TaskChangeAction, TaskChangedEventSchema, TaskChangedTriggerConfigurationSchema, type EventParentReference, type TaskChangedEvent, type TaskChangedTriggerConfiguration } from './trigger_models.js';

/** The Asana project picker UI unit, which saves a workspace, project, and, outside a Trigger, section. */ export const UIUnitProjectPicker = 'projectPicker';
/** Project picker output port: the workspace gid. */ export const UIProjectPickerPortWorkspaceID = 'workspaceId';
/** Project picker output port: the project gid. */ export const UIProjectPickerPortProjectID = 'projectId';
/** Project picker output port: the project name. */ export const UIProjectPickerPortProjectName = 'projectName';

/** Manifest definition of taskChanged. */
export const TaskChangedTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: 'taskChanged' },
  description: 'Receive each task, including subtasks, added to, changed in, removed from, deleted from, or restored to one Asana project, read through the Asana Events API; the first run records a baseline and delivers no existing tasks.',
};

/** One static application binding of taskChanged. */
export interface TaskChangedTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Connector Studio configuration units for the binding, such as the projectPicker unit. */ configurationUI?: ConnectorConfigurationUI;
}

/** Declare a static taskChanged binding for Flow Definition tooling and Dex Web. */
export function defineTaskChangedTriggerBinding(config: TaskChangedTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: TaskChangedTriggerDefinition, ...config });
}

/** Throw TypeError unless projectId is an Asana gid and workspaceId is blank or a gid. */
export function validateTaskChangedTriggerConfiguration(configuration: TaskChangedTriggerConfiguration): void {
  if (!isAsanaGID(configuration.projectId ?? '')) throw new TypeError('taskChanged projectId must be an Asana project gid, digits only');
  if (configuration.workspaceId && !isAsanaGID(configuration.workspaceId)) throw new TypeError('taskChanged workspaceId must be blank or an Asana workspace gid');
}

/** Decode a replayed taskChanged payload from a project Trigger inbox written by Go, Python, or TypeScript. */
export function decodeTaskChangedEventPayload(_payloadJSON: string, payloadBytes: Uint8Array): TaskChangedEvent {
  return TaskChangedEventSchema.parse(parseTriggerPayloadJSON(payloadBytes));
}

/** Binds one taskChanged configuration to its application target and cursor. */
export interface TaskChangedTriggerRoute {
  /** The stable application binding name, unique within the runner. */ bindingName: string;
  /** Selects the watched project; its projectId is required. */ configuration: TaskChangedTriggerConfiguration;
  /** Routes each change; a target from newDurableTriggerTarget records it before the cursor commits past it, any other target consumes it first. */ target: TriggerTarget<TaskChangedEvent>;
  /** Stores the binding's sync token, such as LoadedProject.pollCursorStore; omitted keeps it in memory, so every start records a new baseline. */ cursor?: CursorStore;
}

/** Configures every Asana Trigger binding polled through one connection. */
export interface TaskTriggerRunnerConfig {
  /** A connection from newConnection with a Client or newProjectConnection. */ connection: Connection;
  /** The taskChanged bindings; at least one is required. */ taskChangedRoutes: readonly TaskChangedTriggerRoute[];
}

/**
 * Create one poll runner for the configured taskChanged bindings. run replays each durable target, records a
 * baseline for a binding without a cursor, then polls every binding each pollInterval with jitter, following
 * up to 10 pages of 100 events per poll. An expired or invalid sync token (HTTP 412) records a new baseline and
 * logs a WARN record; changes made between the last poll and the new baseline are not delivered. A 429 pauses
 * every binding of the connection for Retry-After, or backs off exponentially without it; other failures back
 * off per binding up to 15 minutes. Delivery is at least once from Asana's event stream, which Asana itself
 * delivers at most once, and each event keeps one Trigger event ID across refetches of the same sync token, as
 * in Go. Throws TypeError for a connection without a Client, no routes, a blank or repeated binding name, a
 * missing target, or an invalid configuration.
 */
export function newTaskTriggerRunner(config: TaskTriggerRunnerConfig): Runner {
  const client = asanaClientOf(config.connection);
  if (!config.taskChangedRoutes?.length) throw new TypeError('Asana taskChanged routes are required');
  const pollers: Poller[] = config.taskChangedRoutes.map(route => {
    if (!route.bindingName?.trim() || !route.target) throw new TypeError('Asana taskChanged binding name and target are required');
    validateRouteConfiguration(route.configuration);
    return newBindingPoller<TaskChangedEvent>({
      definition: TaskChangedTriggerDefinition,
      binding: { connection: config.connection.reference, trigger: TaskChangedTriggerDefinition.trigger, name: route.bindingName },
      cursor: route.cursor ?? newMemoryCursorStore(),
      fetcher: new TaskEventFetcher(client, config.connection.reference, TaskChangedTriggerDefinition.trigger, route.configuration.projectId),
      target: route.target,
    });
  });
  return newRunner({ connection: config.connection.reference, schedule: pollSchedule(client), pollers, ...(client.triggerLogger ? { logger: client.triggerLogger } : {}) });
}

/** Selects one taskChanged binding saved in the project configuration. */
export interface ProjectTaskChangedTriggerRoute {
  /** The saved binding's name. */ bindingName: string;
  /** Routes each change into the application, usually newDexFlowTriggerTarget. */ target: TriggerTarget<TaskChangedEvent>;
}

/** Selects Asana Trigger bindings saved in the project configuration. */
export interface ProjectTaskTriggerRunnerConfig {
  /** The taskChanged bindings; at least one is required. */ taskChangedRoutes: readonly ProjectTaskChangedTriggerRoute[];
}

/**
 * Open connectionName from the loaded project, load each binding's saved project, and create a durable runner:
 * each route records changes in its binding's project Trigger inbox before its sync token commits in the
 * binding's project poll cursor, so a restarted or replaced replica neither misses nor repeats a change. The
 * client options' logger also applies to those inboxes. Under dexcli dev the poll cursor stays in process
 * memory; hosted project storage keeps it. Throws for a missing target, a binding without a saved project, or
 * an invalid configuration.
 */
export function newProjectTaskTriggerRunner(project: LoadedProject, connectionName: string, config: ProjectTaskTriggerRunnerConfig, options: ClientOptions = {}): Runner {
  const connection = newProjectConnection(project, connectionName, options);
  const triggerName = TaskChangedTriggerDefinition.trigger.triggerName;
  const routes = config.taskChangedRoutes.map((route): TaskChangedTriggerRoute => {
    if (!route.target) throw new TypeError(`Asana taskChanged binding ${JSON.stringify(route.bindingName)} target is required`);
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName, triggerName, bindingName: route.bindingName };
    let configuration: TaskChangedTriggerConfiguration;
    try { configuration = decodeTriggerConfiguration(project.configuration, key, TaskChangedTriggerConfigurationSchema); }
    catch (error) {
      if (error instanceof ObjectNotFoundError) throw new TypeError(`asana taskChanged binding ${JSON.stringify(route.bindingName)} has no saved project; pick one in Dex Web and restart`, { cause: error });
      throw new TypeError(`asana taskChanged binding ${JSON.stringify(route.bindingName)} configuration: ${error instanceof Error ? error.message : String(error)}`, { cause: error });
    }
    const target = newDurableTriggerTarget(project.triggerInbox(key), key, route.target, { ...(options.logger ? { logger: options.logger } : {}), decodePayload: decodeTaskChangedEventPayload });
    return { bindingName: route.bindingName, configuration, target, cursor: project.pollCursorStore(key) };
  });
  return newTaskTriggerRunner({ connection, taskChangedRoutes: routes });
}

/** Configuration of one taskChanged Trigger with its cursor in memory. */
export interface TaskChangedTriggerConfig {
  /** A connection from newConnection with a Client or newProjectConnection. */ connection: Connection;
  /** Static connection name; must equal the connection's name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Selects the watched project. */ configuration: TaskChangedTriggerConfiguration;
  /** Routes each change into the application. */ target: TriggerTarget<TaskChangedEvent>;
}

/**
 * Create one taskChanged TriggerRunner that keeps its cursor in memory, so every start records a new baseline
 * and changes made while the process is stopped are lost; use it for tests and demonstrations and
 * newProjectTaskTriggerRunner otherwise. Throws as newTaskTriggerRunner does.
 */
export function newTaskChangedTrigger(config: TaskChangedTriggerConfig): TriggerRunner {
  if (config.connectionName !== config.connection.reference.name) throw new TypeError(`asana connector trigger connectionName "${config.connectionName}" must equal its connection's name "${config.connection.reference.name}"`);
  const client = asanaClientOf(config.connection);
  if (!config.bindingName?.trim() || !config.target) throw new TypeError('Asana taskChanged binding name and target are required');
  validateRouteConfiguration(config.configuration);
  const binding = { connection: config.connection.reference, trigger: TaskChangedTriggerDefinition.trigger, name: config.bindingName };
  const source = newBindingTriggerSource<TaskChangedEvent>({
    definition: TaskChangedTriggerDefinition, binding, cursor: newMemoryCursorStore(),
    fetcher: new TaskEventFetcher(client, config.connection.reference, TaskChangedTriggerDefinition.trigger, config.configuration.projectId),
  }, pollSchedule(client), client.triggerLogger);
  return newTrigger({ definition: TaskChangedTriggerDefinition, binding, source, target: config.target, ...(client.triggerLogger ? { logger: client.triggerLogger } : {}) });
}

function validateRouteConfiguration(configuration: TaskChangedTriggerConfiguration): void {
  try { validateTaskChangedTriggerConfiguration(configuration ?? { projectId: '' }); }
  catch (error) { throw new TypeError(`asana taskChanged configuration: ${error instanceof Error ? error.message : String(error)}`, { cause: error }); }
}

function pollSchedule(client: Client): PollSchedule {
  // An omitted maxBackoffMs uses the kit's cap, which grows with a long poll interval.
  return { intervalMs: client.pollIntervalMs };
}

function asanaClientOf(connection: Connection): Client {
  if (!(connection?.operations instanceof Client)) throw new TypeError('Asana Triggers require a connection from newProjectConnection or newConnection with a Client');
  return connection.operations;
}
