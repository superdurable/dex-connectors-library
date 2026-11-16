// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { HiverInbox } from "./units.js";

export interface HiverInboxPage { inboxes: HiverInbox[]; nextPageToken: string; }

const hiverIDPattern = /^[A-Za-z0-9_-]{1,64}$/;
const pageTokenPattern = /^[A-Za-z0-9+/=_.-]{1,4096}$/;

/**
 * parseHiverInboxPage keeps inboxes with a valid ID from one GET /v1/inboxes page. Hiver documents IDs as
 * strings in some responses and numbers in others, so both become strings. nextPageToken is Hiver's
 * next_page token, or empty on the last page.
 */
export function parseHiverInboxPage(value: Record<string, unknown>): HiverInboxPage {
  const data = record(value.data) ? value.data : undefined;
  if (!data || !Array.isArray(data.results)) throw new Error("Hiver returned an error instead of the inbox list");
  const inboxes = data.results.flatMap((item) => {
    if (!record(item)) return [];
    const id = typeof item.id === "number" && Number.isSafeInteger(item.id) ? String(item.id) : item.id;
    if (typeof id !== "string" || !hiverIDPattern.test(id)) return [];
    return [{id, name: text(item.display_name) ? item.display_name : id, email: text(item.email) ? item.email : undefined}];
  });
  const pagination = record(data.pagination) ? data.pagination : {};
  const next = pagination.next_page;
  return {inboxes, nextPageToken: typeof next === "string" && pageTokenPattern.test(next) ? next : ""};
}

/** parseInboxID reads a manually entered inbox ID, or undefined when it is not a Hiver ID. */
export function parseInboxID(value: string): string | undefined {
  return hiverIDPattern.test(value) ? value : undefined;
}

function record(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function text(value: unknown): value is string { return typeof value === "string" && value.trim().length > 0; }
