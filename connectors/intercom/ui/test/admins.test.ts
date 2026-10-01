// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ConnectorStudioCommandError, type ConnectorStudioClient, type ConnectorStudioConnection } from "@superdurable/dex-connectors-react";
import { describe, expect, it } from "vitest";
import { adminListCommandOrder, loadIntercomAdmins, parseIntercomAdminList } from "../src/admins.js";

const unreportedConnection: ConnectorStudioConnection = {
  state: "connected", grantedScopes: [], authMethodIds: [], configuration: {}, isConfigurationReported: false,
};

describe("Intercom admin list", () => {
  it("keeps admins with numeric IDs sorted by name, without other fields", () => {
    expect(parseIntercomAdminList({type: "admin.list", admins: [
      {type: "admin", id: "991267", name: "Zoe", email: "zoe@example.com", away_mode_enabled: true, job_title: "Lead"},
      {type: "admin", id: "5017691", name: "Ada", email: "ada@example.com"},
      {type: "team", id: "not-numeric", name: "Billing"},
      "not an object",
    ]})).toEqual([
      {id: "5017691", name: "Ada", email: "ada@example.com", isAway: false},
      {id: "991267", name: "Zoe", email: "zoe@example.com", isAway: true},
    ]);
  });

  it("rejects a response that is not an admin list", () => {
    expect(() => parseIntercomAdminList({type: "error.list", errors: []})).toThrow("did not return an admin list");
    expect(() => parseIntercomAdminList({admins: "none"})).toThrow("did not return an admin list");
  });

  it("asks only the connection's region when the host reports the configuration", () => {
    expect(adminListCommandOrder({...unreportedConnection, isConfigurationReported: true, configuration: {region: "eu"}})).toEqual(["listAdminsEU"]);
    expect(adminListCommandOrder({...unreportedConnection, isConfigurationReported: true, configuration: {region: "au"}})).toEqual(["listAdminsAU"]);
    expect(adminListCommandOrder({...unreportedConnection, isConfigurationReported: true, configuration: {}})).toEqual(["listAdmins"]);
  });

  it("tries every regional host, US first, when the host does not report the configuration", () => {
    expect(adminListCommandOrder(unreportedConnection)).toEqual(["listAdmins", "listAdminsEU", "listAdminsAU"]);
    expect(adminListCommandOrder({...unreportedConnection, configuration: {region: "au"}})).toEqual(["listAdmins", "listAdminsEU", "listAdminsAU"]);
  });

  it("uses the first regional command Intercom accepts", async () => {
    const requested: string[] = [];
    const client: ConnectorStudioClient = {
      ready: undefined, busy: false,
      send: async () => ({}),
      executeProviderCommand: async (commandId, capability) => {
        requested.push(`${commandId}:${capability}`);
        if (commandId === "listAdmins") throw new ConnectorStudioCommandError("CONNECTOR_PROVIDER_COMMAND_FAILED", "provider rejected");
        return {type: "admin.list", admins: [{id: "5017691", name: "Ada"}]};
      },
    };
    await expect(loadIntercomAdmins(client, unreportedConnection)).resolves.toEqual([{id: "5017691", name: "Ada", email: "", isAway: false}]);
    expect(requested).toEqual(["listAdmins:intercom.admins-list", "listAdminsEU:intercom.admins-list"]);
  });
});
