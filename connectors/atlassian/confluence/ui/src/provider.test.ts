// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import type { ConnectorStudioConnection } from "@superdurable/dex-connectors-react";
import { connectionCloudID, isCloudID, isSpaceKey, parseAccessibleSites, parseSpacePage } from "./provider.js";

const siteID = "1324a887-45db-1bf4-1e99-ef0ff456d421";

function connection(configuration: Record<string, unknown>): ConnectorStudioConnection {
  return {state: "connected", grantedScopes: [], authMethodIds: [], configuration, isConfigurationReported: true};
}

describe("Confluence provider responses", () => {
  it("keeps distinct Confluence sites from accessible resources and drops other products", () => {
    expect(parseAccessibleSites([
      {id: siteID.toUpperCase(), name: "Ops", url: "https://ops.atlassian.net", scopes: ["search:confluence", "read:page:confluence"]},
      {id: siteID, name: "Ops again", url: "https://ops.atlassian.net", scopes: ["write:page:confluence"]},
      {id: "99999999-0000-4000-8000-000000000000", name: "Tracker", scopes: ["read:jira-work"]},
      {id: "not-a-uuid", name: "Broken", scopes: ["read:page:confluence"]},
    ])).toEqual([{cloudId: siteID, name: "Ops", url: "https://ops.atlassian.net"}]);
  });

  it("reports that the host could not list sites when it returns an object", () => {
    expect(() => parseAccessibleSites({})).toThrow("Dex Web could not list Confluence sites. Enter the site's cloudId below.");
  });

  it("pages spaces by the next link's cursor and ignores entries without a valid ID or key", () => {
    expect(parseSpacePage({results: [
      {id: "98306", key: "OPS", name: "Operations"},
      {id: "98307", key: "~5b10ac8d82e05b22cc7d4ef5", name: "Ada's space"},
      {id: "abc", key: "BAD"},
      {id: "98308", key: "has space"},
    ], _links: {next: "/wiki/api/v2/spaces?limit=100&cursor=eyJpZCI6OTgzMDh9"}})).toEqual({
      spaces: [{id: "98306", key: "OPS", name: "Operations"}, {id: "98307", key: "~5b10ac8d82e05b22cc7d4ef5", name: "Ada's space"}],
      nextCursor: "eyJpZCI6OTgzMDh9",
    });
    expect(parseSpacePage({results: [{id: "98309", key: "HR"}], _links: {}})).toEqual({spaces: [{id: "98309", key: "HR", name: "HR"}], nextCursor: ""});
    expect(parseSpacePage({})).toEqual({spaces: [], nextCursor: ""});
  });

  it("reads the connection's cloudId only when it is a valid site ID and validates space keys", () => {
    expect(connectionCloudID(connection({cloudId: ` ${siteID.toUpperCase()} `}))).toBe(siteID);
    expect(connectionCloudID(connection({cloudId: "ops.atlassian.net"}))).toBe("");
    expect(connectionCloudID(connection({}))).toBe("");
    expect(isCloudID("https://ops.atlassian.net")).toBe(false);
    expect(isSpaceKey("OPS")).toBe(true);
    expect(isSpaceKey("~ada")).toBe(true);
    expect(isSpaceKey("OPS\"")).toBe(false);
  });
});
