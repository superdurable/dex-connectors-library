// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import type { ConnectorStudioConnection } from "@superdurable/dex-connectors-react";
import { describeOrganization, isOrganizationID, organizationCommandForConnection, organizationCommandIDs, parseOrganizations } from "./provider.js";

function connection(authMethodIds: string[]): ConnectorStudioConnection {
  return {state: "connected", grantedScopes: [], authMethodIds, configuration: {}, isConfigurationReported: true};
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
});
