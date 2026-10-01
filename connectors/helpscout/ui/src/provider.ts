// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { HelpScoutMailbox } from "./units.js";

export interface HelpScoutMailboxPage { mailboxes: HelpScoutMailbox[]; nextPage: string; }

/**
 * parseHelpScoutMailboxPage keeps inboxes with a positive ID and a name from one GET /v2/mailboxes page;
 * nextPage is the page number to request next, or empty on the last page.
 */
export function parseHelpScoutMailboxPage(value: Record<string, unknown>, requestedPage: number): HelpScoutMailboxPage {
  const embedded = record(value._embedded) ? value._embedded : {};
  if (!Array.isArray(embedded.mailboxes) && !record(value.page)) throw new Error("Help Scout returned an error instead of the inbox list");
  const mailboxes = array(embedded.mailboxes).flatMap((item) => {
    if (!record(item) || !isPositiveInteger(item.id) || !text(item.name)) return [];
    return [{id: item.id, name: item.name, email: text(item.email) ? item.email : undefined}];
  });
  const links = record(value._links) ? value._links : {};
  return {mailboxes, nextPage: record(links.next) ? String(requestedPage + 1) : ""};
}

/** parseMailboxID reads a manually entered inbox ID, or undefined when it is not a positive integer. */
export function parseMailboxID(value: string): number | undefined {
  if (!/^[1-9][0-9]{0,15}$/.test(value)) return undefined;
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) ? parsed : undefined;
}

function array(value: unknown): unknown[] { return Array.isArray(value) ? value : []; }
function record(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function text(value: unknown): value is string { return typeof value === "string" && value.length > 0; }
function isPositiveInteger(value: unknown): value is number { return typeof value === "number" && Number.isSafeInteger(value) && value > 0; }
