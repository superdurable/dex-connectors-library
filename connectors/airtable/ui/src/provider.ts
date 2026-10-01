// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export interface AirtableBase { id: string; name: string; permissionLevel: string; }
export interface AirtableBasePage { bases: AirtableBase[]; nextOffset: string; }
export interface AirtableTable { id: string; name: string; }

export const baseIDPattern = /^app[0-9A-Za-z]{14}$/;
export const tableIDPattern = /^tbl[0-9A-Za-z]{14}$/;

/** parseAirtableBasePage keeps the reachable bases of one GET /v0/meta/bases page and its offset cursor. */
export function parseAirtableBasePage(value: Record<string, unknown>): AirtableBasePage {
  if (!Array.isArray(value.bases)) throw new Error(airtableErrorText(value, "Airtable returned no base list"));
  const bases = value.bases.flatMap((item) => {
    if (!record(item) || !text(item.id) || !baseIDPattern.test(item.id) || !text(item.name) || item.permissionLevel === "none") return [];
    return [{id: item.id, name: item.name, permissionLevel: text(item.permissionLevel) ? item.permissionLevel : ""}];
  });
  return {bases, nextOffset: text(value.offset) ? value.offset : ""};
}

/** parseAirtableTables keeps each table's ID and name from GET /v0/meta/bases/{baseId}/tables, in Airtable's order. */
export function parseAirtableTables(value: Record<string, unknown>): AirtableTable[] {
  if (!Array.isArray(value.tables)) throw new Error(airtableErrorText(value, "Airtable returned no table list"));
  return value.tables.flatMap((item) => {
    if (!record(item) || !text(item.id) || !tableIDPattern.test(item.id) || !text(item.name)) return [];
    return [{id: item.id, name: item.name}];
  });
}

// airtableErrorText names only Airtable's uppercase error type, never its message text.
function airtableErrorText(value: Record<string, unknown>, fallback: string): string {
  const error = value.error;
  const type = record(error) ? error.type : error;
  return text(type) && /^[A-Z][A-Z0-9_]{1,63}$/.test(type) ? `${fallback} (${type})` : fallback;
}

function record(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function text(value: unknown): value is string { return typeof value === "string" && value.trim().length > 0; }
