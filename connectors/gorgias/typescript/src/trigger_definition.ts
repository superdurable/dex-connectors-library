// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mustTriggerBindingDefinition, type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerDefinition } from '@superdurable/connectors-sdk';
import { z } from 'zod';
import { ConnectorID } from './generated/definitions.js';

/**
 * Every Gorgias event type whose object is a ticket that ticketChanged reads, spelled as Gorgias's event log
 * spells it, in the order each poll requests them. Message events, such as ticket-message-created, are not
 * among them: their object is a message, and Gorgias documents no ticket ID for it.
 */
export const TicketEventTypes = [
  'ticket-created', 'ticket-updated', 'ticket-closed', 'ticket-reopened',
  'ticket-assigned', 'ticket-unassigned', 'ticket-team-assigned', 'ticket-team-unassigned',
  'ticket-tags-added', 'ticket-tags-removed', 'ticket-subject-updated', 'ticket-customer-updated',
  'ticket-snoozed', 'ticket-self-unsnoozed', 'ticket-marked-spam', 'ticket-unmarked-spam',
  'ticket-trashed', 'ticket-untrashed', 'ticket-deleted', 'ticket-merged', 'ticket-split',
  'ticket-sla-policy-assigned',
] as const;
/** A Gorgias ticket event type that ticketChanged reads. */
export type TicketEventType = (typeof TicketEventTypes)[number];

/** Report whether value is one of TicketEventTypes. */
export function isTicketEventType(value: string): value is TicketEventType { return (TicketEventTypes as readonly string[]).includes(value); }

/**
 * The ticketChanged binding configuration saved in the project configuration. Studio setup commands cannot send a
 * Gorgias API key over HTTP Basic to a per-account host, so the binding has no picker; every field is optional.
 */
export const TicketChangedTriggerConfigurationSchema = z.strictObject({
  /**
   * Limits deliveries to these event types, such as ["ticket-created"] to start a Flow only for new tickets. Each
   * must be one of TicketEventTypes. Empty or omitted delivers every one of them. Skipped events still move the
   * cursor, so adding a type later never delivers events the binding already passed.
   */
  eventTypes: z.array(z.string()).nullable().optional(),
}).superRefine((configuration, context) => {
  const seen = new Set<string>();
  for (const eventType of configuration.eventTypes ?? []) {
    if (!isTicketEventType(eventType)) context.addIssue({ code: 'custom', message: `ticketChanged eventTypes entry ${JSON.stringify(eventType)} is not a ticket event type that ticketChanged reads` });
    else if (seen.has(eventType)) context.addIssue({ code: 'custom', message: `ticketChanged eventTypes lists ${JSON.stringify(eventType)} twice` });
    seen.add(eventType);
  }
});
/** The validated ticketChanged binding configuration. */
export type TicketChangedTriggerConfiguration = z.infer<typeof TicketChangedTriggerConfigurationSchema>;

/** Report whether the binding delivers eventType: every read type when eventTypes is empty or omitted. */
export function deliversTicketEventType(configuration: TicketChangedTriggerConfiguration, eventType: string): boolean {
  const eventTypes = configuration.eventTypes ?? [];
  return eventTypes.length === 0 ? isTicketEventType(eventType) : eventTypes.includes(eventType);
}

/**
 * One Gorgias ticket event read from the account's event log, GET /api/events; field names match the Go
 * connector. It names the ticket and what happened, not the ticket's fields: read the ticket with getTicket for
 * its current status, tags, customer, and messages. One action in Gorgias can record several events, such as
 * ticket-updated and ticket-tags-added, and each arrives as its own delivery.
 */
export const TicketChangedEventSchema = z.object({
  /** Gorgias's event ID, which also names the Trigger event. */ eventId: z.number().int(),
  /** The Gorgias event type, such as ticket-created. */ eventType: z.string(),
  /** The ticket the event is about, the event's object_id. */ ticketId: z.number().int(),
  /** The event's created_datetime in UTC, as Go's RFC 3339 rendering of a time.Time. */ occurredAt: z.string(),
  /** The Gorgias user who caused the event; omitted when Gorgias recorded none, such as for a rule. */ userId: z.number().int().optional(),
  /** Groups the events Gorgias recorded for one chain of actions, such as a rule's changes; omitted when empty. */ context: z.string().optional(),
});
/** One Gorgias ticket event. */
export type TicketChangedEvent = z.infer<typeof TicketChangedEventSchema>;

/** Manifest definition of ticketChanged. */
export const TicketChangedTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: 'ticketChanged' },
  description: 'Receive each Gorgias ticket event, such as ticket-created, ticket-updated, or ticket-closed, recorded after the binding first started, read by polling the account\'s event log in creation order; the first run records a baseline and delivers no earlier events.',
};

/** One static application binding of ticketChanged. */
export interface TicketChangedTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Connector Studio configuration units; ticketChanged has none, so omit it. */ configurationUI?: ConnectorConfigurationUI;
}

/** Declare a static ticketChanged binding for Flow Definition tooling and Dex Web. */
export function defineTicketChangedTriggerBinding(config: TicketChangedTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: TicketChangedTriggerDefinition, ...config });
}
