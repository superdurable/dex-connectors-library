// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { JiraSetupView } from "../src/setup.js";
import { JiraConfigurationUnit, SitePickerUnit } from "../src/units.js";

const siteID = "1324a887-45db-1bf4-1e99-ef0ff456d421";

const projectTarget: ConnectorStudioConfigurationUnitTarget = {
  kind: "configurationUnit", scope: {kind: "operation", operationId: "createIssue", flowType: "JiraIssueTriage", stepType: "CreateTriageIssue"},
  instanceId: "triageProject", unitId: "projectPicker", label: "Triage project",
  description: "Choose the Jira project that receives triage issues. Leave it unsaved to use each Start Flow input's projectKey.", required: false,
  bindings: [{port: "projectId", jsonPointer: "/projectId"}, {port: "projectKey", jsonPointer: "/projectKey"}, {port: "projectName", jsonPointer: "/projectName"}],
  value: {projectId: "10000", projectKey: "OPS", projectName: "Operations"},
};

describe("Jira setup", () => {
  it("shows the connected account without any credential", () => {
    const markup = renderToStaticMarkup(<JiraSetupView connection={{state: "connected", accountEmail: "ada@example.com", grantedScopes: [
      "read:jira-work", "write:jira-work", "offline_access",
    ]}} onConnect={() => undefined} onReconnect={() => undefined}/>);
    expect(markup).toContain("ada@example.com");
    expect(markup).toContain("Choose the Jira site in the connection form.");
    expect(markup).not.toContain("access_token");
    expect(markup).not.toContain("refresh_token");
    expect(markup).not.toContain("client_secret");
  });

  it.each([
    ["revoked", "Connection revoked"],
    ["insufficient_scope", "Additional permission required"],
  ] as const)("shows a reconnect action for %s", (state, label) => {
    const markup = renderToStaticMarkup(<JiraSetupView connection={{state, grantedScopes: []}} onConnect={() => undefined} onReconnect={() => undefined}/>);
    expect(markup).toContain(label);
    expect(markup).toContain("accept every requested scope");
    expect(markup).toContain("Reconnect Jira");
  });

  it("renders the site picker with manual cloudId guidance and validation", () => {
    const markup = renderToStaticMarkup(<SitePickerUnit onChooseSite={() => undefined} onSave={() => undefined}
      sites={[{cloudId: siteID, name: "Ops", url: "https://ops.atlassian.net"}]}
      target={{kind: "connection", unitId: "sitePicker", bindings: [{port: "cloudId", jsonPointer: "/cloudId"}], value: {cloudId: "ops.atlassian.net"}}}/>);
    expect(markup).toContain('class="studio-surface"');
    expect(markup).toContain("Leave it blank to use the only Jira site the authorization grants.");
    expect(markup).toContain("Ops (https://ops.atlassian.net)");
    expect(markup).toContain("https://&lt;your-site&gt;.atlassian.net/_edge/tenant_info");
    expect(markup).toContain('role="alert">The cloudId must be a UUID; a site URL is not accepted.');
    expect(markup).toContain('class="studio-button studio-button-primary" type="button" disabled="">Save');
    expect(markup).not.toContain("<style");
  });

  it("shows a site load failure inline", () => {
    const markup = renderToStaticMarkup(<SitePickerUnit loadError="Dex Web could not list Jira sites. Enter the site's cloudId below."
      onChooseSite={() => undefined} onSave={() => undefined} target={{kind: "connection", unitId: "sitePicker", value: {}}}/>);
    expect(markup).toContain("Dex Web could not list Jira sites.");
    expect(markup).toContain('class="studio-button studio-button-primary" type="button">Save');
  });

  it("renders the saved project with its operation-specific guidance", () => {
    const markup = renderToStaticMarkup(<JiraConfigurationUnit connectionCloudId={siteID} onChooseProject={() => undefined} onSave={() => undefined} target={projectTarget}/>);
    expect(markup).toContain("Choose the Jira project that receives triage issues. Leave it unsaved to use each Start Flow input&#x27;s projectKey.");
    expect(markup).toContain("<strong>Project:</strong> Operations (OPS)");
    expect(markup).toContain('value="OPS"');
    expect(markup).toContain("Jira &gt; Projects &gt; View all projects");
    // The connection's site is known, so no listing site is asked for.
    expect(markup).not.toContain("Jira site cloudId");
    expect(markup).not.toContain("more projects than one list can show");
  });

  it("asks for a listing site when the host reports no connection cloudId", () => {
    const markup = renderToStaticMarkup(<JiraConfigurationUnit connectionCloudId="" isProjectListTruncated
      onChooseProject={() => undefined} onSave={() => undefined}
      projects={[{id: "10000", key: "OPS", name: "Operations"}]} target={{...projectTarget, value: {}}}/>);
    expect(markup).toContain("Jira site cloudId");
    expect(markup).toContain("Used only to list projects");
    expect(markup).toContain('class="studio-button" type="button" disabled="">Choose project');
    expect(markup).toContain("Operations (OPS)");
    expect(markup).toContain("Jira returned more projects than one list can show.");
  });

  it("rejects a lowercase project key and requires a key for a required unit", () => {
    const invalid = renderToStaticMarkup(<JiraConfigurationUnit connectionCloudId={siteID} onChooseProject={() => undefined} onSave={() => undefined}
      target={{...projectTarget, value: {projectKey: "ops"}}}/>);
    expect(invalid).toContain("The project key must be uppercase letters");
    const required = renderToStaticMarkup(<JiraConfigurationUnit connectionCloudId={siteID} onChooseProject={() => undefined} onSave={() => undefined}
      target={{...projectTarget, required: true, value: {}}}/>);
    expect(required).toContain('class="studio-button studio-button-primary" type="button" disabled="">Save');
  });
});
