// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  mustTriggerBindingDefinition, newTrigger,
  type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerDefinition, type TriggerLogger, type TriggerRunner, type TriggerTarget,
} from '@superdurable/connectors-sdk';
import { newBindingPoller, newBindingTriggerSource, newMemoryCursorStore, newRunner, type CursorStore, type PollSchedule, type Poller, type Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { ObjectNotFoundError, decodeTriggerConfiguration, newDurableTriggerTarget, type LoadedProject, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { MailchimpClient } from './client.js';
import { ConnectorID } from './generated/definitions.js';
import type { Connection } from './generated/connection.js';
import { MemberChangeFetcher, MemberSnapshotOptions } from './member_change_fetcher.js';
import { MemberChangedEventSchema, MemberChangedTriggerConfigurationSchema, type MemberChangedEvent, type MemberChangedTriggerConfiguration } from './member_snapshot.js';

/** Manifest definition of memberChanged. */
export const MemberChangedTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: 'memberChanged' },
  description: 'Receive each contact added to or changed in one Mailchimp audience, including subscribes, unsubscribes, profile and tag changes, and cleaned addresses, read by polling contacts by last change time; the first run records a baseline and delivers no existing contacts.',
};

/** Unit ID of the audience ID entry in the connector's UI bundle. */
export const UIUnitAudienceIDEntry = 'audienceIdEntry';
/** Port of the audienceIdEntry unit that saves the audience ID. */
export const UIAudienceIDEntryPortAudienceID = 'audienceId';
/** Unit ID of the contact status picker in the connector's UI bundle. */
export const UIUnitMemberStatusPicker = 'memberStatusPicker';
/** Port of the memberStatusPicker unit that saves the selected statuses. */
export const UIMemberStatusPickerPortStatuses = 'statuses';

/** One static application binding of memberChanged. */
export interface MemberChangedTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Connector Studio units that save the binding's audienceId and statuses, such as audienceIdEntry and memberStatusPicker. */ configurationUI?: ConnectorConfigurationUI;
}

/** Declare a static memberChanged binding for Flow Definition tooling and Dex Web. */
export function defineMemberChangedTriggerBinding(config: MemberChangedTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: MemberChangedTriggerDefinition, ...config });
}

/** Binds one memberChanged configuration to its application target and cursor. */
export interface MemberChangedTriggerRoute {
  /** The stable application binding name, unique within the runner. */ bindingName: string;
  /** Selects the watched audience, which is required, and the delivered statuses. */ configuration: MemberChangedTriggerConfiguration;
  /**
   * Routes each change into the application. A target from newDurableTriggerTarget records each change before
   * the cursor commits past it; any other target consumes it first.
   */
  target: TriggerTarget<MemberChangedEvent>;
  /**
   * Stores the binding's position, such as LoadedProject.pollCursorStore. Omitted keeps it in memory, so every
   * process start records a new baseline and changes made while stopped are lost.
   */
  cursor?: CursorStore;
}

/** Options of the memberChanged runners. */
export interface MemberTriggerRunnerOptions {
  /** Receives seed, reseed, poll failure, rate-limit, and inbox records, never contact data or credentials; omitted uses defaultTriggerLogger. */ logger?: TriggerLogger;
}

/**
 * Create one poll Runner for the connection's memberChanged bindings. Run replays each durable target,
 * records a baseline for a binding without a cursor, then polls every binding each pollInterval with jitter,
 * one request at a time, so the runner holds at most one of the 10 simultaneous requests Mailchimp allows
 * each user. A 429, or a 403 without an error document, pauses every binding of the connection for
 * Retry-After, or backs off exponentially without it; a 400 for the cursor's query records a new baseline
 * and logs a WARN record; other failures back off per binding up to 15 minutes. Delivery is at least once,
 * and each contact version keeps one Trigger event ID across refetches. Throws for a connection without a
 * MailchimpClient, no routes, a blank or repeated binding name, a missing target, or an invalid configuration.
 */
export function newMemberTriggerRunner(connection: Connection, routes: readonly MemberChangedTriggerRoute[], options: MemberTriggerRunnerOptions = {}): Runner {
  const client = mailchimpClientOf(connection);
  if (!routes?.length) throw new TypeError('Mailchimp memberChanged routes are required');
  const pollers: Poller[] = routes.map(route => {
    if (!route?.bindingName?.trim() || !route.target) throw new TypeError('Mailchimp memberChanged binding name and target are required');
    const configuration = parseConfiguration(route.configuration);
    return newBindingPoller<MemberChangedEvent>({
      definition: MemberChangedTriggerDefinition,
      binding: { connection: connection.reference, trigger: MemberChangedTriggerDefinition.trigger, name: route.bindingName },
      cursor: route.cursor ?? newMemoryCursorStore(),
      fetcher: new MemberChangeFetcher(client, connection.reference, MemberChangedTriggerDefinition.trigger, configuration),
      target: route.target, snapshot: MemberSnapshotOptions,
    });
  });
  return newRunner({ connection: connection.reference, schedule: pollSchedule(client), pollers, ...(options.logger ? { logger: options.logger } : {}) });
}

/** Selects one memberChanged binding saved in the project configuration. */
export interface ProjectMemberChangedTriggerRoute {
  /** Names the saved binding. */ bindingName: string;
  /** Routes each change into the application, usually newDexFlowTriggerTarget. */ target: TriggerTarget<MemberChangedEvent>;
}

/**
 * Create a durable Runner for memberChanged bindings saved in the loaded project. connection is the one
 * newProjectConnection opened from project. Each route loads its binding's saved audienceId and statuses and
 * records changes in the binding's project Trigger inbox before its position commits in the binding's project
 * poll cursor, so a restarted or replaced replica neither misses nor repeats a change. Inbox payloads use the
 * Go connector's JSON fields, so Go and TypeScript replicas share them. Throws for a binding without a saved
 * audience, an invalid saved configuration, and as newMemberTriggerRunner does.
 */
export function newProjectMemberTriggerRunner(project: LoadedProject, connection: Connection, routes: readonly ProjectMemberChangedTriggerRoute[], options: MemberTriggerRunnerOptions = {}): Runner {
  if (!project) throw new TypeError('Mailchimp memberChanged runner requires the loaded project configuration');
  const durableRoutes = (routes ?? []).map((route): MemberChangedTriggerRoute => {
    if (!route?.target) throw new TypeError(`Mailchimp memberChanged binding ${JSON.stringify(route?.bindingName)} target is required`);
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName: connection.reference.name, triggerName: MemberChangedTriggerDefinition.trigger.triggerName, bindingName: route.bindingName };
    let configuration: MemberChangedTriggerConfiguration;
    try { configuration = decodeTriggerConfiguration(project.configuration, key, MemberChangedTriggerConfigurationSchema); }
    catch (error) {
      if (error instanceof ObjectNotFoundError) throw new TypeError(`mailchimp memberChanged binding ${JSON.stringify(route.bindingName)} has no saved audience; enter its audience ID in Dex Web Connections`);
      throw new TypeError(`mailchimp memberChanged binding ${JSON.stringify(route.bindingName)} configuration: ${error instanceof Error ? error.message : String(error)}`);
    }
    const target = newDurableTriggerTarget(project.triggerInbox(key), key, route.target, { decodePayload: decodeMemberChangedEventPayload, ...(options.logger ? { logger: options.logger } : {}) });
    return { bindingName: route.bindingName, configuration, target, cursor: project.pollCursorStore(key) };
  });
  return newMemberTriggerRunner(connection, durableRoutes, options);
}

/** Configuration of one in-memory memberChanged Trigger. */
export interface MemberChangedTriggerConfig {
  /** Mailchimp connection from newProjectConnection or newConnection with a MailchimpClient. */ connection: Connection;
  /** Static binding name. */ bindingName: string;
  /** Selects the watched audience, which is required, and the delivered statuses. */ configuration: MemberChangedTriggerConfiguration;
  /** Routes accepted changes into the application. */ target: TriggerTarget<MemberChangedEvent>;
  /** Receives the Trigger's records; omitted uses defaultTriggerLogger. */ logger?: TriggerLogger;
}

/**
 * Create a single memberChanged TriggerRunner, as Go's generated NewMemberChangedTrigger does. It has no
 * project storage, so it keeps the cursor in memory and records a new baseline on every start; changes made
 * while stopped are lost. Use newProjectMemberTriggerRunner for durable delivery.
 */
export function newMemberChangedTrigger(config: MemberChangedTriggerConfig): TriggerRunner {
  const client = mailchimpClientOf(config.connection);
  if (!config.bindingName?.trim() || !config.target) throw new TypeError('Mailchimp memberChanged binding name and target are required');
  const configuration = parseConfiguration(config.configuration);
  const binding = { connection: config.connection.reference, trigger: MemberChangedTriggerDefinition.trigger, name: config.bindingName };
  const source = newBindingTriggerSource<MemberChangedEvent>({
    definition: MemberChangedTriggerDefinition, binding, cursor: newMemoryCursorStore(),
    fetcher: new MemberChangeFetcher(client, config.connection.reference, MemberChangedTriggerDefinition.trigger, configuration), snapshot: MemberSnapshotOptions,
  }, pollSchedule(client), config.logger);
  return newTrigger({ definition: MemberChangedTriggerDefinition, binding, source, target: config.target, ...(config.logger ? { logger: config.logger } : {}) });
}

/** Decode a replayed inbox payload, written by a Go or TypeScript replica; throws for another payload. */
export function decodeMemberChangedEventPayload(payloadJSON: string): MemberChangedEvent {
  const parsed = MemberChangedEventSchema.safeParse(JSON.parse(payloadJSON));
  if (!parsed.success) throw new TypeError('stored Mailchimp memberChanged payload is invalid');
  return parsed.data;
}

function parseConfiguration(configuration: MemberChangedTriggerConfiguration | undefined): MemberChangedTriggerConfiguration {
  const parsed = MemberChangedTriggerConfigurationSchema.safeParse(configuration ?? {});
  if (!parsed.success) throw new TypeError(`mailchimp memberChanged configuration: ${parsed.error.issues.map(issue => issue.message).join('; ')}`);
  return parsed.data;
}

function mailchimpClientOf(connection: Connection): MailchimpClient {
  if (!(connection?.operations instanceof MailchimpClient)) throw new TypeError('Mailchimp memberChanged Trigger requires a connection from newProjectConnection or newConnection with a MailchimpClient');
  return connection.operations;
}

// Omitted maxBackoffMs uses the kit's cap, which grows with a long poll interval.
function pollSchedule(client: MailchimpClient): PollSchedule { return { intervalMs: client.pollIntervalMs }; }
