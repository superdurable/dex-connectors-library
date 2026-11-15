// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  mustTriggerBindingDefinition, newTrigger,
  type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerDefinition, type TriggerLogger, type TriggerRunner, type TriggerTarget,
} from '@superdurable/connectors-sdk';
import { newBindingPoller, newBindingTriggerSource, newMemoryCursorStore, newRunner, type CursorStore, type PollSchedule, type Poller, type Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { ObjectNotFoundError, decodeTriggerConfiguration, newDurableTriggerTarget, parseTriggerPayloadJSON, type LoadedProject, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { CalendarClient } from './client.js';
import { ConnectorID } from './generated/definitions.js';
import type { Connection } from './generated/connection.js';
import { EventSchema } from './generated/models.js';
import { EventSyncFetcher } from './event_sync.js';

/** The calendar an eventChanged binding watches when it saves no calendar. */
export const DefaultTriggerCalendarID = 'primary';
/** Calendar picker UI unit, which saves calendarId and calendarName. */ export const UIUnitCalendarPicker = 'calendarPicker';
/** Calendar picker output port for the calendar ID. */ export const UICalendarPickerPortCalendarID = 'calendarId';
/** Calendar picker output port for the calendar display name. */ export const UICalendarPickerPortCalendarName = 'calendarName';

/** The derived kind of one eventChanged delivery. */
export const EventChangeKind = {
  /** A new event: Google's updated time is within one second of its created time. */ Created: 'created',
  /** Any other change to an event that is not cancelled, including an edit made before the next poll read its creation. */ Updated: 'updated',
  /** A deleted event or a cancelled instance of a recurring event. */ Cancelled: 'cancelled',
} as const;
/** created, updated, or cancelled. */
export type EventChangeKind = typeof EventChangeKind[keyof typeof EventChangeKind];

/**
 * The eventChanged binding configuration the calendar picker saves in Dex Web. A blank calendarId watches
 * DefaultTriggerCalendarID, the authorized account's primary calendar; one with surrounding whitespace is
 * refused. Changing the calendar of a binding that already polled makes its next poll record a new baseline.
 */
export const EventChangedTriggerConfigurationSchema = z.strictObject({
  /** The watched calendar ID from the calendar picker, such as team@group.calendar.google.com; blank watches primary. */
  calendarId: z.string().refine(value => value === value.trim(), 'eventChanged calendarId must not have surrounding whitespace').optional(),
  /** The picked calendar's display name, kept for display only. */ calendarName: z.string().optional(),
});
/** The calendar one eventChanged binding watches. */
export type EventChangedTriggerConfiguration = z.infer<typeof EventChangedTriggerConfigurationSchema>;

/** Return the configured calendar ID, or DefaultTriggerCalendarID when it is blank. */
export function watchedCalendarId(configuration: EventChangedTriggerConfiguration): string {
  return configuration.calendarId || DefaultTriggerCalendarID;
}

/**
 * One created, updated, or cancelled event read through incremental sync; field names match the Go
 * connector. Recurring events are not expanded: a series change arrives once, and a changed or cancelled
 * single instance arrives as its own event with recurringEventId and originalStartTime set. A cancelled
 * event is only guaranteed to carry its id.
 */
export const EventChangedEventSchema = z.strictObject({
  /** The watched calendar. */ calendarId: z.string(),
  /** The derived change kind. */ changeKind: z.enum([EventChangeKind.Created, EventChangeKind.Updated, EventChangeKind.Cancelled]),
  /** The event as Google returned it in the sync page. */ event: EventSchema,
});
/** One created, updated, or cancelled event. */
export type EventChangedEvent = z.infer<typeof EventChangedEventSchema>;

/** Manifest definition of eventChanged. */
export const EventChangedTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: 'eventChanged' },
  description: 'Receive each event created, updated, or cancelled on one calendar, read through Google Calendar incremental sync; the first run records a baseline and delivers no existing events.',
};

/** One static application binding of eventChanged. */
export interface EventChangedTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Connector Studio configuration units for the binding, such as the calendarPicker unit. */ configurationUI: ConnectorConfigurationUI;
}

/** Declare a static eventChanged binding for Flow Definition tooling and Dex Web. */
export function defineEventChangedTriggerBinding(config: EventChangedTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: EventChangedTriggerDefinition, ...config });
}

/** Decode a replayed change, including one a Go replica recorded. */
export function decodeEventChangedPayload(_payloadJSON: string, payloadBytes: Uint8Array): EventChangedEvent {
  return EventChangedEventSchema.parse(parseTriggerPayloadJSON(payloadBytes));
}

function calendarClientOf(connection: Connection): CalendarClient {
  if (!(connection?.operations instanceof CalendarClient)) throw new TypeError('Google Calendar Triggers require a connection from newConnection with a CalendarClient or newProjectConnection, not a mock connection');
  return connection.operations;
}

function pollScheduleOf(client: CalendarClient): PollSchedule {
  // An omitted maxBackoffMs uses the kit's cap, which grows with a long poll interval.
  return { intervalMs: client.pollIntervalMs };
}

/** Binds one eventChanged configuration to its application target and cursor. */
export interface EventChangedTriggerRoute {
  /** The stable application binding name, unique within the runner. */ bindingName: string;
  /** Selects the watched calendar. */ configuration: EventChangedTriggerConfiguration;
  /**
   * Routes each change into the application. A target from newDurableTriggerTarget records each change
   * before the cursor commits past it; any other target consumes it first.
   */
  target: TriggerTarget<EventChangedEvent>;
  /**
   * Stores the binding's sync token, such as LoadedProject.pollCursorStore. Omitted keeps it in memory, so
   * every process start records a new baseline and changes made while stopped are lost.
   */
  cursor?: CursorStore | undefined;
}

/**
 * Create one poll runner for the eventChanged bindings polled through one connection. run replays each
 * durable target, records a baseline for a binding without a cursor, then polls every binding each
 * pollInterval with jitter. A 410 Gone records a new baseline and logs a WARN record; changes made between
 * the last poll and the new baseline are not delivered. A 429 or a 403 rate-limit or quota reason pauses
 * every binding for Retry-After, or backs off exponentially without it; other failures back off per binding
 * up to 15 minutes. Delivery is at least once, and each change keeps one Trigger event ID across refetches.
 * logger receives records with binding identity, never payloads or credentials. Throws for no routes, a
 * blank or repeated binding name, a missing target, an invalid configuration, or a mock connection.
 */
export function newEventTriggerRunner(connection: Connection, routes: EventChangedTriggerRoute[], logger?: TriggerLogger): Runner {
  const client = calendarClientOf(connection);
  if (!routes?.length) throw new TypeError('Google Calendar eventChanged routes are required');
  const pollers: Poller[] = routes.map(route => {
    if (!route.bindingName?.trim() || !route.target) throw new TypeError('Google Calendar eventChanged binding name and target are required');
    const configuration = parseEventChangedTriggerConfiguration(route.configuration);
    return newBindingPoller({
      definition: EventChangedTriggerDefinition,
      binding: { connection: connection.reference, trigger: EventChangedTriggerDefinition.trigger, name: route.bindingName },
      cursor: route.cursor ?? newMemoryCursorStore(), fetcher: new EventSyncFetcher(client, connection.reference, watchedCalendarId(configuration)), target: route.target,
    });
  });
  return newRunner({ connection: connection.reference, schedule: pollScheduleOf(client), pollers, ...(logger ? { logger } : {}) });
}

/** Selects one eventChanged binding saved in the project configuration. */
export interface ProjectEventChangedTriggerRoute {
  /** The saved binding's name. */ bindingName: string;
  /** Routes each change into the application, usually newDexFlowTriggerTarget. */ target: TriggerTarget<EventChangedEvent>;
}

/**
 * Create a durable runner for eventChanged bindings saved in the loaded project. connection is the project's
 * connection from newProjectConnection; the caller shares it with the Flow. Each binding's saved calendar is
 * loaded (an unsaved binding watches primary), and each route records changes in its binding's project
 * Trigger inbox before its sync token commits in the binding's project poll cursor, so a restarted or
 * replaced replica neither misses nor repeats a change. logger also receives the inboxes' records. Throws
 * for an invalid saved configuration or route, as newEventTriggerRunner does.
 */
export function newProjectEventTriggerRunner(project: LoadedProject, connection: Connection, routes: ProjectEventChangedTriggerRoute[], logger?: TriggerLogger): Runner {
  if (!project) throw new TypeError('Google Calendar Trigger runner requires the loaded project configuration');
  calendarClientOf(connection);
  const triggerName = EventChangedTriggerDefinition.trigger.triggerName;
  return newEventTriggerRunner(connection, (routes ?? []).map(route => {
    if (!route.target) throw new TypeError(`Google Calendar eventChanged binding "${route.bindingName}" target is required`);
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName: connection.reference.name, triggerName, bindingName: route.bindingName };
    let configuration: EventChangedTriggerConfiguration = {};
    try { configuration = decodeTriggerConfiguration(project.configuration, key, EventChangedTriggerConfigurationSchema); }
    catch (error) {
      // An unsaved binding watches the primary calendar, as a blank picker does.
      if (!(error instanceof ObjectNotFoundError)) throw new TypeError(`google-calendar eventChanged binding "${route.bindingName}" configuration: ${error instanceof Error ? error.message : 'invalid'}`);
    }
    const target = newDurableTriggerTarget(project.triggerInbox(key), key, route.target, { decodePayload: decodeEventChangedPayload, ...(logger ? { logger } : {}) });
    return { bindingName: route.bindingName, configuration, target, cursor: project.pollCursorStore(key) };
  }), logger);
}

/** Configuration of one eventChanged Trigger with an in-memory cursor. */
export interface EventChangedTriggerConfig {
  /** Google Calendar connection from newProjectConnection or newConnection with a CalendarClient. */ connection: Connection;
  /** Static connection name; must equal the connection's name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** The watched calendar. */ configuration: EventChangedTriggerConfiguration;
  /** Routes accepted events into the application. */ target: TriggerTarget<EventChangedEvent>;
  /** Receives the runner's records; omitted uses defaultTriggerLogger. */ logger?: TriggerLogger | undefined;
}

/**
 * Create one eventChanged binding's TriggerRunner, as Go's generated NewEventChangedTrigger does. It has no
 * project storage, so it keeps the cursor in memory and records a new baseline on every start; use
 * newProjectEventTriggerRunner for a durable cursor and inbox. Throws for a mismatched connection name, an
 * invalid configuration, or a mock connection.
 */
export function newEventChangedTrigger(config: EventChangedTriggerConfig): TriggerRunner {
  if (config.connectionName !== config.connection.reference.name) throw new TypeError(`google-calendar connector trigger connectionName "${config.connectionName}" must equal its connection's name "${config.connection.reference.name}"`);
  const client = calendarClientOf(config.connection);
  const configuration = parseEventChangedTriggerConfiguration(config.configuration);
  const binding = { connection: config.connection.reference, trigger: EventChangedTriggerDefinition.trigger, name: config.bindingName };
  const source = newBindingTriggerSource({
    definition: EventChangedTriggerDefinition, binding, cursor: newMemoryCursorStore(), fetcher: new EventSyncFetcher(client, config.connection.reference, watchedCalendarId(configuration)),
  }, pollScheduleOf(client), config.logger);
  return newTrigger({ definition: EventChangedTriggerDefinition, binding, source, target: config.target, ...(config.logger ? { logger: config.logger } : {}) });
}

function parseEventChangedTriggerConfiguration(configuration: EventChangedTriggerConfiguration): EventChangedTriggerConfiguration {
  const parsed = EventChangedTriggerConfigurationSchema.safeParse(configuration ?? {});
  if (!parsed.success) throw new TypeError(`google-calendar eventChanged configuration: ${parsed.error.issues[0]?.message ?? 'invalid'}`);
  return parsed.data;
}
