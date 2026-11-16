// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  mustTriggerBindingDefinition, newTrigger,
  type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerDefinition, type TriggerLogger, type TriggerRunner, type TriggerTarget,
} from '@superdurable/connectors-sdk';
import { newBindingPoller, newBindingTriggerSource, newMemoryCursorStore, newRunner, type CursorStore, type PollSchedule, type Poller, type RequestLimiter, type Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { ObjectNotFoundError, decodeTriggerConfiguration, newDurableTriggerTarget, type LoadedProject, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { BambooHRClient } from './client.js';
import { EmployeeChangeFetcher, validateEmployeeChangedTriggerConfiguration, type EmployeeChangedEvent, type EmployeeChangedTriggerConfiguration } from './employee_change_fetcher.js';
import type { Connection } from './generated/connection.js';
import { ConnectorID } from './generated/definitions.js';
import { EmployeeChangeSchema } from './generated/models.js';

/** Manifest definition of employeeChanged. */
export const EmployeeChangedTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: 'employeeChanged' },
  description: 'Receive each employee BambooHR reports as inserted, updated, or deleted after the last processed change, polled from the changed-employees list with a durable cursor; the first poll records the current time and delivers no earlier changes.',
};

/** The connector UI unit that saves an employeeChanged binding's change type. */
export const UIUnitChangeTypePicker = 'changeTypePicker';
/** The changeTypePicker output port: inserted, updated, deleted, or empty for every change. */
export const UIChangeTypePickerPortChangeType = 'changeType';

/** The Go connector's JSON of one employeeChanged event, which Go and TypeScript inbox replicas share. */
export const EmployeeChangedEventSchema: z.ZodType<EmployeeChangedEvent> = EmployeeChangeSchema;
/** The saved binding configuration; an unknown field is rejected, as Go decodes it. */
export const EmployeeChangedTriggerConfigurationSchema: z.ZodType<EmployeeChangedTriggerConfiguration> = z.strictObject({ changeType: z.string().optional() });

/** One static application binding of employeeChanged. */
export interface EmployeeChangedTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Optional Connector Studio units, usually one changeTypePicker bound to /changeType. */ configurationUI?: ConnectorConfigurationUI;
}

/** Declare a static employeeChanged binding for Flow Definition tooling and Dex Web. */
export function defineEmployeeChangedTriggerBinding(config: EmployeeChangedTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: EmployeeChangedTriggerDefinition, ...config });
}

/** Binds one employeeChanged configuration to its application target and cursor. */
export interface EmployeeChangedTriggerRoute {
  /** The stable application binding name, unique within the runner. */ bindingName: string;
  /** Selects the change type the binding delivers; omitted delivers every change. */ configuration?: EmployeeChangedTriggerConfiguration;
  /**
   * Routes each change into the application. A target from newDurableTriggerTarget records each change before
   * the cursor commits past it; any other target consumes it first.
   */
  target: TriggerTarget<EmployeeChangedEvent>;
  /**
   * Stores the binding's position, such as LoadedProject.pollCursorStore. Omitted keeps it in memory, so every
   * process start begins at the current time and changes made while stopped are lost.
   */
  cursor?: CursorStore;
}

/** Options of the employeeChanged runners. */
export interface EmployeeChangedTriggerRunnerOptions {
  /** Receives seed, reseed, poll failure, rate-limit, and inbox records, never employee data or API keys; omitted uses defaultTriggerLogger. */ logger?: TriggerLogger;
  /** Optionally paces every poll request, for example to share BambooHR's throttling budget with other work. */ requestLimiter?: RequestLimiter;
}

/**
 * Create one poll Runner for the connection's employeeChanged bindings. Run replays each durable target,
 * starts a binding without a cursor at BambooHR's current time, then polls every binding each pollInterval
 * with jitter. Each poll makes one GET /employees/changed request and delivers the changes after the cursor
 * that are at least 30 seconds old by BambooHR's Date header, oldest first, at most 500 per page; further
 * pages are read at once, up to the kit's page cap per poll. A 429 or 503 pauses every binding of the
 * connection for Retry-After, or backs off exponentially without it. A 400 rejecting the cursor starts the
 * binding again at the current time and logs a WARN record; changes in between are not delivered. Other
 * failures back off per binding. Delivery is at least once, and each change keeps one Trigger event ID
 * across refetches. Throws for a connection without a BambooHRClient, no routes, a blank binding name, a
 * missing target, or an invalid configuration.
 */
export function newEmployeeChangedTriggerRunner(connection: Connection, routes: readonly EmployeeChangedTriggerRoute[], options: EmployeeChangedTriggerRunnerOptions = {}): Runner {
  const client = bambooHRClientOf(connection);
  if (!routes?.length) throw new TypeError('BambooHR employeeChanged routes are required');
  const pollers: Poller[] = routes.map(route => {
    if (!route?.bindingName?.trim() || !route.target) throw new TypeError('BambooHR employeeChanged binding name and target are required');
    const configuration = route.configuration ?? {};
    validateEmployeeChangedTriggerConfiguration(configuration);
    return newBindingPoller<EmployeeChangedEvent>({
      definition: EmployeeChangedTriggerDefinition,
      binding: { connection: connection.reference, trigger: EmployeeChangedTriggerDefinition.trigger, name: route.bindingName },
      cursor: route.cursor ?? newMemoryCursorStore(),
      fetcher: new EmployeeChangeFetcher(client, connection.reference, EmployeeChangedTriggerDefinition.trigger, configuration),
      target: route.target,
    });
  });
  return newRunner({
    connection: connection.reference, schedule: pollSchedule(client), pollers,
    ...(options.logger ? { logger: options.logger } : {}), ...(options.requestLimiter ? { requestLimiter: options.requestLimiter } : {}),
  });
}

/** Selects one employeeChanged binding saved in the project configuration. */
export interface ProjectEmployeeChangedTriggerRoute {
  /** Names the saved binding. */ bindingName: string;
  /** Routes each change into the application, usually newDexFlowTriggerTarget. */ target: TriggerTarget<EmployeeChangedEvent>;
}

/**
 * Create a durable Runner for employeeChanged bindings saved in the loaded project. connection is the one
 * newProjectConnection opened from project. Each route loads its binding's saved change type (an unsaved
 * binding delivers every change) and records changes in the binding's project Trigger inbox before its cursor
 * commits in the binding's project poll cursor, so a restarted or replaced replica neither misses nor repeats
 * a change. Inbox payloads and cursors use the Go connector's JSON, so Go and TypeScript replicas share them.
 * Throws for an invalid saved configuration and as newEmployeeChangedTriggerRunner does.
 */
export function newProjectEmployeeChangedTriggerRunner(project: LoadedProject, connection: Connection, routes: readonly ProjectEmployeeChangedTriggerRoute[], options: EmployeeChangedTriggerRunnerOptions = {}): Runner {
  if (!project) throw new TypeError('BambooHR employeeChanged runner requires the loaded project configuration');
  const durableRoutes = (routes ?? []).map((route): EmployeeChangedTriggerRoute => {
    if (!route?.target) throw new TypeError(`BambooHR employeeChanged binding ${JSON.stringify(route?.bindingName)} target is required`);
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName: connection.reference.name, triggerName: EmployeeChangedTriggerDefinition.trigger.triggerName, bindingName: route.bindingName };
    let configuration: EmployeeChangedTriggerConfiguration = {};
    try { configuration = decodeTriggerConfiguration(project.configuration, key, EmployeeChangedTriggerConfigurationSchema); }
    catch (error) {
      // An unsaved binding delivers every change, as an empty changeTypePicker does.
      if (!(error instanceof ObjectNotFoundError)) throw new TypeError(`bamboohr employeeChanged binding ${JSON.stringify(route.bindingName)} configuration: ${error instanceof Error ? error.message : String(error)}`);
    }
    const target = newDurableTriggerTarget(project.triggerInbox(key), key, route.target, { decodePayload: decodeEmployeeChangedEventPayload, ...(options.logger ? { logger: options.logger } : {}) });
    return { bindingName: route.bindingName, configuration, target, cursor: project.pollCursorStore(key) };
  });
  return newEmployeeChangedTriggerRunner(connection, durableRoutes, options);
}

/** Configuration of one in-memory employeeChanged Trigger. */
export interface EmployeeChangedTriggerConfig {
  /** BambooHR connection from newProjectConnection or newConnection with a BambooHRClient. */ connection: Connection;
  /** Selects the change type; omitted delivers every change. */ configuration?: EmployeeChangedTriggerConfiguration;
  /** Routes accepted changes into the application. */ target: TriggerTarget<EmployeeChangedEvent>;
  /** Receives the Trigger's records; omitted uses defaultTriggerLogger. */ logger?: TriggerLogger;
}

/**
 * Create a single employeeChanged TriggerRunner, as Go's generated NewEmployeeChangedTrigger does; as in Go,
 * the binding is named after the Trigger. It has no project storage, so it keeps the cursor in memory and
 * starts at the current time on every start; changes made while stopped are lost. Use
 * newProjectEmployeeChangedTriggerRunner for durable delivery.
 */
export function newEmployeeChangedTrigger(config: EmployeeChangedTriggerConfig): TriggerRunner {
  const client = bambooHRClientOf(config.connection);
  if (!config.target) throw new TypeError('BambooHR employeeChanged target is required');
  const configuration = config.configuration ?? {};
  validateEmployeeChangedTriggerConfiguration(configuration);
  const trigger = EmployeeChangedTriggerDefinition.trigger;
  const binding = { connection: config.connection.reference, trigger, name: trigger.triggerName };
  const source = newBindingTriggerSource<EmployeeChangedEvent>({
    definition: EmployeeChangedTriggerDefinition, binding, cursor: newMemoryCursorStore(),
    fetcher: new EmployeeChangeFetcher(client, config.connection.reference, trigger, configuration),
  }, pollSchedule(client), config.logger);
  return newTrigger({ definition: EmployeeChangedTriggerDefinition, binding, source, target: config.target, ...(config.logger ? { logger: config.logger } : {}) });
}

/** Decode a replayed inbox payload, written by a Go or TypeScript replica; throws for another payload. */
export function decodeEmployeeChangedEventPayload(payloadJSON: string): EmployeeChangedEvent {
  const parsed = EmployeeChangedEventSchema.safeParse(JSON.parse(payloadJSON));
  if (!parsed.success) throw new TypeError('stored BambooHR employeeChanged payload is invalid');
  return parsed.data;
}

function bambooHRClientOf(connection: Connection): BambooHRClient {
  if (!(connection?.operations instanceof BambooHRClient)) throw new TypeError('BambooHR employeeChanged Trigger requires a connection from newProjectConnection or newConnection with a BambooHRClient');
  return connection.operations;
}

// Omitted maxBackoffMs uses the kit's cap, which grows with a long poll interval.
function pollSchedule(client: BambooHRClient): PollSchedule { return { intervalMs: client.pollIntervalMs }; }
