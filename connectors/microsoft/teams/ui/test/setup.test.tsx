// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { TeamsSetupView } from "../src/setup.js";
import { TeamsConfigurationUnit, type TeamsUnitProps } from "../src/units.js";

const teamID = "fbe2bf47-16c8-47cf-b4a5-4b9b187c508b";
const channelID = "19:4a95f7d8db4c4e7fae857bcebe0623e6@thread.tacv2";

function unitTarget(unitId: string, value: Record<string, unknown>, required = true): ConnectorStudioConfigurationUnitTarget {
  return {
    kind: "configurationUnit", scope: {kind: "operation", operationId: "postChannelMessage", flowType: "TeamsIncidentAcknowledgement", stepType: "PostIncidentUpdate"},
    instanceId: unitId, unitId, label: "Incident " + unitId, description: "Step-specific guidance for " + unitId + ".", required,
    bindings: [], value,
  };
}

function render(props: Partial<TeamsUnitProps> & {target: ConnectorStudioConfigurationUnitTarget}): string {
  return renderToStaticMarkup(<TeamsConfigurationUnit onLoadChannels={() => undefined} onLoadChats={() => undefined}
    onLoadTeams={() => undefined} onSave={() => undefined} {...props}/>);
}

describe("Microsoft Teams setup", () => {
  it("shows the connected account and admin consent guidance without any credential", () => {
    const connected = renderToStaticMarkup(<TeamsSetupView connection={{state: "connected", accountEmail: "incident-bot@contoso.com", grantedScopes: [
      "ChannelMessage.Send", "ChannelMessage.Read.All",
    ]}} onConnect={() => undefined} onReconnect={() => undefined}/>);
    expect(connected).toContain("incident-bot@contoso.com");
    expect(connected).toContain("Messages show this account as the sender.");
    for (const secret of ["access_token", "refresh_token", "client_secret", "oauth_client_secret"]) expect(connected).not.toContain(secret);

    const notConfigured = renderToStaticMarkup(<TeamsSetupView connection={{state: "not_configured", grantedScopes: []}} onConnect={() => undefined} onReconnect={() => undefined}/>);
    expect(notConfigured).toContain("ChannelMessage.Read.All");
    expect(notConfigured).toContain("Need admin approval");
    expect(notConfigured).toContain("Connect Microsoft Teams");
  });

  it.each([
    ["revoked", "Connection revoked"],
    ["insufficient_scope", "Additional permission required"],
    ["expired", "Connection expired"],
  ] as const)("shows a reconnect action for %s", (state, label) => {
    const markup = renderToStaticMarkup(<TeamsSetupView connection={{state, grantedScopes: []}} onConnect={() => undefined} onReconnect={() => undefined}/>);
    expect(markup).toContain(label);
    expect(markup).toContain("accept every requested permission");
    expect(markup).toContain("Reconnect Microsoft Teams");
  });

  it("renders the team picker with its Step guidance, list, manual ID hint, and validation", () => {
    const markup = render({target: unitTarget("teamPicker", {teamId: "Operations"}), teams: [{id: teamID, name: "Operations"}]});
    expect(markup).toContain('class="studio-surface"');
    expect(markup).toContain("Step-specific guidance for teamPicker.");
    expect(markup).toContain("Load teams");
    expect(markup).toContain(`<option value="${teamID}">Operations</option>`);
    expect(markup).toContain("Get link to team");
    expect(markup).toContain('role="alert">A team ID is a GUID');
    expect(markup).toContain('class="studio-button studio-button-primary" type="button" disabled="">Save');
    expect(markup).not.toContain("<style");
  });

  it("asks for a saved team before listing channels and shows the saved channel", () => {
    const withoutTeam = render({target: unitTarget("channelPicker", {})});
    expect(withoutTeam).toContain("Save a team first to list its channels.");
    expect(withoutTeam).toContain('class="studio-button" type="button" disabled="">Load channels');

    const saved = render({
      target: unitTarget("channelPicker", {teamId: teamID, channelId: channelID, channelName: "Incidents"}),
      channels: [{id: channelID, name: "Incidents", membershipType: "standard"}, {id: "19:abc@thread.tacv2", name: "Leads", membershipType: "private"}],
      isListTruncated: true,
    });
    expect(saved).toContain("<strong>Channel:</strong> Incidents");
    expect(saved).toContain("Leads (private)");
    expect(saved).toContain("Get link to channel");
    expect(saved).toContain("more channels than one list can show");
    expect(saved).toContain('class="studio-button studio-button-primary" type="button">Save');
  });

  it("lets an optional chat picker save blank and rejects a malformed chat ID", () => {
    const blank = render({target: unitTarget("chatPicker", {}, false), loadError: "Microsoft Teams returned no chat list (Forbidden)"});
    expect(blank).toContain("Microsoft Teams returned no chat list (Forbidden)");
    expect(blank).toContain("https://teams.microsoft.com");
    expect(blank).toContain('class="studio-button studio-button-primary" type="button">Save');
    const invalid = render({target: unitTarget("chatPicker", {chatId: "General"}, false)});
    expect(invalid).toContain('role="alert">A chat ID starts with 19:');
    expect(invalid).toContain('class="studio-button studio-button-primary" type="button" disabled="">Save');
    const required = render({target: unitTarget("chatPicker", {}, true), chats: [{id: "19:a@thread.v2", label: "On-call managers", chatType: "group"}]});
    expect(required).toContain("On-call managers (group)");
    expect(required).toContain('class="studio-button studio-button-primary" type="button" disabled="">Save');
  });

  it("rejects an unknown unit", () => {
    expect(render({target: unitTarget("spacePicker", {})})).toContain("Unsupported Microsoft Teams configuration unit: spacePicker");
  });
});
