// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export interface ExcelWorkbook { driveId: string; workbookId: string; name: string; }
export interface ExcelWorkbookPage { workbooks: ExcelWorkbook[]; nextSkipToken: string; }
export interface ExcelWorksheet { id: string; name: string; }
export interface ExcelTable { id: string; name: string; }

/** graphItemIDPattern matches the Graph drive and item IDs the connector's operations accept. */
export const graphItemIDPattern = /^[A-Za-z0-9!_.~-]{1,512}$/;
/** studioPathParameterPattern is what Dex Web accepts in a Studio command path parameter. */
export const studioPathParameterPattern = /^[A-Za-z0-9._~-]+$/;
const workbookNamePattern = /\.(xlsx|xlsm)$/i;
const businessDrivePrefix = "b!";

/** parseWorkbookSearchPage keeps the .xlsx and .xlsm files of one drive search page, resolving shared items to their own drive. */
export function parseWorkbookSearchPage(value: Record<string, unknown>): ExcelWorkbookPage {
  if (!Array.isArray(value.value)) throw new Error("Microsoft Graph returned no file list");
  const workbooks = value.value.flatMap((item) => {
    const workbook = workbookFromDriveItem(item);
    return workbook ? [workbook] : [];
  });
  return {workbooks, nextSkipToken: nextSkipToken(value["@odata.nextLink"])};
}

/** parseSharedWorkbook returns the workbook a sharing link resolves to, or throws when it is not an Excel workbook. */
export function parseSharedWorkbook(value: Record<string, unknown>): ExcelWorkbook {
  const workbook = workbookFromDriveItem(value);
  if (!workbook) throw new Error("The link does not open an .xlsx or .xlsm workbook in OneDrive or SharePoint");
  return workbook;
}

/** parseWorksheets keeps each worksheet's ID and name in workbook order. */
export function parseWorksheets(value: Record<string, unknown>): ExcelWorksheet[] {
  if (!Array.isArray(value.value)) throw new Error("Microsoft Graph returned no worksheet list");
  const worksheets = value.value.flatMap((item) => {
    if (!record(item) || !text(item.id) || !text(item.name)) return [];
    return [{id: item.id, name: item.name, position: typeof item.position === "number" ? item.position : 0}];
  });
  return worksheets.sort((left, right) => left.position - right.position).map(({id, name}) => ({id, name}));
}

/** parseTables keeps each table's ID and name in workbook order. */
export function parseTables(value: Record<string, unknown>): ExcelTable[] {
  if (!Array.isArray(value.value)) throw new Error("Microsoft Graph returned no table list");
  return value.value.flatMap((item) => (record(item) && text(item.id) && text(item.name) ? [{id: item.id, name: item.name}] : []));
}

/**
 * encodeSharingUrl turns a OneDrive or SharePoint link into the unpadded
 * base64url token Graph's shares API reads after its u! prefix.
 */
export function encodeSharingUrl(link: string): string {
  const trimmed = link.trim();
  if (!URL.canParse(trimmed) || new URL(trimmed).protocol !== "https:") throw new Error("Paste an https:// sharing link");
  const bytes = new TextEncoder().encode(trimmed);
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary).replace(/=+$/, "").replace(/\+/g, "-").replace(/\//g, "_");
}

/**
 * driveKeyFromDriveId returns the part of a OneDrive for work or school or
 * SharePoint drive ID after b!, which a Studio command path can carry, or ""
 * when the ID cannot be listed through a Studio command.
 */
export function driveKeyFromDriveId(driveId: string): string {
  if (!driveId.startsWith(businessDrivePrefix)) return "";
  const driveKey = driveId.slice(businessDrivePrefix.length);
  return studioPathParameterPattern.test(driveKey) ? driveKey : "";
}

/** canListWorkbookContents reports whether the worksheet and table commands can address the workbook. */
export function canListWorkbookContents(driveId: string, workbookId: string): boolean {
  return driveKeyFromDriveId(driveId) !== "" && studioPathParameterPattern.test(workbookId);
}

function workbookFromDriveItem(item: unknown): ExcelWorkbook | undefined {
  if (!record(item) || !text(item.name) || !workbookNamePattern.test(item.name)) return undefined;
  const source = record(item.remoteItem) ? item.remoteItem : item;
  const parent = record(source.parentReference) ? source.parentReference : undefined;
  const driveId = parent && text(parent.driveId) ? parent.driveId : "";
  const workbookId = text(source.id) ? source.id : "";
  if (!graphItemIDPattern.test(driveId) || !graphItemIDPattern.test(workbookId)) return undefined;
  return {driveId, workbookId, name: item.name};
}

// nextSkipToken follows only a next link on Microsoft Graph itself.
function nextSkipToken(nextLink: unknown): string {
  if (!text(nextLink)) return "";
  let parsed: URL;
  try {
    parsed = new URL(nextLink);
  } catch {
    return "";
  }
  if (parsed.protocol !== "https:" || parsed.host !== "graph.microsoft.com") return "";
  let skipToken = "";
  parsed.searchParams.forEach((value, name) => {
    if (skipToken === "" && (name.toLowerCase() === "$skiptoken" || name.toLowerCase() === "skiptoken")) skipToken = value;
  });
  return skipToken;
}

function record(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function text(value: unknown): value is string { return typeof value === "string" && value.trim().length > 0; }
