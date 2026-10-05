// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import type { ConnectorStudioConnection } from "@superdurable/dex-connectors-react";
import { connectionCloudID, isCloudID, isNumericID, parseAccessibleSites, parseRequestTypePage, parseServiceDeskPage } from "./provider.js";

const siteID = "1324a887-45db-1bf4-1e99-ef0ff456d421";

function connection(configuration: Record<string, unknown>): ConnectorStudioConnection {
  return {state: "connected", grantedScopes: [], authMethodIds: [], configuration, isConfigurationReported: true};
}

describe("Jira Service Management provider responses", () => {
  it("keeps distinct service desk sites from accessible resources and drops other products", () => {
    expect(parseAccessibleSites([
      {id: siteID.toUpperCase(), name: "Help", url: "https://help.atlassian.net", scopes: ["read:servicedesk-request", "read:jira-work"]},
      {id: siteID, name: "Help again", scopes: ["write:servicedesk-request"]},
      {id: "99999999-0000-4000-8000-000000000000", name: "Jira only", scopes: ["read:jira-work"]},
      {id: "not-a-uuid", name: "Broken", scopes: ["read:servicedesk-request"]},
    ])).toEqual([{cloudId: siteID, name: "Help", url: "https://help.atlassian.net"}]);
  });

  it("reports that the host could not list sites when it returns an object", () => {
    expect(() => parseAccessibleSites({})).toThrow("Dex Web could not list Atlassian sites. Enter the site's cloudId below.");
  });

  it("pages service desks by start and ignores entries without a numeric ID or valid project key", () => {
    expect(parseServiceDeskPage({isLastPage: false, values: [
      {id: "10", projectKey: "ITH", projectName: "IT Help"},
      {id: "11", projectKey: "hr", projectName: "Lowercase"},
      {id: "x", projectKey: "FAC"},
    ]}, 50)).toEqual({items: [{id: "10", projectKey: "ITH", name: "IT Help"}], nextStart: "53"});
    expect(parseServiceDeskPage({isLastPage: true, values: [{id: "12", projectKey: "HR"}]}, 0)).toEqual({items: [{id: "12", projectKey: "HR", name: "HR"}], nextStart: ""});
    expect(parseServiceDeskPage({}, 0)).toEqual({items: [], nextStart: ""});
  });

  it("pages request types and keeps only numeric IDs", () => {
    expect(parseRequestTypePage({isLastPage: false, values: [{id: "25", name: "Get IT help"}, {id: "", name: "Blank"}, {id: "26"}]}, 0))
      .toEqual({items: [{id: "25", name: "Get IT help"}, {id: "26", name: "26"}], nextStart: "3"});
  });

  it("reads the connection's cloudId only when it is a valid site ID", () => {
    expect(connectionCloudID(connection({cloudId: ` ${siteID.toUpperCase()} `}))).toBe(siteID);
    expect(connectionCloudID(connection({cloudId: "help.atlassian.net"}))).toBe("");
    expect(connectionCloudID(connection({}))).toBe("");
    expect(isCloudID("https://help.atlassian.net")).toBe(false);
    expect(isNumericID("10")).toBe(true);
    expect(isNumericID("010")).toBe(false);
  });
});
