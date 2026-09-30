// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";

/** CalendarAccessRole is Google's calendarList accessRole, from least to most access. */
export type CalendarAccessRole = "freeBusyReader" | "reader" | "writer" | "owner";

export interface CalendarListEntry {
  id: string;
  name: string;
  accessRole: CalendarAccessRole;
  timeZone: string;
  isPrimary: boolean;
}

const accessRoles: readonly CalendarAccessRole[] = ["freeBusyReader", "reader", "writer", "owner"];

/** parseCalendarPage keeps only entries with an ID, a known access role, and a display name. */
export function parseCalendarPage(value: Record<string, unknown>): {calendars: CalendarListEntry[]; nextPageToken: string} {
  const calendars = array(value.items).flatMap((item): CalendarListEntry[] => {
    if (!record(item) || !text(item.id) || !isAccessRole(item.accessRole)) return [];
    const name = text(item.summaryOverride) ? item.summaryOverride : text(item.summary) ? item.summary : item.id;
    return [{id: item.id, name, accessRole: item.accessRole, timeZone: text(item.timeZone) ? item.timeZone : "", isPrimary: item.primary === true}];
  });
  return {calendars, nextPageToken: text(value.nextPageToken) ? value.nextPageToken : ""};
}

/**
 * minimumAccessRole is the least calendarList access an operation needs: event writes need
 * writer, event reads need reader, and free/busy needs only freeBusyReader.
 */
export function minimumAccessRole(target: ConnectorStudioConfigurationUnitTarget): CalendarAccessRole {
  if (target.scope.kind !== "operation") return "reader";
  switch (target.scope.operationId) {
    case "createEvent":
    case "updateEvent":
      return "writer";
    case "queryFreeBusy":
      return "freeBusyReader";
    default:
      return "reader";
  }
}

function isAccessRole(value: unknown): value is CalendarAccessRole {
  return typeof value === "string" && (accessRoles as readonly string[]).includes(value);
}
function array(value: unknown): unknown[] { return Array.isArray(value) ? value : []; }
function record(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function text(value: unknown): value is string { return typeof value === "string" && value.length > 0; }
