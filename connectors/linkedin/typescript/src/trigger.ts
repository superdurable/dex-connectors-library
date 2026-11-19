// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  mustTriggerBindingDefinition, newTrigger,
  type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerDefinition, type TriggerLogger, type TriggerRunner, type TriggerTarget,
} from '@superdurable/connectors-sdk';
import { newBindingPoller, newBindingTriggerSource, newMemoryCursorStore, newRunner, type CursorStore, type PollSchedule, type Poller, type Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { ObjectNotFoundError, decodeTriggerConfiguration, newDurableTriggerTarget, type LoadedProject, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { LinkedInClient } from './client.js';
import { ConnectorID } from './generated/definitions.js';
import type { Connection } from './generated/connection.js';
import { ProfileFetcher } from './profile_fetcher.js';
import { ProfileChangedEventSchema, ProfileChangedTriggerConfigurationSchema, type ProfileChangedEvent, type ProfileChangedTriggerConfiguration } from './profile_changed.js';

/** Manifest definition of profileChanged. */
export const ProfileChangedTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: 'profileChanged' },
  description: "Receive a change whenever the connected member's own LinkedIn OpenID Connect profile claims (name, given and family name, email, email verification, picture, or locale) change, found by comparing each UserInfo poll with the last; it never watches other members, and the first run records a baseline and delivers nothing.",
};

/** One static application binding of profileChanged. */
export interface ProfileChangedTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Optional Connector Studio units; the binding has no field, because it always watches the connected member's own profile. */ configurationUI?: ConnectorConfigurationUI;
}

/** Declare a static profileChanged binding for Flow Definition tooling and Dex Web. */
export function defineProfileChangedTriggerBinding(config: ProfileChangedTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: ProfileChangedTriggerDefinition, ...config });
}

/** Binds one profileChanged binding to its application target and cursor. */
export interface ProfileChangedTriggerRoute {
  /** The stable application binding name, unique within the runner. */ bindingName: string;
  /** The binding configuration; the empty object is the only one. */ configuration?: ProfileChangedTriggerConfiguration;
  /**
   * Routes each change into the application. A target from newDurableTriggerTarget records each change before
   * the cursor commits past it; any other target consumes it first.
   */
  target: TriggerTarget<ProfileChangedEvent>;
  /**
   * Stores the binding's last profile, such as LoadedProject.pollCursorStore. Omitted keeps it in memory, so
   * every process start records a new baseline and changes made while stopped are lost.
   */
  cursor?: CursorStore;
}

/** Options of the profileChanged runners. */
export interface ProfileTriggerRunnerOptions {
  /** Receives seed, reseed, poll failure, rate-limit, and inbox records, never profile claims or credentials; omitted uses defaultTriggerLogger. */ logger?: TriggerLogger;
}

/**
 * Create one poll Runner for the connection's profileChanged bindings. Each binding watches only the
 * connected member's own profile. Run replays each durable target, records the current profile as a baseline
 * for a binding without a cursor, then reads LinkedIn UserInfo each pollInterval with jitter and delivers one
 * change whenever the profile's claims differ from the stored ones. A stored cursor that is undecodable, or a
 * connection re-authorized as another member, records a new baseline and logs a WARN record; differences in
 * that gap are not delivered. A 429 pauses every binding of the connection for Retry-After, or backs off
 * exponentially without it; other failures, including a 401 that a credential refresh did not fix, back off
 * per binding. Delivery is at least once, and each change keeps one Trigger event ID across refetches.
 * Throws for a connection without a LinkedInClient, no routes, a blank binding name, a missing target, or an
 * invalid configuration.
 */
export function newProfileTriggerRunner(connection: Connection, routes: readonly ProfileChangedTriggerRoute[], options: ProfileTriggerRunnerOptions = {}): Runner {
  const client = linkedInClientOf(connection);
  if (!routes?.length) throw new TypeError('LinkedIn profileChanged routes are required');
  const pollers: Poller[] = routes.map(route => {
    if (!route?.bindingName?.trim() || !route.target) throw new TypeError('LinkedIn profileChanged binding name and target are required');
    parseConfiguration(route.configuration ?? {});
    return newBindingPoller<ProfileChangedEvent>({
      definition: ProfileChangedTriggerDefinition,
      binding: { connection: connection.reference, trigger: ProfileChangedTriggerDefinition.trigger, name: route.bindingName },
      cursor: route.cursor ?? newMemoryCursorStore(),
      fetcher: new ProfileFetcher(client, connection.reference, ProfileChangedTriggerDefinition.trigger), target: route.target,
    });
  });
  return newRunner({ connection: connection.reference, schedule: pollSchedule(client), pollers, ...(options.logger ? { logger: options.logger } : {}) });
}

/** Selects one profileChanged binding saved in the project configuration. */
export interface ProjectProfileChangedTriggerRoute {
  /** Names the saved binding. */ bindingName: string;
  /** Routes each change into the application, usually newDexFlowTriggerTarget. */ target: TriggerTarget<ProfileChangedEvent>;
}

/**
 * Create a durable Runner for profileChanged bindings saved in the loaded project. connection is the one
 * newProjectConnection opened from project. Each route loads its binding's saved configuration (an unsaved
 * binding uses the empty configuration) and records changes in the binding's project Trigger inbox before its
 * stored profile commits in the binding's project poll cursor, so a restarted or replaced replica neither
 * misses nor repeats a change. Cursor tokens and inbox payloads use the Go connector's JSON, so Go and
 * TypeScript replicas share them. Throws for an invalid saved configuration and as newProfileTriggerRunner does.
 */
export function newProjectProfileTriggerRunner(project: LoadedProject, connection: Connection, routes: readonly ProjectProfileChangedTriggerRoute[], options: ProfileTriggerRunnerOptions = {}): Runner {
  if (!project) throw new TypeError('LinkedIn profileChanged runner requires the loaded project configuration');
  const durableRoutes = (routes ?? []).map((route): ProfileChangedTriggerRoute => {
    if (!route?.target) throw new TypeError(`LinkedIn profileChanged binding ${JSON.stringify(route?.bindingName)} target is required`);
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName: connection.reference.name, triggerName: ProfileChangedTriggerDefinition.trigger.triggerName, bindingName: route.bindingName };
    let configuration: ProfileChangedTriggerConfiguration = {};
    try { configuration = decodeTriggerConfiguration(project.configuration, key, ProfileChangedTriggerConfigurationSchema); }
    catch (error) {
      // An unsaved binding uses the empty configuration.
      if (!(error instanceof ObjectNotFoundError)) throw new TypeError(`linkedin profileChanged binding ${JSON.stringify(route.bindingName)} configuration: ${error instanceof Error ? error.message : String(error)}`);
    }
    const target = newDurableTriggerTarget(project.triggerInbox(key), key, route.target, { decodePayload: decodeProfileChangedEventPayload, ...(options.logger ? { logger: options.logger } : {}) });
    return { bindingName: route.bindingName, configuration, target, cursor: project.pollCursorStore(key) };
  });
  return newProfileTriggerRunner(connection, durableRoutes, options);
}

/** Configuration of one in-memory profileChanged Trigger. */
export interface ProfileChangedTriggerConfig {
  /** LinkedIn connection from newProjectConnection or newConnection with a LinkedInClient. */ connection: Connection;
  /** Static binding name. */ bindingName: string;
  /** The binding configuration; the empty object is the only one. */ configuration?: ProfileChangedTriggerConfiguration;
  /** Routes accepted changes into the application. */ target: TriggerTarget<ProfileChangedEvent>;
  /** Receives the Trigger's records; omitted uses defaultTriggerLogger. */ logger?: TriggerLogger;
}

/**
 * Create a single profileChanged TriggerRunner, as Go's generated NewProfileChangedTrigger does. It has no
 * project storage, so it keeps the cursor in memory and records a new baseline on every start; changes made
 * while stopped are lost. Use newProjectProfileTriggerRunner for durable delivery.
 */
export function newProfileChangedTrigger(config: ProfileChangedTriggerConfig): TriggerRunner {
  const client = linkedInClientOf(config.connection);
  if (!config.bindingName?.trim() || !config.target) throw new TypeError('LinkedIn profileChanged binding name and target are required');
  parseConfiguration(config.configuration ?? {});
  const binding = { connection: config.connection.reference, trigger: ProfileChangedTriggerDefinition.trigger, name: config.bindingName };
  const source = newBindingTriggerSource<ProfileChangedEvent>({
    definition: ProfileChangedTriggerDefinition, binding, cursor: newMemoryCursorStore(),
    fetcher: new ProfileFetcher(client, config.connection.reference, ProfileChangedTriggerDefinition.trigger),
  }, pollSchedule(client), config.logger);
  return newTrigger({ definition: ProfileChangedTriggerDefinition, binding, source, target: config.target, ...(config.logger ? { logger: config.logger } : {}) });
}

/** Decode a replayed inbox payload, written by a Go or TypeScript replica; throws for another payload. */
export function decodeProfileChangedEventPayload(payloadJSON: string): ProfileChangedEvent {
  const parsed = ProfileChangedEventSchema.safeParse(JSON.parse(payloadJSON));
  if (!parsed.success) throw new TypeError('stored LinkedIn profileChanged payload is invalid');
  return parsed.data;
}

function parseConfiguration(configuration: ProfileChangedTriggerConfiguration): void {
  const parsed = ProfileChangedTriggerConfigurationSchema.safeParse(configuration);
  if (!parsed.success) throw new TypeError(`linkedin profileChanged configuration: ${parsed.error.issues.map(issue => issue.message).join('; ')}`);
}

function linkedInClientOf(connection: Connection): LinkedInClient {
  if (!(connection?.operations instanceof LinkedInClient)) throw new TypeError('LinkedIn profileChanged Trigger requires a connection from newProjectConnection or newConnection with a LinkedInClient');
  return connection.operations;
}

// Omitted maxBackoffMs uses the kit's cap, which grows with a long poll interval.
function pollSchedule(client: LinkedInClient): PollSchedule { return { intervalMs: client.pollIntervalMs }; }
