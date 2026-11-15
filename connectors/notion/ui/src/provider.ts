// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

/** dataSourceReadCapability is the backend capability both read-only Notion commands declare. */
export const dataSourceReadCapability = "notion.data-sources-read";

const dashedIDPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const compactIDPattern = /^[0-9a-f]{32}$/i;
const trailingIDPattern = /([0-9a-f]{32})$/i;
const notionHosts = ["notion.so", "notion.com", "notion.site"];

export interface NotionDataSource { id: string; name: string; }

/** ExecuteNotionCommand runs one declared read-only command with its path parameters. */
export type ExecuteNotionCommand = (commandId: string, parameters: Record<string, string>) => Promise<Record<string, unknown>>;

/**
 * parseNotionID returns the dashed, lowercase form of a Notion ID, from the dashed or 32-character form or
 * a notion.so, notion.com, or notion.site link whose last path segment ends with the ID, as the Go
 * connector's ParseID does; it returns "" for anything else.
 */
export function parseNotionID(value: string): string {
  const text = value.trim();
  if (/^https:\/\//i.test(text)) {
    let url: URL;
    try { url = new URL(text); } catch { return ""; }
    const host = url.hostname.toLowerCase();
    if (!notionHosts.some((domain) => host === domain || host.endsWith(`.${domain}`))) return "";
    const segments = url.pathname.replace(/^\/+|\/+$/g, "").split("/");
    const last = segments[segments.length - 1] ?? "";
    if (dashedIDPattern.test(last)) return last.toLowerCase();
    const match = trailingIDPattern.exec(last.replaceAll("-", ""));
    return match ? canonicalID(match[1]) : "";
  }
  return canonicalID(text);
}

/** listDatabaseDataSources reads one database and returns the data sources Notion lists under it. */
export async function listDatabaseDataSources(executeCommand: ExecuteNotionCommand, databaseLinkOrID: string): Promise<NotionDataSource[]> {
  const databaseId = parseNotionID(databaseLinkOrID);
  if (databaseId === "") throw new Error("Enter a Notion database link or a 32-character database ID");
  const database = await executeCommand("getDatabase", {databaseId});
  assertNotionObject(database, "database");
  return array(database.data_sources).flatMap((entry) => {
    if (!record(entry) || typeof entry.id !== "string") return [];
    const id = parseNotionID(entry.id);
    if (id === "") return [];
    return [{id, name: text(entry.name) ? entry.name : id}];
  });
}

/** readDataSource reads one pasted data source ID and returns it with its title. */
export async function readDataSource(executeCommand: ExecuteNotionCommand, dataSourceIDText: string): Promise<NotionDataSource> {
  const dataSourceId = parseNotionID(dataSourceIDText);
  if (dataSourceId === "") throw new Error("Enter a data source ID of 32 hexadecimal characters, with or without dashes");
  const dataSource = await executeCommand("getDataSource", {dataSourceId});
  assertNotionObject(dataSource, "data_source");
  const title = array(dataSource.title).map((part) => record(part) && typeof part.plain_text === "string" ? part.plain_text : "").join("");
  return {id: dataSourceId, name: title || dataSourceId};
}

// assertNotionObject reports an error object in connector words; Notion's own message never reaches the frame.
function assertNotionObject(value: Record<string, unknown>, objectType: string) {
  if (value.object === objectType) return;
  if (value.object === "error" && (value.code === "object_not_found" || value.status === 404)) {
    throw new Error("Notion could not find it; share the database with this connection from its ••• menu > Connections");
  }
  throw new Error(`Notion returned no ${objectType.replace("_", " ")}`);
}

function canonicalID(value: string): string {
  if (dashedIDPattern.test(value)) return value.toLowerCase();
  if (!compactIDPattern.test(value)) return "";
  const lower = value.toLowerCase();
  return `${lower.slice(0, 8)}-${lower.slice(8, 12)}-${lower.slice(12, 16)}-${lower.slice(16, 20)}-${lower.slice(20)}`;
}

function array(value: unknown): unknown[] { return Array.isArray(value) ? value : []; }
function record(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function text(value: unknown): value is string { return typeof value === "string" && value.length > 0; }
