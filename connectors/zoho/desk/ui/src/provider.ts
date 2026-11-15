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

/** departmentsCapability is the backend capability every department-list command declares. */
export const departmentsCapability = "zohodesk.departments-list";

/** departmentPageSize is the limit every department-list command sends; a full page has another after it. */
export const departmentPageSize = 200;

/** departmentCommandIDs maps each data center's authorization method to its own department-list command. */
export const departmentCommandIDs: Readonly<Record<string, string>> = {
  "zoho-us-oauth": "listDepartmentsUS",
  "zoho-eu-oauth": "listDepartmentsEU",
  "zoho-in-oauth": "listDepartmentsIN",
  "zoho-au-oauth": "listDepartmentsAU",
  "zoho-jp-oauth": "listDepartmentsJP",
  "zoho-ca-oauth": "listDepartmentsCA",
  "zoho-sa-oauth": "listDepartmentsSA",
  "zoho-sg-oauth": "listDepartmentsSG",
  "zoho-ae-oauth": "listDepartmentsAE",
};

/** ZohoDeskDepartment is one enabled department of the connection's organization. */
export interface ZohoDeskDepartment {
  departmentId: string;
  name: string;
}

/** ZohoDeskDepartmentPage is one GET /api/v1/departments page and the from of the next one, or "". */
export interface ZohoDeskDepartmentPage {
  departments: ZohoDeskDepartment[];
  nextFrom: string;
}

/** isDepartmentID accepts a numeric Zoho Desk department ID, the same rule the Go connector enforces. */
export function isDepartmentID(value: string): boolean { return organizationIDPattern.test(value.trim()); }

/**
 * departmentCommandForConnection is the department command of the connection's data center, or undefined
 * when the host reports no supported authorization method or the connection has no saved numeric orgId.
 */
export function departmentCommandForConnection(connection: ConnectorStudioConnection): {commandId: string; orgId: string} | undefined {
  const [authMethodId] = connection.authMethodIds;
  const commandId = authMethodId === undefined ? undefined : departmentCommandIDs[authMethodId];
  const orgId = connection.configuration.orgId;
  if (commandId === undefined || typeof orgId !== "string" || !isOrganizationID(orgId)) return undefined;
  return {commandId, orgId: orgId.trim()};
}

/** parseDepartmentPage keeps the departments with numeric IDs of one page that started at from. */
export function parseDepartmentPage(value: Record<string, unknown>, from: number): ZohoDeskDepartmentPage {
  if (!Array.isArray(value.data)) throw new Error("Zoho Desk returned no department list. Enter the department ID below.");
  const departments = value.data.flatMap((item): ZohoDeskDepartment[] => {
    if (!record(item)) return [];
    const departmentId = typeof item.id === "number" ? String(item.id) : text(item.id) ? item.id : "";
    if (!isDepartmentID(departmentId)) return [];
    return [{departmentId, name: text(item.name) ? item.name : departmentId}];
  });
  return {departments, nextFrom: value.data.length >= departmentPageSize ? String(from + departmentPageSize) : ""};
}

function record(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function text(value: unknown): value is string { return typeof value === "string" && value.length > 0; }
