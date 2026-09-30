// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectorConnectionView, ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { HubSpotSetupView } from "../src/setup.js";
import { HubSpotConfigurationUnit, type HubSpotUnitProps } from "../src/units.js";

const scope = {kind: "operation" as const, operationId: "updateObject", flowType: "HubSpotLeadQualification", stepType: "AdvanceHubSpotOpenDeal"};

function renderSetup(connection: ConnectorConnectionView): string {
  return renderToStaticMarkup(<HubSpotSetupView connection={connection} onConnect={() => undefined} onReconnect={() => undefined}/>);
}

function renderUnit(target: Partial<ConnectorStudioConfigurationUnitTarget> & Pick<ConnectorStudioConfigurationUnitTarget, "unitId">, props: Partial<HubSpotUnitProps> = {}): string {
  return renderToStaticMarkup(<HubSpotConfigurationUnit
    onLoadOwners={() => undefined}
    onLoadPipelines={() => undefined}
    onSave={() => undefined}
    owners={[]}
    pipelines={[]}
    {...props}
    target={{kind: "configurationUnit", scope, instanceId: "unit", label: "Unit", required: false, bindings: [], value: {}, ...target}}
  />);
}

describe("HubSpot connection setup", () => {
  it("guides a private app token without an OAuth button or the token", () => {
    const markup = renderSetup({state: "not_configured", grantedScopes: [], authMethodIds: ["private-app-token"]});
    expect(markup).toContain("Paste the private app access token into the connection form");
    expect(markup).not.toContain("Connect HubSpot");
    expect(markup).not.toContain("pat-na1-");
  });

  it("offers OAuth connect and reconnect only for the OAuth method", () => {
    expect(renderSetup({state: "not_configured", grantedScopes: [], authMethodIds: ["hubspot-oauth"]})).toContain("Connect HubSpot");
    const revoked = renderSetup({state: "revoked", grantedScopes: [], authMethodIds: ["hubspot-oauth"]});
    expect(revoked).toContain("Connection revoked. Authorize HubSpot again");
    expect(revoked).toContain("Reconnect HubSpot");
    const revokedToken = renderSetup({state: "revoked", grantedScopes: [], authMethodIds: ["private-app-token"]});
    expect(revokedToken).toContain("Paste a current private app access token");
    expect(revokedToken).not.toContain("Reconnect HubSpot");
  });

  it("asks for a method before one is chosen and reports the connected method", () => {
    expect(renderSetup({state: "not_configured", grantedScopes: []})).toContain("Choose Private app access token for one HubSpot account");
    expect(renderSetup({state: "connected", grantedScopes: [], authMethodIds: ["hubspot-oauth"]})).toContain("refreshes the access token automatically");
    expect(renderSetup({state: "connected", grantedScopes: [], authMethodIds: ["private-app-token"]})).toContain("Connected with a private app access token.");
    expect(renderSetup({state: "error", grantedScopes: [], detail: "Dex Web could not load the connection"})).toContain("Dex Web could not load the connection");
  });
});

describe("HubSpot configuration units", () => {
  it("renders the object type picker with HubSpot's three object names", () => {
    const markup = renderUnit({unitId: "objectTypePicker", label: "Record type", description: "Select the object this Step reads.", value: {objectType: "deals"}});
    expect(markup).toContain("Record type");
    expect(markup).toContain("Select the object this Step reads.");
    expect(markup).toContain("Saves HubSpot&#x27;s object name: contacts, companies, or deals.");
    for (const objectType of ["contacts", "companies", "deals"]) expect(markup).toContain(`· ${objectType}`);
    expect(markup).toContain(`<option value="deals" selected="">`);
    expect(renderUnit({unitId: "objectTypePicker", value: {objectType: "tickets"}})).toContain("The saved object type is not supported");
  });

  it("renders loaded owners, the numeric fallback, and blank owner semantics", () => {
    const markup = renderUnit({
      unitId: "ownerPicker", label: "Lead owner",
      description: "Select the HubSpot owner assigned to each upserted lead contact; blank leaves the contact's current owner unchanged.",
      bindings: [{port: "ownerId", jsonPointer: "/ownerId"}], value: {ownerId: "77"},
    }, {owners: [{id: "77", displayName: "Ada Lovelace", email: "ada@example.com", isQueue: false}], isOwnerListTruncated: true});
    expect(markup).toContain("Load owners");
    expect(markup).toContain("Settings &gt; Users &amp; Teams");
    expect(markup).toContain("Ada Lovelace · ada@example.com · 77");
    expect(markup).toContain("No owner (leave unchanged)");
    expect(markup).toContain("Owner ID fallback");
    expect(markup).toContain("Blank means no owner is set.");
    expect(markup).toContain("HubSpot has more owners than this list shows");
    expect(markup).not.toContain("disabled=\"\">Save");

    const invalid = renderUnit({unitId: "ownerPicker", value: {ownerId: "ada"}});
    expect(invalid).toContain("An owner ID contains only digits.");
    expect(invalid).toMatch(/<button[^>]*disabled=""[^>]*>Save<\/button>/);
  });

  it("renders deal pipelines, the selected pipeline's stages, and requires both IDs", () => {
    const pipelines = [{id: "default", label: "Sales Pipeline", stages: [
      {id: "appointmentscheduled", label: "Appointment scheduled", isClosed: false},
      {id: "closedwon", label: "Closed won", isClosed: true},
    ]}];
    const markup = renderUnit({
      unitId: "dealStagePicker", label: "Qualified deal stage", required: true,
      description: "Select the deal pipeline searched for the contact's open deal and the stage that deal moves to.",
      bindings: [{port: "pipelineId", jsonPointer: "/pipelineId"}, {port: "stageId", jsonPointer: "/stageId"}],
      value: {pipelineId: "default", stageId: "appointmentscheduled"},
    }, {pipelines, loadError: "HubSpot deal pipelines could not be loaded"});
    expect(markup).toContain("Load deal pipelines");
    expect(markup).toContain("Settings &gt; Objects &gt; Deals &gt; Pipelines");
    expect(markup).toContain("Sales Pipeline · default");
    expect(markup).toContain("Appointment scheduled · appointmentscheduled");
    expect(markup).toContain("Closed won (closed) · closedwon");
    expect(markup).toContain("Pipeline ID fallback");
    expect(markup).toContain("Stage ID fallback");
    expect(markup).toContain("HubSpot deal pipelines could not be loaded. Enter the ID below instead.");

    const halfFilled = renderUnit({unitId: "dealStagePicker", required: true, value: {pipelineId: "default"}});
    expect(halfFilled).toContain("Choose both a pipeline and a stage.");
    expect(halfFilled).toMatch(/<button[^>]*disabled=""[^>]*>Save<\/button>/);
  });

  it("shows saved IDs before HubSpot lists load", () => {
    const owner = renderUnit({unitId: "ownerPicker", value: {ownerId: "77"}});
    expect(owner).toContain(`<option value="77" selected="">Saved owner · 77</option>`);
    const stage = renderUnit({unitId: "dealStagePicker", value: {pipelineId: "default", stageId: "qualifiedtobuy"}});
    expect(stage).toContain(`<option value="default" selected="">Saved pipeline · default</option>`);
    expect(stage).toContain(`<option value="qualifiedtobuy" selected="">Saved stage · qualifiedtobuy</option>`);
    const loaded = renderUnit({unitId: "ownerPicker", value: {ownerId: "77"}}, {owners: [{id: "77", displayName: "Ada", isQueue: false}]});
    expect(loaded).not.toContain("Saved owner");
  });

  it("never renders credential material", () => {
    for (const unitId of ["objectTypePicker", "ownerPicker", "dealStagePicker"]) {
      const markup = renderUnit({unitId, value: {}});
      expect(markup).not.toContain("access_token");
      expect(markup).not.toContain("refresh_token");
      expect(markup).not.toContain("client_secret");
    }
    expect(renderUnit({unitId: "unknownUnit"})).toContain("Unsupported HubSpot configuration unit: unknownUnit");
  });
});
