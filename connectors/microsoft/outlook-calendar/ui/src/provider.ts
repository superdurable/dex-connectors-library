// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectorStudioConfigurationUnitTarget, ConnectorStudioConnection } from "@superdurable/dex-connectors-react";

/** appOnlyAuthMethodID is the manifest method whose calls address the configured mailbox. */
export const appOnlyAuthMethodID = "app-only";
export const calendarsListCapability = "microsoft.outlook-calendar.calendars-list";

export interface CalendarListEntry {
  id: string;
  name: string;
  canEdit: boolean;
  isDefault: boolean;
  ownerAddress: string;
}

/** CalendarListCommand is the read-only Studio command that lists the connection's calendars. */
export type CalendarListCommand =
  | {commandId: "listCalendars"; parameters: Record<string, string>}
  | {commandId: "listMailboxCalendars"; parameters: {mailbox: string}};

/**
 * parseCalendarPage keeps only entries with an ID. Graph documents its nextLink as opaque,
 * so the picker never follows it; isTruncated points the user to manual entry instead.
 */
export function parseCalendarPage(value: Record<string, unknown>): {calendars: CalendarListEntry[]; isTruncated: boolean} {
  const calendars = array(value.value).flatMap((item): CalendarListEntry[] => {
    if (!record(item) || !text(item.id)) return [];
    const owner = record(item.owner) && text(item.owner.address) ? item.owner.address : "";
    return [{
      id: item.id, name: text(item.name) ? item.name : item.id, canEdit: item.canEdit === true,
      isDefault: item.isDefaultCalendar === true, ownerAddress: owner,
    }];
  });
  return {calendars, isTruncated: text(value["@odata.nextLink"])};
}

/** shouldListOnlyEditableCalendars reports that the Step writes events, so read-only calendars are hidden. */
export function shouldListOnlyEditableCalendars(target: ConnectorStudioConfigurationUnitTarget): boolean {
  return target.scope.kind === "operation" && (target.scope.operationId === "createEvent" || target.scope.operationId === "updateEvent");
}

/**
 * selectCalendarListCommand lists /me/calendars for a delegated connection and
 * /users/{mailbox}/calendars for an app-only one, whose mailbox the connection form saved.
 */
export function selectCalendarListCommand(connection: Pick<ConnectorStudioConnection, "authMethodIds" | "configuration">): CalendarListCommand | {error: string} {
  const mailbox = connection.configuration.mailbox;
  const isAppOnly = connection.authMethodIds.includes(appOnlyAuthMethodID) || (connection.authMethodIds.length === 0 && text(mailbox));
  if (!isAppOnly) return {commandId: "listCalendars", parameters: {}};
  if (!text(mailbox) || mailbox.trim() !== mailbox) {
    return {error: "Save the mailbox in the connection form before choosing a calendar."};
  }
  return {commandId: "listMailboxCalendars", parameters: {mailbox}};
}

function array(value: unknown): unknown[] { return Array.isArray(value) ? value : []; }
function record(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function text(value: unknown): value is string { return typeof value === "string" && value.length > 0; }
