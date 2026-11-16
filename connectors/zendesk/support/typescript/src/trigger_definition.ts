// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mustTriggerBindingDefinition, type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerDefinition } from '@superdurable/connectors-sdk';
import { z } from 'zod';
import { ConnectorID } from './generated/definitions.js';
import { TicketSchema } from './generated/models.js';

/** The status the incremental ticket export reports for a deleted ticket; no operation accepts it. */
export const TicketStatusDeleted = 'deleted';
/** A new ticket: its updated_at is within one second of its created_at. */
export const TicketChangeCreated = 'created';
/** Any other change to a ticket that is not deleted, such as a reply, a status or assignee change, or an early edit. */
export const TicketChangeUpdated = 'updated';
/** A ticket the export reports with status deleted; bindings without includeDeleted never receive it. */
export const TicketChangeDeleted = 'deleted';
/** The derived kind of one ticketChanged delivery; Zendesk's export reports no change type. */
export type TicketChangeKind = typeof TicketChangeCreated | typeof TicketChangeUpdated | typeof TicketChangeDeleted;

/**
 * The ticketChanged binding configuration saved in the project configuration. The Trigger watches the whole
 * account, as the connection's administrator sees it; a Flow's accept function narrows deliveries, for example to
 * one group or brand. An empty configuration delivers created and updated tickets only.
 */
export const TicketChangedTriggerConfigurationSchema = z.strictObject({
  /**
   * Also delivers tickets deleted in Zendesk, as deleted. Omitted or false sends exclude_deleted=true, so the
   * export omits deletions. Changing it keeps the binding's position.
   */
  includeDeleted: z.boolean().nullable().optional(),
});
/** The validated ticketChanged binding configuration. */
export type TicketChangedTriggerConfiguration = z.infer<typeof TicketChangedTriggerConfigurationSchema>;

/**
 * One ticket the incremental ticket export reported as created, updated, or deleted after the binding's cursor;
 * field names match the Go connector. Several changes made between two polls arrive as one event carrying the
 * ticket's latest state.
 */
export const TicketChangedEventSchema = z.object({
  /** created, updated, or deleted; see TicketChangeKind. */ changeKind: z.enum([TicketChangeCreated, TicketChangeUpdated, TicketChangeDeleted]),
  /** The ticket as the export returned it, with its description cut at 16 KiB; a deleted ticket has status deleted. */ ticket: TicketSchema,
  /**
   * Zendesk's generated_timestamp as RFC 3339 UTC: the ticket's last change, including system updates that leave
   * updatedAt unchanged, at one-second precision. Omitted when Zendesk omits it.
   */
  generatedAt: z.string().optional(),
});
/** One created, updated, or deleted Zendesk ticket. */
export type TicketChangedEvent = z.infer<typeof TicketChangedEventSchema>;

/** Manifest definition of ticketChanged. */
export const TicketChangedTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: 'ticketChanged' },
  description: "Receive each ticket created, updated, or deleted in the account, read through Zendesk's cursor-based incremental ticket export; the first run records a baseline and delivers no existing tickets. Requires an admin connection.",
};

/** One static application binding of ticketChanged. */
export interface TicketChangedTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Connector Studio configuration units; ticketChanged has none, because Zendesk has no setup bundle, so omit it. */ configurationUI?: ConnectorConfigurationUI;
}

/** Declare a static ticketChanged binding for Flow Definition tooling and Dex Web. */
export function defineTicketChangedTriggerBinding(config: TicketChangedTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: TicketChangedTriggerDefinition, ...config });
}
