// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectorStudioConfigurationUnitTarget, ConnectorStudioConnection } from "@superdurable/dex-connectors-react";

/** JiraSite is one Jira Cloud site the authorization grants. */
export interface JiraSite {
  cloudId: string;
  name: string;
  url: string;
}

/** JiraProject is one project from Jira's project search. */
export interface JiraProject {
  id: string;
  key: string;
  name: string;
}

/** ProjectAction is Jira's project/search action filter. */
export type ProjectAction = "create" | "browse";

const cloudIDPattern = /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/;
const projectKeyPattern = /^[A-Z][A-Z0-9_]{0,254}$/;
const jiraSiteScopes = ["read:jira-work", "write:jira-work"];

/** isCloudID accepts a Jira site UUID, the same rule the Go connector enforces. */
export function isCloudID(value: string): boolean { return cloudIDPattern.test(value.trim()); }

/** isProjectKey accepts an uppercase Jira project key such as OPS. */
export function isProjectKey(value: string): boolean { return projectKeyPattern.test(value.trim()); }

/**
 * parseAccessibleSites keeps the distinct Jira sites of an accessible-resources response. Atlassian answers
 * with a top-level array, which Dex Web cli-v1.1.0 rejects before it reaches the bundle; an object means the
 * host could not list sites.
 */
export function parseAccessibleSites(value: unknown): JiraSite[] {
  if (!Array.isArray(value)) throw new Error("Dex Web could not list Jira sites. Enter the site's cloudId below.");
  const seen = new Set<string>();
  return value.flatMap((item): JiraSite[] => {
    if (!record(item) || !text(item.id) || !isCloudID(item.id) || !isJiraSite(item.scopes)) return [];
    const cloudId = item.id.toLowerCase();
    if (seen.has(cloudId)) return [];
    seen.add(cloudId);
    return [{cloudId, name: text(item.name) ? item.name : cloudId, url: text(item.url) ? item.url : ""}];
  });
}

/** parseProjectPage keeps projects with an ID and key and returns the next startAt, or "" on the last page. */
export function parseProjectPage(value: Record<string, unknown>, startAt: number): {projects: JiraProject[]; nextStartAt: string} {
  const values = array(value.values);
  const projects = values.flatMap((item): JiraProject[] => {
    if (!record(item) || !text(item.id) || !text(item.key) || !isProjectKey(item.key)) return [];
    return [{id: item.id, key: item.key, name: text(item.name) ? item.name : item.key}];
  });
  const isLast = value.isLast !== false || values.length === 0;
  return {projects, nextStartAt: isLast ? "" : String(startAt + values.length)};
}

/** projectAction lists only projects the account can create issues in for createIssue. */
export function projectAction(target: ConnectorStudioConfigurationUnitTarget): ProjectAction {
  return target.scope.kind === "operation" && target.scope.operationId === "createIssue" ? "create" : "browse";
}

/** connectionCloudID is the connection's saved site, or "" when the host reports none. */
export function connectionCloudID(connection: ConnectorStudioConnection): string {
  const cloudId = connection.configuration.cloudId;
  return typeof cloudId === "string" && isCloudID(cloudId) ? cloudId.trim().toLowerCase() : "";
}

function isJiraSite(scopes: unknown): boolean {
  return array(scopes).some((scope) => typeof scope === "string" && jiraSiteScopes.includes(scope));
}
function array(value: unknown): unknown[] { return Array.isArray(value) ? value : []; }
function record(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function text(value: unknown): value is string { return typeof value === "string" && value.length > 0; }
