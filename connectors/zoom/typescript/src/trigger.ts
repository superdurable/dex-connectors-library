// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  mustTriggerBindingDefinition, newTrigger,
  type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerDefinition, type TriggerLogger, type TriggerRunner, type TriggerTarget,
} from '@superdurable/connectors-sdk';
import { newBindingPoller, newBindingTriggerSource, newMemoryCursorStore, newRunner, type CursorStore, type PollSchedule, type Poller, type Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { ObjectNotFoundError, decodeTriggerConfiguration, newDurableTriggerTarget, type LoadedProject, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { ZoomClient } from './client.js';
import { ConnectorID } from './generated/definitions.js';
import type { Connection } from './generated/connection.js';
import { MeetingListFetcher, MeetingSnapshotOptions } from './meeting_list_fetcher.js';
import { MeetingChangedEventSchema, MeetingChangedTriggerConfigurationSchema, type MeetingChangedEvent, type MeetingChangedTriggerConfiguration } from './meeting_snapshot.js';

/** Manifest definition of meetingChanged. */
export const MeetingChangedTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: 'meetingChanged' },
  description: "Receive each meeting the authorized user schedules, changes, or that leaves Zoom's scheduled list, found by comparing each poll of List meetings with the last; the first run records a baseline and delivers no existing meetings.",
};

/** One static application binding of meetingChanged. */
export interface MeetingChangedTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Optional Connector Studio units; the binding has no field, because it always watches the authorized user. */ configurationUI?: ConnectorConfigurationUI;
}

/** Declare a static meetingChanged binding for Flow Definition tooling and Dex Web. */
export function defineMeetingChangedTriggerBinding(config: MeetingChangedTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: MeetingChangedTriggerDefinition, ...config });
}

/** Binds one meetingChanged configuration to its application target and cursor. */
export interface MeetingChangedTriggerRoute {
  /** The stable application binding name, unique within the runner. */ bindingName: string;
  /** Narrows the delivered change kinds; omitted delivers every kind. */ configuration?: MeetingChangedTriggerConfiguration;
  /**
   * Routes each change into the application. A target from newDurableTriggerTarget records each change before
   * the cursor commits past it; any other target consumes it first.
   */
  target: TriggerTarget<MeetingChangedEvent>;
  /**
   * Stores the binding's meeting snapshot, such as LoadedProject.pollCursorStore. Omitted keeps it in memory,
   * so every process start records a new baseline and changes made while stopped are lost.
   */
  cursor?: CursorStore;
}

/** Options of the meetingChanged runners. */
export interface MeetingTriggerRunnerOptions {
  /** Receives seed, reseed, poll failure, rate-limit, and inbox records, never payloads or credentials; omitted uses defaultTriggerLogger. */ logger?: TriggerLogger;
}

/**
 * Create one poll Runner for the connection's meetingChanged bindings. Run replays each durable target,
 * records the current meeting list as a baseline for a binding without a cursor, then lists every page of
 * the authorized user's scheduled meetings each pollInterval with jitter and delivers the differences from
 * the stored snapshot. A stored cursor that is undecodable or holds another Zoom user's meetings records a
 * new baseline and logs a WARN record; differences in that gap are not delivered. A 429, including Zoom's
 * daily-limit 429, pauses every binding of the connection for Retry-After, or backs off exponentially
 * without it; other failures back off per binding up to 15 minutes. Delivery is at least once, and each
 * change keeps one Trigger event ID across refetches. Throws for a connection without a ZoomClient, no
 * routes, a blank binding name, a missing target, or an invalid configuration.
 */
export function newMeetingTriggerRunner(connection: Connection, routes: readonly MeetingChangedTriggerRoute[], options: MeetingTriggerRunnerOptions = {}): Runner {
  const client = zoomClientOf(connection);
  if (!routes?.length) throw new TypeError('Zoom meetingChanged routes are required');
  const pollers: Poller[] = routes.map(route => {
    if (!route?.bindingName?.trim() || !route.target) throw new TypeError('Zoom meetingChanged binding name and target are required');
    const configuration = parseConfiguration(route.configuration ?? {});
    return newBindingPoller<MeetingChangedEvent>({
      definition: MeetingChangedTriggerDefinition,
      binding: { connection: connection.reference, trigger: MeetingChangedTriggerDefinition.trigger, name: route.bindingName },
      cursor: route.cursor ?? newMemoryCursorStore(),
      fetcher: new MeetingListFetcher(client, connection.reference, MeetingChangedTriggerDefinition.trigger, configuration),
      target: route.target, snapshot: MeetingSnapshotOptions,
    });
  });
  return newRunner({ connection: connection.reference, schedule: pollSchedule(client), pollers, ...(options.logger ? { logger: options.logger } : {}) });
}

/** Selects one meetingChanged binding saved in the project configuration. */
export interface ProjectMeetingChangedTriggerRoute {
  /** Names the saved binding. */ bindingName: string;
  /** Routes each change into the application, usually newDexFlowTriggerTarget. */ target: TriggerTarget<MeetingChangedEvent>;
}

/**
 * Create a durable Runner for meetingChanged bindings saved in the loaded project. connection is the one
 * newProjectConnection opened from project. Each route loads its binding's saved configuration (an unsaved
 * binding delivers every change kind) and records changes in the binding's project Trigger inbox before its
 * meeting snapshot commits in the binding's project poll cursor, so a restarted or replaced replica neither
 * misses nor repeats a change. Inbox payloads use the Go connector's JSON fields, so Go and TypeScript
 * replicas share them. Throws for an invalid saved configuration and as newMeetingTriggerRunner does.
 */
export function newProjectMeetingTriggerRunner(project: LoadedProject, connection: Connection, routes: readonly ProjectMeetingChangedTriggerRoute[], options: MeetingTriggerRunnerOptions = {}): Runner {
  if (!project) throw new TypeError('Zoom meetingChanged runner requires the loaded project configuration');
  const durableRoutes = (routes ?? []).map((route): MeetingChangedTriggerRoute => {
    if (!route?.target) throw new TypeError(`Zoom meetingChanged binding ${JSON.stringify(route?.bindingName)} target is required`);
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName: connection.reference.name, triggerName: MeetingChangedTriggerDefinition.trigger.triggerName, bindingName: route.bindingName };
    let configuration: MeetingChangedTriggerConfiguration = {};
    try { configuration = decodeTriggerConfiguration(project.configuration, key, MeetingChangedTriggerConfigurationSchema); }
    catch (error) {
      // An unsaved binding delivers every change kind.
      if (!(error instanceof ObjectNotFoundError)) throw new TypeError(`zoom meetingChanged binding ${JSON.stringify(route.bindingName)} configuration: ${error instanceof Error ? error.message : String(error)}`);
    }
    const target = newDurableTriggerTarget(project.triggerInbox(key), key, route.target, { decodePayload: decodeMeetingChangedEventPayload, ...(options.logger ? { logger: options.logger } : {}) });
    return { bindingName: route.bindingName, configuration, target, cursor: project.pollCursorStore(key) };
  });
  return newMeetingTriggerRunner(connection, durableRoutes, options);
}

/** Configuration of one in-memory meetingChanged Trigger. */
export interface MeetingChangedTriggerConfig {
  /** Zoom connection from newProjectConnection or newConnection with a ZoomClient. */ connection: Connection;
  /** Static binding name. */ bindingName: string;
  /** Narrows the delivered change kinds; omitted delivers every kind. */ configuration?: MeetingChangedTriggerConfiguration;
  /** Routes accepted changes into the application. */ target: TriggerTarget<MeetingChangedEvent>;
  /** Receives the Trigger's records; omitted uses defaultTriggerLogger. */ logger?: TriggerLogger;
}

/**
 * Create a single meetingChanged TriggerRunner, as Go's generated NewMeetingChangedTrigger does. It has no
 * project storage, so it keeps the cursor in memory and records a new baseline on every start; changes made
 * while stopped are lost. Use newProjectMeetingTriggerRunner for durable delivery.
 */
export function newMeetingChangedTrigger(config: MeetingChangedTriggerConfig): TriggerRunner {
  const client = zoomClientOf(config.connection);
  if (!config.bindingName?.trim() || !config.target) throw new TypeError('Zoom meetingChanged binding name and target are required');
  const configuration = parseConfiguration(config.configuration ?? {});
  const binding = { connection: config.connection.reference, trigger: MeetingChangedTriggerDefinition.trigger, name: config.bindingName };
  const source = newBindingTriggerSource<MeetingChangedEvent>({
    definition: MeetingChangedTriggerDefinition, binding, cursor: newMemoryCursorStore(),
    fetcher: new MeetingListFetcher(client, config.connection.reference, MeetingChangedTriggerDefinition.trigger, configuration), snapshot: MeetingSnapshotOptions,
  }, pollSchedule(client), config.logger);
  return newTrigger({ definition: MeetingChangedTriggerDefinition, binding, source, target: config.target, ...(config.logger ? { logger: config.logger } : {}) });
}

/** Decode a replayed inbox payload, written by a Go or TypeScript replica; throws for another payload. */
export function decodeMeetingChangedEventPayload(payloadJSON: string): MeetingChangedEvent {
  const parsed = MeetingChangedEventSchema.safeParse(JSON.parse(payloadJSON));
  if (!parsed.success) throw new TypeError('stored Zoom meetingChanged payload is invalid');
  return parsed.data;
}

function parseConfiguration(configuration: MeetingChangedTriggerConfiguration): MeetingChangedTriggerConfiguration {
  const parsed = MeetingChangedTriggerConfigurationSchema.safeParse(configuration);
  if (!parsed.success) throw new TypeError(`zoom meetingChanged configuration: ${parsed.error.issues.map(issue => issue.message).join('; ')}`);
  return parsed.data;
}

function zoomClientOf(connection: Connection): ZoomClient {
  if (!(connection?.operations instanceof ZoomClient)) throw new TypeError('Zoom meetingChanged Trigger requires a connection from newProjectConnection or newConnection with a ZoomClient');
  return connection.operations;
}

// Omitted maxBackoffMs uses the kit's cap, which grows with a long poll interval.
function pollSchedule(client: ZoomClient): PollSchedule { return { intervalMs: client.pollIntervalMs }; }
