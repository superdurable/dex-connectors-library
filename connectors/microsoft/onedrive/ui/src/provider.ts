// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export interface SharePointSite { id: string; name: string; webUrl: string; }
export interface GraphDrive { id: string; name: string; driveType: string; }
export interface GraphFolder { id: string; name: string; }
export interface GraphPage<T> { items: T[]; nextSkipToken: string; }

/** SiteIDParts are the comma-free path parameters Dex Web accepts for /sites/{hostname},{collection},{web}. */
export interface SiteIDParts { siteHostname: string; siteCollectionId: string; siteWebId: string; }

const graphIDPattern = /^[A-Za-z0-9!_.-]{1,256}$/;
// Dex Web accepts only these characters in a Studio command path parameter.
const pathParameterPattern = /^[A-Za-z0-9._~-]+$/;
const guidPattern = /^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$/;
const hostnamePattern = /^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)+$/;

export function parseSitePage(value: Record<string, unknown>): GraphPage<SharePointSite> {
  const items = array(value.value).flatMap((site) => {
    if (!record(site) || !text(site.id) || !siteIDParts(site.id)) return [];
    const name = text(site.displayName) ? site.displayName : text(site.name) ? site.name : site.id;
    return [{id: site.id, name, webUrl: text(site.webUrl) ? site.webUrl : ""}];
  });
  return {items, nextSkipToken: skipTokenFromNextLink(value["@odata.nextLink"])};
}

export function parseDrivePage(value: Record<string, unknown>): GraphPage<GraphDrive> {
  return {items: array(value.value).flatMap((drive) => parseDrive(drive)), nextSkipToken: skipTokenFromNextLink(value["@odata.nextLink"])};
}

/** parseDrive reads one drive object, such as the /me/drive response, or nothing when it is unusable. */
export function parseDrive(value: unknown): GraphDrive[] {
  if (!record(value) || !text(value.id) || !graphIDPattern.test(value.id)) return [];
  return [{id: value.id, name: text(value.name) ? value.name : value.id, driveType: text(value.driveType) ? value.driveType : ""}];
}

/** parseFolderPage keeps only children with a folder facet. */
export function parseFolderPage(value: Record<string, unknown>): GraphPage<GraphFolder> {
  const items = array(value.value).flatMap((item) => record(item) && text(item.id) && graphIDPattern.test(item.id) && text(item.name) && record(item.folder)
    ? [{id: item.id, name: item.name}] : []);
  return {items, nextSkipToken: skipTokenFromNextLink(value["@odata.nextLink"])};
}

/** siteIDParts splits a Graph site ID, hostname,site-collection-GUID,web-GUID, or returns undefined. */
export function siteIDParts(siteId: string): SiteIDParts | undefined {
  const parts = siteId.trim().split(",");
  if (parts.length !== 3 || !hostnamePattern.test(parts[0]) || !guidPattern.test(parts[1]) || !guidPattern.test(parts[2])) return undefined;
  return {siteHostname: parts[0], siteCollectionId: parts[1], siteWebId: parts[2]};
}

/** driveKeyFromDriveID returns the part after b! that the folder commands send, or "" when the ID cannot be browsed. */
export function driveKeyFromDriveID(driveId: string): string {
  const key = driveId.trim().startsWith("b!") ? driveId.trim().slice(2) : "";
  return pathParameterPattern.test(key) ? key : "";
}

/** canBrowseItem reports whether an item ID can travel as a Studio command path parameter. */
export function canBrowseItem(itemId: string): boolean { return pathParameterPattern.test(itemId); }

export function isGraphID(value: string): boolean { return graphIDPattern.test(value.trim()); }

/** skipTokenFromNextLink reads $skiptoken from a Graph next link on graph.microsoft.com, or returns "". */
export function skipTokenFromNextLink(value: unknown): string {
  if (!text(value)) return "";
  let link: URL;
  try {
    link = new URL(value);
  } catch {
    return "";
  }
  if (link.protocol !== "https:" || link.host !== "graph.microsoft.com") return "";
  return link.searchParams.get("$skiptoken") ?? link.searchParams.get("$skipToken") ?? "";
}

function array(value: unknown): unknown[] { return Array.isArray(value) ? value : []; }
function record(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function text(value: unknown): value is string { return typeof value === "string" && value.length > 0; }
