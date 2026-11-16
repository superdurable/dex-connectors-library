// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mustTriggerBindingDefinition, type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerDefinition } from '@superdurable/connectors-sdk';
import { z } from 'zod';
import { ConnectorID } from './generated/definitions.js';
import { TicketSchema, type Ticket } from './generated/models.js';

/** Zoho Desk department picker UI unit, which stores the department ID and name. */ export const UIUnitDepartmentPicker = 'departmentPicker';
/** Department picker output port holding the numeric department ID. */ export const UIDepartmentPickerPortDepartmentID = 'departmentId';
/** Department picker output port holding the department name, for display only. */ export const UIDepartmentPickerPortDepartmentName = 'departmentName';

/**
 * A ticket the Trigger had not seen that was created inside the window the poll read, so it is new since the
 * previous poll. Changes made to a new ticket before the poll read it, such as an assignment rule, arrive inside
 * this one delivery.
 */
export const TicketChangeCreated = 'created';
/**
 * Any other change: a status, priority, assignee, department, or field change, a new thread or comment that moves
 * the ticket's modified time, or a ticket created before the baseline.
 */
export const TicketChangeUpdated = 'updated';
/** The derived kind of one ticketChanged delivery; Zoho Desk ticket search reports no change type. */
export type TicketChangeKind = typeof TicketChangeCreated | typeof TicketChangeUpdated;

/**
 * The ticketChanged binding configuration that the department picker saves in Dex Web. Changing the department of
 * a binding that already polled makes its next poll record a new baseline for the new department.
 */
export interface TicketChangedTriggerConfiguration {
  /**
   * The watched department's numeric Zoho Desk ID from the department picker, such as 1892000000006907; blank or
   * omitted watches every department the connection's agent can see.
   */
  departmentId?: string | undefined;
  /** The picked department's name, kept for display only. */ departmentName?: string | undefined;
}

/**
 * Decodes a saved ticketChanged configuration strictly; unknown fields are rejected and null reads as blank. It
 * does not validate values; validateTicketChangedTriggerConfiguration does, as every runner and Trigger factory does.
 */
export const TicketChangedTriggerConfigurationSchema: z.ZodType<TicketChangedTriggerConfiguration> = z.strictObject({
  departmentId: z.string().nullable().optional().transform(value => value ?? undefined),
  departmentName: z.string().nullable().optional().transform(value => value ?? undefined),
});

/** Reject a department ID that is not a numeric Zoho Desk ID, as the Go connector's Validate does; blank is valid. */
export function validateTicketChangedTriggerConfiguration(configuration: TicketChangedTriggerConfiguration): void {
  const departmentId = configuration?.departmentId ?? '';
  if (departmentId && !/^[0-9]{1,20}$/.test(departmentId)) throw new Error('ticketChanged departmentId must be a numeric Zoho Desk ID such as 1892000000042034');
}

/**
 * One created or changed ticket read from Zoho Desk ticket search. Several changes to one ticket between two polls
 * arrive as one delivery of its latest version. Tickets moved to the trash, deleted, or archived leave the search
 * results and are not delivered. Field names match the Go connector.
 */
export interface TicketChangedEvent {
  /** The connection's Zoho Desk organization. */ orgId: string;
  /** The watched department, omitted when the binding watches every department. */ departmentId?: string | undefined;
  /** The derived change kind; see TicketChangeKind. */ changeKind: TicketChangeKind;
  /**
   * The ticket as the search returned it, without its descriptionHtml. Read it with getTicket for the description,
   * threads, and comments.
   */
  ticket: Ticket;
}

/** Decodes a ticketChanged payload that this package or the Go connector recorded. */
export const TicketChangedEventSchema: z.ZodType<TicketChangedEvent> = z.object({
  orgId: z.string(),
  departmentId: z.string().optional(),
  changeKind: z.enum([TicketChangeCreated, TicketChangeUpdated]),
  ticket: TicketSchema,
});

/** Manifest definition of ticketChanged. */
export const TicketChangedTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: 'ticketChanged' },
  description: 'Receive each Zoho Desk ticket created or changed in the organization, optionally in one department, read by polling ticket search in modified-time order; the first run records a baseline and delivers no existing tickets.',
};

/** One static application binding of ticketChanged. */
export interface TicketChangedTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Connector Studio configuration units for the binding, such as the departmentPicker unit. */ configurationUI?: ConnectorConfigurationUI | undefined;
}

/** Declare a static ticketChanged binding for Flow Definition tooling and Dex Web. */
export function defineTicketChangedTriggerBinding(config: TicketChangedTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({
    definition: TicketChangedTriggerDefinition, connectionName: config.connectionName, bindingName: config.bindingName,
    ...(config.configurationUI ? { configurationUI: config.configurationUI } : {}),
  });
}
