// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mustTriggerBindingDefinition, type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerDefinition } from '@superdurable/connectors-sdk';
import { z } from 'zod';
import { ConnectorID } from './generated/definitions.js';
import { TicketSchema } from './generated/models.js';

/** A ticket created inside the poll window that the binding has not delivered before. */
export const TicketChangeCreated = 'created';
/** Any later change that moved the ticket's updated_at, such as a status change or a new reply or note. */
export const TicketChangeUpdated = 'updated';
/** The derived kind of one ticketChanged delivery; Freshdesk's ticket list reports no change type. */
export type TicketChangeKind = typeof TicketChangeCreated | typeof TicketChangeUpdated;

/**
 * The ticketChanged binding configuration saved in the project configuration. Freshdesk setup commands
 * cannot send the agent's HTTP Basic API key, so the binding has no provider picker; every field is optional.
 */
export const TicketChangedTriggerConfigurationSchema = z.strictObject({
  /**
   * Limits deliveries to these kinds, such as ["created"]. Empty or omitted delivers both. Skipped changes still
   * move the cursor, so adding a kind later never delivers changes the binding already passed.
   */
  changeKinds: z.array(z.string()).nullable().optional(),
}).superRefine((configuration, context) => {
  const seen = new Set<string>();
  for (const kind of configuration.changeKinds ?? []) {
    if (kind !== TicketChangeCreated && kind !== TicketChangeUpdated) context.addIssue({ code: 'custom', message: `ticketChanged changeKinds entry ${JSON.stringify(kind)} must be created or updated` });
    else if (seen.has(kind)) context.addIssue({ code: 'custom', message: `ticketChanged changeKinds lists ${JSON.stringify(kind)} twice` });
    seen.add(kind);
  }
});
/** The validated ticketChanged binding configuration. */
export type TicketChangedTriggerConfiguration = z.infer<typeof TicketChangedTriggerConfigurationSchema>;

/**
 * One created or updated ticket read from Freshdesk's List All Tickets endpoint; field names match the Go
 * connector. Several changes between two polls arrive as one delivery of the latest version. Spam and deleted
 * tickets are not listed, so a ticket moved to spam or trash is not delivered.
 */
export const TicketChangedEventSchema = z.object({
  /** created or updated; see TicketChangeKind. */ changeKind: z.enum([TicketChangeCreated, TicketChangeUpdated]),
  /** The ticket as the list returned it, without its description; read it with getTicket for its text. */ ticket: TicketSchema,
});
/** One created or updated Freshdesk ticket. */
export type TicketChangedEvent = z.infer<typeof TicketChangedEventSchema>;

/** Manifest definition of ticketChanged. */
export const TicketChangedTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: 'ticketChanged' },
  description: 'Receive each Freshdesk ticket created or updated after the binding first started, read by polling the ticket list in updated_at order; the first run records a baseline and delivers no existing tickets.',
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
