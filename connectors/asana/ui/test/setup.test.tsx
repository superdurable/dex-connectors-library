// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { AsanaSetupView } from "../src/setup.js";
import { AsanaConfigurationUnit } from "../src/units.js";

const projectTarget: ConnectorStudioConfigurationUnitTarget = {
  kind: "configurationUnit", scope: {kind: "operation", operationId: "createTask", flowType: "AsanaApprovedRequestTask", stepType: "CreateRequestTask"},
  instanceId: "requestProject", unitId: "projectPicker", label: "Request project and section",
  description: "Choose the Asana project that receives approved request tasks. Leave it unsaved to use each Start Flow input's projectId and sectionId.",
  required: false,
  bindings: [
    {port: "workspaceId", jsonPointer: "/workspaceId"}, {port: "projectId", jsonPointer: "/projectId"}, {port: "projectName", jsonPointer: "/projectName"},
    {port: "sectionId", jsonPointer: "/sectionId"}, {port: "sectionName", jsonPointer: "/sectionName"},
  ],
  value: {workspaceId: "1100000000000001", projectId: "1201000000000001", projectName: "Facilities", sectionId: "1201000000000101", sectionName: "Approved"},
};

const workspaceTarget: ConnectorStudioConfigurationUnitTarget = {
  ...projectTarget, instanceId: "taskWorkspace", unitId: "workspacePicker", label: "Task workspace",
  description: "Choose the workspace that receives tasks created without a project.", required: true,
  bindings: [{port: "workspaceId", jsonPointer: "/workspaceId"}, {port: "workspaceName", jsonPointer: "/workspaceName"}], value: {},
};

const noLoads = {onLoadProjects: () => undefined, onLoadSections: () => undefined, onLoadWorkspaces: () => undefined, onSave: () => undefined};

describe("Asana setup", () => {
  it("tells the user where the token goes without rendering any credential", () => {
    const markup = renderToStaticMarkup(<AsanaSetupView connection={{state: "not_configured", grantedScopes: []}}/>);
    expect(markup).toContain('class="studio-surface"');
    expect(markup).toContain("Paste the personal access token into the connection form.");
    expect(markup).not.toContain("access_token");
    expect(markup).not.toContain("<style");
    expect(renderToStaticMarkup(<AsanaSetupView connection={{state: "connected", grantedScopes: []}}/>)).toContain("Connected with a personal access token.");
  });

  it("explains how to replace a revoked token", () => {
    const markup = renderToStaticMarkup(<AsanaSetupView connection={{state: "revoked", grantedScopes: []}}/>);
    expect(markup).toContain("https://app.asana.com/0/my-apps");
    expect(markup).not.toContain("Reconnect");
  });

  it("renders the saved project and section with the Step's own guidance", () => {
    const markup = renderToStaticMarkup(<AsanaConfigurationUnit {...noLoads} target={projectTarget}/>);
    expect(markup).toContain("Choose the Asana project that receives approved request tasks. Leave it unsaved to use each Start Flow input&#x27;s projectId and sectionId.");
    expect(markup).toContain("<strong>Project:</strong> Facilities › Approved");
    expect(markup).toContain('value="1201000000000001"');
    expect(markup).toContain('value="1201000000000101"');
    expect(markup).toContain("the long number in the item&#x27;s Asana web address");
    expect(markup).toContain('class="studio-button" type="button">Choose project');
    expect(markup).toContain('class="studio-button studio-button-primary" type="button">Save');
  });

  it("lists loaded projects and flags a truncated list", () => {
    const markup = renderToStaticMarkup(<AsanaConfigurationUnit {...noLoads}
      projects={{resources: [{id: "1201000000000001", name: "Facilities"}, {id: "1201000000000002", name: "IT"}], isTruncated: true}}
      target={{...projectTarget, value: {workspaceId: "1100000000000001"}}}/>);
    expect(markup).toContain('<option value="1201000000000002">IT</option>');
    expect(markup).toContain("Asana returned more entries than one list can show.");
    expect(markup).toContain('class="studio-button" type="button" disabled="">Choose section');
  });

  it("blocks saving a name typed where a gid belongs and a section without a project", () => {
    const invalidProject = renderToStaticMarkup(<AsanaConfigurationUnit {...noLoads} target={{...projectTarget, value: {projectId: "Facilities"}}}/>);
    expect(invalidProject).toContain("The project gid must be digits only");
    expect(invalidProject).toContain('class="studio-button studio-button-primary" type="button" disabled="">Save');
    const sectionOnly = renderToStaticMarkup(<AsanaConfigurationUnit {...noLoads} target={{...projectTarget, value: {sectionId: "1201000000000101"}}}/>);
    expect(sectionOnly).toContain('class="studio-button studio-button-primary" type="button" disabled="">Save');
  });

  it("requires a workspace for a required workspace picker and shows load failures inline", () => {
    const markup = renderToStaticMarkup(<AsanaConfigurationUnit {...noLoads} loadError="Asana workspaces could not be loaded: denied. Enter the gid instead." target={workspaceTarget}/>);
    expect(markup).toContain("Choose the workspace that receives tasks created without a project.");
    expect(markup).toContain("Asana workspaces could not be loaded: denied. Enter the gid instead.");
    expect(markup).toContain('class="studio-button studio-button-primary" type="button" disabled="">Save');
    const empty = renderToStaticMarkup(<AsanaConfigurationUnit {...noLoads} target={workspaceTarget} workspaces={{resources: [], isTruncated: false}}/>);
    expect(empty).toContain("Asana returned no workspace for this choice.");
  });

  it("rejects an unknown unit", () => {
    expect(renderToStaticMarkup(<AsanaConfigurationUnit {...noLoads} target={{...projectTarget, unitId: "tagPicker"}}/>)).toContain("Unsupported Asana configuration unit: tagPicker");
  });
});
