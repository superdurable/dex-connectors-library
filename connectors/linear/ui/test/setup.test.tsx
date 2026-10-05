// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { LinearSetupView } from "../src/setup.js";
import { LinearConfigurationUnit } from "../src/units.js";

const sentinelToken = "SENTINEL-LINEAR-ACCESS-TOKEN";

describe("Linear setup", () => {
  it("offers OAuth consent only for the OAuth method and keeps keys in the host form", () => {
    const keySetup = renderToStaticMarkup(<LinearSetupView connection={{state: "not_configured", grantedScopes: [], authMethodIds: ["personal-api-key"]}} onConnect={() => undefined} onReconnect={() => undefined}/>);
    expect(keySetup).toContain("Paste a personal API key in the form above");
    expect(keySetup).not.toContain("Connect Linear");
    const oauthSetup = renderToStaticMarkup(<LinearSetupView connection={{state: "not_configured", grantedScopes: [], authMethodIds: ["linear-oauth"]}} onConnect={() => undefined} onReconnect={() => undefined}/>);
    expect(oauthSetup).toContain("Connect Linear");
    const revoked = renderToStaticMarkup(<LinearSetupView connection={{state: "revoked", grantedScopes: [], authMethodIds: ["personal-api-key"]}} onConnect={() => undefined} onReconnect={() => undefined}/>);
    expect(revoked).toContain("Create a new personal API key");
    expect(revoked).not.toContain("Reconnect Linear");
    const connectedKey = renderToStaticMarkup(<LinearSetupView connection={{state: "connected", grantedScopes: [], authMethodIds: ["personal-api-key"]}} onConnect={() => undefined} onReconnect={() => undefined}/>);
    expect(connectedKey).toContain("Team pickers accept a team UUID for a personal API key");
  });

  it("renders the team picker for a Trigger binding with its description, blank meaning, and UUID fallback", () => {
    const scope = {kind: "trigger" as const, triggerName: "issueEventReceived", bindingName: "issue-created", flowType: "LinearIssueIntake"};
    const description = "Choose the Linear team whose new issues start this Flow; leave it empty to record issues of every team.";
    const markup = renderToStaticMarkup(<LinearConfigurationUnit
      target={{kind: "configurationUnit", scope, instanceId: "team", unitId: "teamPicker", label: "Team", description, required: false,
        bindings: [{port: "teamId", jsonPointer: "/teamId"}], value: {teamId: "2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4", teamKey: "ENG", teamName: "Engineering"}}}
      teams={[
        {id: "2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4", key: "ENG", name: "Engineering"},
        {id: "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d", key: "OPS", name: "Operations"},
      ]}
      onLoadTeams={() => undefined} onSave={() => undefined}/>);
    expect(markup).toContain(description);
    expect(markup).toContain("Load teams");
    expect(markup).toContain("Every team");
    expect(markup).toContain("Engineering (ENG)");
    expect(markup).toContain("Operations (OPS)");
    expect(markup).toContain("Team UUID fallback");
    expect(markup).toContain("Copy team ID");
    expect(markup).not.toContain(sentinelToken);
  });

  it("rejects an unknown unit", () => {
    const scope = {kind: "operation" as const, operationId: "createIssue", flowType: "Flow", stepType: "CreateIssue"};
    const markup = renderToStaticMarkup(<LinearConfigurationUnit
      target={{kind: "configurationUnit", scope, instanceId: "x", unitId: "textInput", label: "X", required: false, bindings: [], value: {}}}
      teams={[]} onLoadTeams={() => undefined} onSave={() => undefined}/>);
    expect(markup).toContain("Unsupported Linear configuration unit: textInput");
  });
});
