// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import type { ConnectorStudioConnection } from "@superdurable/dex-connectors-react";
import {
  departmentCommandForConnection, departmentCommandIDs, describeOrganization, isDepartmentID, isOrganizationID, organizationCommandForConnection,
  organizationCommandIDs, parseDepartmentPage, parseOrganizations,
} from "./provider.js";

function connection(authMethodIds: string[], configuration: Record<string, unknown> = {}): ConnectorStudioConnection {
  return {state: "connected", grantedScopes: [], authMethodIds, configuration, isConfigurationReported: true, storedCredentialFields: []};
}

describe("Zoho Desk provider responses", () => {
  it("keeps distinct organizations with numeric IDs", () => {
    expect(parseOrganizations({data: [
      {id: "3981311", companyName: "Zylker INC.", portalName: "zylker", isSandboxPortal: "false", primaryContact: "steve@zylker.com"},
      {id: 5988319, companyName: "Nshlerin LLC.", portalName: "nshlerin", isSandboxPortal: "true"},
      {id: "3981311", companyName: "Duplicate"},
      {id: "zylker", companyName: "Portal name, not an ID"},
      "not an organization",
    ]})).toEqual([
      {orgId: "3981311", companyName: "Zylker INC.", portalName: "zylker", isSandbox: false},
      {orgId: "5988319", companyName: "Nshlerin LLC.", portalName: "nshlerin", isSandbox: true},
    ]);
  });

  it("reports a response without an organization list", () => {
    expect(() => parseOrganizations({errorCode: "SCOPE_MISMATCH"})).toThrow("Zoho Desk returned no organization list. Enter the orgId below.");
  });

  it("labels an organization by company, portal, and ID", () => {
    expect(describeOrganization({orgId: "5988319", companyName: "Nshlerin LLC.", portalName: "nshlerin", isSandbox: true}))
      .toBe("Nshlerin LLC. (nshlerin), orgId 5988319, sandbox");
  });

  it("runs only the command of the connection's own data center", () => {
    expect(organizationCommandForConnection(connection(["zoho-eu-oauth"]))).toBe("listOrganizationsEU");
    expect(organizationCommandForConnection(connection(["zoho-ca-oauth"]))).toBe("listOrganizationsCA");
    expect(organizationCommandForConnection(connection([]))).toBeUndefined();
    expect(organizationCommandForConnection(connection(["zoho-cn-oauth"]))).toBeUndefined();
    expect(new Set(Object.values(organizationCommandIDs)).size).toBe(9);
  });

  it("accepts only numeric orgIds", () => {
    expect(isOrganizationID(" 2389290 ")).toBe(true);
    expect(isOrganizationID("zylker")).toBe(false);
    expect(isOrganizationID("https://desk.zoho.com/support/zylker")).toBe(false);
  });

  it("lists departments only on the connection's own data center for its saved organization", () => {
    expect(departmentCommandForConnection(connection(["zoho-eu-oauth"], {orgId: " 2389290 "}))).toEqual({commandId: "listDepartmentsEU", orgId: "2389290"});
    expect(departmentCommandForConnection(connection(["zoho-ae-oauth"], {orgId: "2389290"}))?.commandId).toBe("listDepartmentsAE");
    expect(departmentCommandForConnection(connection(["zoho-us-oauth"]))).toBeUndefined();
    expect(departmentCommandForConnection(connection(["zoho-us-oauth"], {orgId: "zylker"}))).toBeUndefined();
    expect(departmentCommandForConnection(connection([], {orgId: "2389290"}))).toBeUndefined();
    expect(new Set(Object.values(departmentCommandIDs)).size).toBe(9);
    expect(Object.keys(departmentCommandIDs)).toEqual(Object.keys(organizationCommandIDs));
  });

  it("keeps departments with numeric IDs and pages by from", () => {
    expect(parseDepartmentPage({data: [
      {id: "1892000000006907", name: "Billing", isEnabled: true},
      {id: 1892000000082069, name: "Shipping"},
      {id: "billing", name: "Not an ID"},
      "not a department",
    ]}, 0)).toEqual({departments: [
      {departmentId: "1892000000006907", name: "Billing"},
      {departmentId: "1892000000082069", name: "Shipping"},
    ], nextFrom: ""});
    const fullPage = parseDepartmentPage({data: Array.from({length: 200}, (_, index) => ({id: String(1000 + index), name: `D${index}`}))}, 200);
    expect(fullPage.nextFrom).toBe("400");
    expect(() => parseDepartmentPage({errorCode: "SCOPE_MISMATCH"}, 0)).toThrow("Zoho Desk returned no department list.");
  });

  it("accepts only numeric department IDs", () => {
    expect(isDepartmentID(" 1892000000006907 ")).toBe(true);
    expect(isDepartmentID("Billing")).toBe(false);
  });
});
