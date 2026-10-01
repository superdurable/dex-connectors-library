// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectorStudioConnection } from "@superdurable/dex-connectors-react";

/** organizationsCapability is the backend capability every organization-list command declares. */
export const organizationsCapability = "zohodesk.organizations-list";

/**
 * organizationCommandIDs maps each data center's authorization method to the setup command that lists
 * organizations on that data center's own Zoho Desk host, so a token never reaches another data center.
 */
export const organizationCommandIDs: Readonly<Record<string, string>> = {
  "zoho-us-oauth": "listOrganizationsUS",
  "zoho-eu-oauth": "listOrganizationsEU",
  "zoho-in-oauth": "listOrganizationsIN",
  "zoho-au-oauth": "listOrganizationsAU",
  "zoho-jp-oauth": "listOrganizationsJP",
  "zoho-ca-oauth": "listOrganizationsCA",
  "zoho-sa-oauth": "listOrganizationsSA",
  "zoho-sg-oauth": "listOrganizationsSG",
  "zoho-ae-oauth": "listOrganizationsAE",
};

/** ZohoDeskOrganization is one organization the authorized user belongs to. */
export interface ZohoDeskOrganization {
  orgId: string;
  companyName: string;
  portalName: string;
  isSandbox: boolean;
}

const organizationIDPattern = /^[0-9]{1,20}$/;

/** isOrganizationID accepts a numeric Zoho Desk orgId, the same rule the Go connector enforces. */
export function isOrganizationID(value: string): boolean { return organizationIDPattern.test(value.trim()); }

/**
 * organizationCommandForConnection is the command of the connection's data center, or undefined when the
 * host reports no supported authorization method; the bundle then offers manual entry instead of guessing.
 */
export function organizationCommandForConnection(connection: ConnectorStudioConnection): string | undefined {
  const [authMethodId] = connection.authMethodIds;
  return authMethodId === undefined ? undefined : organizationCommandIDs[authMethodId];
}

/** parseOrganizations keeps the distinct organizations of a GET /api/v1/organizations response. */
export function parseOrganizations(value: Record<string, unknown>): ZohoDeskOrganization[] {
  if (!Array.isArray(value.data)) throw new Error("Zoho Desk returned no organization list. Enter the orgId below.");
  const seen = new Set<string>();
  return value.data.flatMap((item): ZohoDeskOrganization[] => {
    if (!record(item)) return [];
    const orgId = typeof item.id === "number" ? String(item.id) : text(item.id) ? item.id : "";
    if (!isOrganizationID(orgId) || seen.has(orgId)) return [];
    seen.add(orgId);
    const portalName = text(item.portalName) ? item.portalName : "";
    return [{
      orgId, portalName, companyName: text(item.companyName) ? item.companyName : portalName || orgId,
      isSandbox: item.isSandboxPortal === true || item.isSandboxPortal === "true",
    }];
  });
}

/** describeOrganization labels an organization by company, portal, and ID. */
export function describeOrganization(organization: ZohoDeskOrganization): string {
  const portal = organization.portalName ? ` (${organization.portalName})` : "";
  const sandbox = organization.isSandbox ? ", sandbox" : "";
  return `${organization.companyName}${portal}, orgId ${organization.orgId}${sandbox}`;
}

function record(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function text(value: unknown): value is string { return typeof value === "string" && value.length > 0; }
