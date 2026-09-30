// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import type { ConnectorStudioConfigurationUnitTarget, ConnectorStudioConnection } from "@superdurable/dex-connectors-react";
import { connectionCloudID, isCloudID, parseAccessibleSites, parseProjectPage, projectAction } from "./provider.js";

const siteID = "1324a887-45db-1bf4-1e99-ef0ff456d421";

function pickerTarget(operationId: string): ConnectorStudioConfigurationUnitTarget {
  return {
    kind: "configurationUnit", scope: {kind: "operation", operationId, flowType: "JiraIssueTriage", stepType: "CreateTriageIssue"},
    instanceId: "triageProject", unitId: "projectPicker", label: "Project", required: false,
    bindings: [{port: "projectKey", jsonPointer: "/projectKey"}], value: {},
  };
}

function connection(configuration: Record<string, unknown>): ConnectorStudioConnection {
  return {state: "connected", grantedScopes: [], authMethodIds: [], configuration, isConfigurationReported: true};
}

describe("Jira provider responses", () => {
  it("keeps distinct Jira sites from accessible resources and drops other products", () => {
    expect(parseAccessibleSites([
      {id: siteID.toUpperCase(), name: "Ops", url: "https://ops.atlassian.net", scopes: ["read:jira-work", "write:jira-work"]},
      {id: siteID, name: "Ops again", url: "https://ops.atlassian.net", scopes: ["write:jira-work"]},
      {id: "99999999-0000-4000-8000-000000000000", name: "Wiki", scopes: ["read:confluence-content.all"]},
      {id: "not-a-uuid", name: "Broken", scopes: ["read:jira-work"]},
    ])).toEqual([{cloudId: siteID, name: "Ops", url: "https://ops.atlassian.net"}]);
  });

  it("reports that the host could not list sites when it returns an object", () => {
    expect(() => parseAccessibleSites({})).toThrow("Dex Web could not list Jira sites. Enter the site's cloudId below.");
  });

  it("pages projects by startAt and ignores entries without a valid key", () => {
    expect(parseProjectPage({isLast: false, values: [
      {id: "10000", key: "OPS", name: "Operations"},
      {id: "10001", key: "fac", name: "Lowercase"},
      {key: "NOID", name: "Missing ID"},
    ]}, 50)).toEqual({projects: [{id: "10000", key: "OPS", name: "Operations"}], nextStartAt: "53"});
    expect(parseProjectPage({isLast: true, values: [{id: "10002", key: "HR"}]}, 0)).toEqual({projects: [{id: "10002", key: "HR", name: "HR"}], nextStartAt: ""});
    expect(parseProjectPage({}, 0)).toEqual({projects: [], nextStartAt: ""});
  });

  it("lists create-permitted projects only for createIssue", () => {
    expect(projectAction(pickerTarget("createIssue"))).toBe("create");
    expect(projectAction(pickerTarget("searchIssues"))).toBe("browse");
  });

  it("reads the connection's cloudId only when it is a valid site ID", () => {
    expect(connectionCloudID(connection({cloudId: ` ${siteID.toUpperCase()} `}))).toBe(siteID);
    expect(connectionCloudID(connection({cloudId: "ops.atlassian.net"}))).toBe("");
    expect(connectionCloudID(connection({}))).toBe("");
    expect(isCloudID(siteID)).toBe(true);
    expect(isCloudID("https://ops.atlassian.net")).toBe(false);
  });
});
