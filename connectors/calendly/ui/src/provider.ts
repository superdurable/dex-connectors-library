// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { CalendlyEventType } from "./units.js";

const userURIPrefix = "https://api.calendly.com/users/";
const eventTypeURIPrefix = "https://api.calendly.com/event_types/";

export interface CalendlyEventTypePage { eventTypes: CalendlyEventType[]; nextPageToken: string; }

/** parseCalendlyCurrentUserURI returns the connected user's URI from GET /users/me. */
export function parseCalendlyCurrentUserURI(value: Record<string, unknown>): string {
  assertCalendlyOK(value);
  const resource = record(value.resource) ? value.resource : {};
  if (!text(resource.uri) || !resource.uri.startsWith(userURIPrefix)) throw new Error("Calendly did not return the connected user");
  return resource.uri;
}

/** parseCalendlyEventTypePage keeps event types with a Calendly event type URI and a name. */
export function parseCalendlyEventTypePage(value: Record<string, unknown>): CalendlyEventTypePage {
  assertCalendlyOK(value);
  const eventTypes = array(value.collection).flatMap((item) => {
    if (!record(item) || !text(item.uri) || !item.uri.startsWith(eventTypeURIPrefix) || !text(item.name)) return [];
    const durationMinutes = typeof item.duration === "number" && Number.isFinite(item.duration) ? item.duration : undefined;
    return [{uri: item.uri, name: item.name, durationMinutes, isActive: item.active !== false}];
  });
  const pagination = record(value.pagination) ? value.pagination : {};
  return {eventTypes, nextPageToken: text(pagination.next_page_token) ? pagination.next_page_token.trim() : ""};
}

/** isCalendlyEventTypeURI reports whether a manually entered value is an event type URI. */
export function isCalendlyEventTypeURI(value: string): boolean {
  return /^https:\/\/api\.calendly\.com\/event_types\/[A-Za-z0-9_-]{1,64}$/.test(value);
}

// assertCalendlyOK reports an error object in connector words; Calendly's own text never reaches the frame.
function assertCalendlyOK(value: Record<string, unknown>) {
  if (text(value.title) && !("collection" in value) && !("resource" in value)) throw new Error("Calendly returned an error instead of the expected data");
}

function array(value: unknown): unknown[] { return Array.isArray(value) ? value : []; }
function record(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function text(value: unknown): value is string { return typeof value === "string" && value.length > 0; }
