// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { ServiceManagementSetupView } from "../src/setup.js";
import { ServiceManagementConfigurationUnit, SitePickerUnit } from "../src/units.js";

const siteID = "1324a887-45db-1bf4-1e99-ef0ff456d421";
const noop = () => undefined;

const deskTarget: ConnectorStudioConfigurationUnitTarget = {
  kind: "configurationUnit", scope: {kind: "operation", operationId: "findCustomerByEmail", flowType: "JiraServiceManagementSupportRequest", stepType: "FindRequester"},
  instanceId: "supportDesk", unitId: "serviceDeskPicker", label: "Support service desk",
  description: "Choose the service desk whose customers are looked up. Leave it unsaved to use each Start Flow input's serviceDeskId and projectKey.", required: false,
  bindings: [{port: "serviceDeskId", jsonPointer: "/serviceDeskId"}, {port: "projectKey", jsonPointer: "/projectKey"}, {port: "serviceDeskName", jsonPointer: "/serviceDeskName"}],
  value: {serviceDeskId: "10", projectKey: "ITH", serviceDeskName: "IT Help"},
};

const requestTypeTarget: ConnectorStudioConfigurationUnitTarget = {
  kind: "configurationUnit", scope: {kind: "operation", operationId: "createTicket", flowType: "JiraServiceManagementSupportRequest", stepType: "CreateSupportTicket"},
  instanceId: "supportRequestType", unitId: "requestTypePicker", label: "Support request type",
  description: "Choose the service desk and then the request type that new support requests use. Leave it unsaved to use each Start Flow input's requestTypeId.", required: false,
  bindings: [{port: "serviceDeskId", jsonPointer: "/serviceDeskId"}, {port: "requestTypeId", jsonPointer: "/requestTypeId"}, {port: "requestTypeName", jsonPointer: "/requestTypeName"}],
  value: {serviceDeskId: "10", requestTypeId: "25", requestTypeName: "Get IT help"},
};

function renderUnit(target: ConnectorStudioConfigurationUnitTarget, connectionCloudId = siteID, extra: Record<string, unknown> = {}): string {
  return renderToStaticMarkup(<ServiceManagementConfigurationUnit connectionCloudId={connectionCloudId} onChooseRequestType={noop}
    onChooseServiceDesk={noop} onSave={noop} target={target} {...extra}/>);
}

describe("Jira Service Management setup", () => {
  it("shows the connected agent without any credential", () => {
    const markup = renderToStaticMarkup(<ServiceManagementSetupView connection={{state: "connected", accountEmail: "ada@example.com", grantedScopes: [
      "read:servicedesk-request", "write:servicedesk-request", "manage:servicedesk-customer", "read:jira-work", "write:jira-work", "offline_access",
    ]}} onConnect={noop} onReconnect={noop}/>);
    expect(markup).toContain("ada@example.com");
    expect(markup).toContain("Choose the site in the connection form.");
    for (const secret of ["access_token", "refresh_token", "client_secret"]) expect(markup).not.toContain(secret);
  });

  it.each([
    ["revoked", "Connection revoked"],
    ["insufficient_scope", "Additional permission required"],
  ] as const)("shows a reconnect action for %s", (state, label) => {
    const markup = renderToStaticMarkup(<ServiceManagementSetupView connection={{state, grantedScopes: []}} onConnect={noop} onReconnect={noop}/>);
    expect(markup).toContain(label);
    expect(markup).toContain("accept every requested scope");
    expect(markup).toContain("Reconnect Jira Service Management");
  });

  it("renders the site picker with manual cloudId guidance and validation", () => {
    const markup = renderToStaticMarkup(<SitePickerUnit onChooseSite={noop} onSave={noop}
      sites={[{cloudId: siteID, name: "Help", url: "https://help.atlassian.net"}]}
      target={{kind: "connection", unitId: "sitePicker", bindings: [{port: "cloudId", jsonPointer: "/cloudId"}], value: {cloudId: "help.atlassian.net"}}}/>);
    expect(markup).toContain('class="studio-surface"');
    expect(markup).toContain("Leave it blank to use the only such site the authorization grants.");
    expect(markup).toContain("Help (https://help.atlassian.net)");
    expect(markup).toContain("https://&lt;your-site&gt;.atlassian.net/_edge/tenant_info");
    expect(markup).toContain('role="alert">The cloudId must be a UUID; a site URL is not accepted.');
    expect(markup).toContain('class="studio-button studio-button-primary" type="button" disabled="">Save');
    expect(markup).not.toContain("<style");
  });

  it("renders the saved service desk with its Step guidance and manual entry", () => {
    const markup = renderUnit(deskTarget);
    expect(markup).toContain("Choose the service desk whose customers are looked up. Leave it unsaved to use each Start Flow input&#x27;s serviceDeskId and projectKey.");
    expect(markup).toContain("<strong>Service desk:</strong> IT Help (ITH)");
    expect(markup).toContain('value="10"');
    expect(markup).toContain('value="ITH"');
    expect(markup).toContain("https://&lt;your-site&gt;.atlassian.net/rest/servicedeskapi/servicedesk");
    expect(markup).toContain("Jira &gt; Projects &gt; View all projects");
    expect(markup).not.toContain("Site cloudId for listing");
  });

  it("asks for a listing site and flags a truncated list when the host reports no cloudId", () => {
    const markup = renderUnit({...deskTarget, value: {}}, "", {isListTruncated: true, serviceDesks: [{id: "10", projectKey: "ITH", name: "IT Help"}]});
    expect(markup).toContain("Site cloudId for listing");
    expect(markup).toContain('class="studio-button" type="button" disabled="">Choose service desk');
    expect(markup).toContain("IT Help (ITH)");
    expect(markup).toContain("more service desks than one list can show");
  });

  it("rejects half-entered desk values", () => {
    const markup = renderUnit({...deskTarget, value: {serviceDeskId: "10"}});
    expect(markup).toContain("Enter both a numeric service desk ID and an uppercase project key, or leave both blank.");
    expect(markup).toContain('class="studio-button studio-button-primary" type="button" disabled="">Save');
  });

  it("renders the saved request type and lists types for the chosen desk", () => {
    const markup = renderUnit(requestTypeTarget, siteID, {requestTypes: [{id: "25", name: "Get IT help"}, {id: "26", name: "Request new hardware"}]});
    expect(markup).toContain("Choose the service desk and then the request type that new support requests use.");
    expect(markup).toContain("<strong>Request type:</strong> Get IT help");
    expect(markup).toContain("Request new hardware");
    expect(markup).toContain('class="studio-button" type="button">Choose request type');
    expect(markup).toContain("/rest/servicedeskapi/servicedesk/&lt;service desk ID&gt;/requesttype");
  });

  it("disables request type listing until a desk is known and requires a type for a required unit", () => {
    const markup = renderUnit({...requestTypeTarget, required: true, value: {}});
    expect(markup).toContain('class="studio-button" type="button" disabled="">Choose request type');
    expect(markup).toContain('class="studio-button studio-button-primary" type="button" disabled="">Save');
  });
});
