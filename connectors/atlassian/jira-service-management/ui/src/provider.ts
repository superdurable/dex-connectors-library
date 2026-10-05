// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectorStudioConnection } from "@superdurable/dex-connectors-react";

/** ServiceManagementSite is one Atlassian site with Jira Service Management that the authorization grants. */
export interface ServiceManagementSite {
  cloudId: string;
  name: string;
  url: string;
}

/** ServiceDesk is one service desk from GET /rest/servicedeskapi/servicedesk. */
export interface ServiceDesk {
  id: string;
  projectKey: string;
  name: string;
}

/** RequestType is one request type from GET /rest/servicedeskapi/servicedesk/{serviceDeskId}/requesttype. */
export interface RequestType {
  id: string;
  name: string;
}

/** ProviderPage is one parsed page and the next start index, or "" on the last page. */
export interface ProviderPage<T> {
  items: T[];
  nextStart: string;
}

const cloudIDPattern = /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/;
const numericIDPattern = /^[1-9][0-9]{0,17}$/;
const projectKeyPattern = /^[A-Z][A-Z0-9_]{0,254}$/;
const serviceDeskScopes = ["read:servicedesk-request", "write:servicedesk-request"];

/** isCloudID accepts a site UUID, the same rule the Go connector enforces. */
export function isCloudID(value: string): boolean { return cloudIDPattern.test(value.trim()); }

/** isNumericID accepts a service desk or request type ID such as 10. */
export function isNumericID(value: string): boolean { return numericIDPattern.test(value.trim()); }

/** isProjectKey accepts an uppercase Jira project key such as ITH. */
export function isProjectKey(value: string): boolean { return projectKeyPattern.test(value.trim()); }

/**
 * parseAccessibleSites keeps the distinct sites whose grant carries a Jira Service Management scope. Atlassian
 * answers with a top-level array, which Dex Web rejects before it reaches the bundle; an object means the host
 * could not list sites.
 */
export function parseAccessibleSites(value: unknown): ServiceManagementSite[] {
  if (!Array.isArray(value)) throw new Error("Dex Web could not list Atlassian sites. Enter the site's cloudId below.");
  const seen = new Set<string>();
  return value.flatMap((item): ServiceManagementSite[] => {
    if (!record(item) || !text(item.id) || !isCloudID(item.id) || !hasServiceDeskScope(item.scopes)) return [];
    const cloudId = item.id.toLowerCase();
    if (seen.has(cloudId)) return [];
    seen.add(cloudId);
    return [{cloudId, name: text(item.name) ? item.name : cloudId, url: text(item.url) ? item.url : ""}];
  });
}

/** parseServiceDeskPage keeps desks with a numeric ID and a valid project key. */
export function parseServiceDeskPage(value: Record<string, unknown>, start: number): ProviderPage<ServiceDesk> {
  const values = array(value.values);
  const items = values.flatMap((item): ServiceDesk[] => {
    if (!record(item) || !text(item.id) || !isNumericID(item.id) || !text(item.projectKey) || !isProjectKey(item.projectKey)) return [];
    return [{id: item.id, projectKey: item.projectKey, name: text(item.projectName) ? item.projectName : item.projectKey}];
  });
  return {items, nextStart: nextStart(value, values, start)};
}

/** parseRequestTypePage keeps request types with a numeric ID. */
export function parseRequestTypePage(value: Record<string, unknown>, start: number): ProviderPage<RequestType> {
  const values = array(value.values);
  const items = values.flatMap((item): RequestType[] => {
    if (!record(item) || !text(item.id) || !isNumericID(item.id)) return [];
    return [{id: item.id, name: text(item.name) ? item.name : item.id}];
  });
  return {items, nextStart: nextStart(value, values, start)};
}

/** connectionCloudID is the connection's saved site, or "" when the host reports none. */
export function connectionCloudID(connection: ConnectorStudioConnection): string {
  const cloudId = connection.configuration.cloudId;
  return typeof cloudId === "string" && isCloudID(cloudId) ? cloudId.trim().toLowerCase() : "";
}

function nextStart(value: Record<string, unknown>, values: unknown[], start: number): string {
  const isLastPage = value.isLastPage !== false || values.length === 0;
  return isLastPage ? "" : String(start + values.length);
}

function hasServiceDeskScope(scopes: unknown): boolean {
  return array(scopes).some((scope) => typeof scope === "string" && serviceDeskScopes.includes(scope));
}
function array(value: unknown): unknown[] { return Array.isArray(value) ? value : []; }
function record(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function text(value: unknown): value is string { return typeof value === "string" && value.length > 0; }
