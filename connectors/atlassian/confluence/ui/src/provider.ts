// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectorStudioConnection } from "@superdurable/dex-connectors-react";

/** ConfluenceSite is one Confluence Cloud site the authorization grants. */
export interface ConfluenceSite {
  cloudId: string;
  name: string;
  url: string;
}

/** ConfluenceSpace is one space from Confluence's space list. */
export interface ConfluenceSpace {
  id: string;
  key: string;
  name: string;
}

const cloudIDPattern = /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/;
const spaceKeyPattern = /^~?[A-Za-z0-9_-]{1,255}$/;
const contentIDPattern = /^[1-9][0-9]{0,18}$/;

/** isCloudID accepts a site UUID, the same rule the Go connector enforces. */
export function isCloudID(value: string): boolean { return cloudIDPattern.test(value.trim()); }

/** isSpaceKey accepts a Confluence space key such as OPS or a personal key such as ~5b10ac8d82e05b22cc7d4ef5. */
export function isSpaceKey(value: string): boolean { return spaceKeyPattern.test(value.trim()); }

/**
 * parseAccessibleSites keeps the distinct Confluence sites of an accessible-resources response. Atlassian
 * answers with a top-level array, which Dex Web rejects before it reaches the bundle; an object means the
 * host could not list sites.
 */
export function parseAccessibleSites(value: unknown): ConfluenceSite[] {
  if (!Array.isArray(value)) throw new Error("Dex Web could not list Confluence sites. Enter the site's cloudId below.");
  const seen = new Set<string>();
  return value.flatMap((item): ConfluenceSite[] => {
    if (!record(item) || !text(item.id) || !isCloudID(item.id) || !isConfluenceSite(item.scopes)) return [];
    const cloudId = item.id.toLowerCase();
    if (seen.has(cloudId)) return [];
    seen.add(cloudId);
    return [{cloudId, name: text(item.name) ? item.name : cloudId, url: text(item.url) ? item.url : ""}];
  });
}

/**
 * parseSpacePage keeps spaces with a numeric ID and a valid key and returns the cursor of Confluence's
 * relative next link, or "" on the last page.
 */
export function parseSpacePage(value: Record<string, unknown>): {spaces: ConfluenceSpace[]; nextCursor: string} {
  const spaces = array(value.results).flatMap((item): ConfluenceSpace[] => {
    if (!record(item) || !text(item.id) || !contentIDPattern.test(item.id) || !text(item.key) || !isSpaceKey(item.key)) return [];
    return [{id: item.id, key: item.key, name: text(item.name) ? item.name : item.key}];
  });
  const links = record(value._links) ? value._links : {};
  return {spaces, nextCursor: text(links.next) ? cursorOf(links.next) : ""};
}

/** connectionCloudID is the connection's saved site, or "" when the host reports none. */
export function connectionCloudID(connection: ConnectorStudioConnection): string {
  const cloudId = connection.configuration.cloudId;
  return typeof cloudId === "string" && isCloudID(cloudId) ? cloudId.trim().toLowerCase() : "";
}

function cursorOf(nextLink: string): string {
  try {
    return new URL(nextLink, "https://api.atlassian.com").searchParams.get("cursor") ?? "";
  } catch {
    return "";
  }
}

function isConfluenceSite(scopes: unknown): boolean {
  return array(scopes).some((scope) => typeof scope === "string" && scope.includes("confluence"));
}
function array(value: unknown): unknown[] { return Array.isArray(value) ? value : []; }
function record(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function text(value: unknown): value is string { return typeof value === "string" && value.length > 0; }
