// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectorConnectionView, ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { PipedriveSetupView } from "../src/setup.js";
import { PipedriveConfigurationUnit, type PipedriveUnitProps } from "../src/units.js";

const scope = {kind: "operation" as const, operationId: "createObject", flowType: "PipedriveDealIntake", stepType: "CreatePipedriveDeal"};
// Keys are built at run time, so no token-shaped 40-character literal is checked in.
const customFieldKey = "ab".repeat(20);
const tierFieldKey = "cd".repeat(20);

function renderSetup(connection: ConnectorConnectionView): string {
  return renderToStaticMarkup(<PipedriveSetupView connection={connection} onConnect={() => undefined} onReconnect={() => undefined}/>);
}

function renderUnit(target: Partial<ConnectorStudioConfigurationUnitTarget> & Pick<ConnectorStudioConfigurationUnitTarget, "unitId">, props: Partial<PipedriveUnitProps> = {}): string {
  return renderToStaticMarkup(<PipedriveConfigurationUnit
    canListFromPipedrive
    customFieldObjectType=""
    customFields={[]}
    onLoadCustomFields={() => undefined}
    onLoadOwners={() => undefined}
    onLoadPipelines={() => undefined}
    onSave={() => undefined}
    owners={[]}
    pipelines={[]}
    {...props}
    target={{kind: "configurationUnit", scope, instanceId: "unit", label: "Unit", required: false, bindings: [], value: {}, ...target}}
  />);
}

describe("Pipedrive connection setup", () => {
  it("guides a Personal API token without an OAuth button or the token", () => {
    const markup = renderSetup({state: "not_configured", grantedScopes: [], authMethodIds: ["api-token"]});
    expect(markup).toContain("Paste the Personal API token from Pipedrive Personal preferences &gt; API");
    expect(markup).not.toContain("Connect Pipedrive");
  });

  it("offers OAuth connect and reconnect only for the OAuth method and explains manual IDs", () => {
    expect(renderSetup({state: "not_configured", grantedScopes: [], authMethodIds: ["pipedrive-oauth"]})).toContain("Connect Pipedrive");
    const revoked = renderSetup({state: "revoked", grantedScopes: [], authMethodIds: ["pipedrive-oauth"]});
    expect(revoked).toContain("Connection revoked. Authorize Pipedrive again");
    expect(revoked).toContain("Reconnect Pipedrive");
    const revokedToken = renderSetup({state: "revoked", grantedScopes: [], authMethodIds: ["api-token"]});
    expect(revokedToken).toContain("Paste a current Personal API token");
    expect(revokedToken).not.toContain("Reconnect Pipedrive");
    const connected = renderSetup({state: "connected", grantedScopes: [], authMethodIds: ["pipedrive-oauth"]});
    expect(connected).toContain("company&#x27;s API domain");
    expect(connected).toContain("enter those IDs by hand");
  });

  it("asks for a method before one is chosen and reports errors", () => {
    expect(renderSetup({state: "not_configured", grantedScopes: []})).toContain("Choose Personal API token for one Pipedrive company");
    expect(renderSetup({state: "connected", grantedScopes: [], authMethodIds: ["api-token"]})).toContain("Connected with a Personal API token.");
    expect(renderSetup({state: "error", grantedScopes: [], detail: "Dex Web could not load the connection"})).toContain("Dex Web could not load the connection");
  });
});

describe("Pipedrive configuration units", () => {
  it("renders loaded users, the numeric fallback, and blank owner semantics", () => {
    const markup = renderUnit({
      unitId: "ownerPicker", label: "Lead owner", description: "Select the Pipedrive user who owns each upserted lead person.",
      bindings: [{port: "ownerId", jsonPointer: "/ownerId"}], value: {ownerId: "7"},
    }, {owners: [{id: "7", name: "Ada Lovelace", email: "ada@example.com"}]});
    expect(markup).toContain("Load users");
    expect(markup).toContain("Company settings &gt; Manage users");
    expect(markup).toContain("Ada Lovelace · ada@example.com · 7");
    expect(markup).toContain("No owner");
    expect(markup).toContain("Owner ID fallback");
    expect(markup).toContain("Blank sets no owner.");

    const invalid = renderUnit({unitId: "ownerPicker", value: {ownerId: "ada"}});
    expect(invalid).toContain("A Pipedrive user ID contains only digits.");
    expect(invalid).toMatch(/<button[^>]*disabled=""[^>]*>Save<\/button>/);
  });

  it("renders pipelines, the selected pipeline's stages, and requires both numeric IDs", () => {
    const pipelines = [{id: "1", name: "Sales", stages: [{id: "3", name: "Qualified"}, {id: "4", name: "Negotiation"}]}];
    const markup = renderUnit({
      unitId: "dealStagePicker", label: "Deal stage", required: true, description: "Select the pipeline and stage.",
      bindings: [{port: "pipelineId", jsonPointer: "/pipelineId"}, {port: "stageId", jsonPointer: "/stageId"}],
      value: {pipelineId: "1", stageId: "3"},
    }, {pipelines, loadError: "Pipedrive pipelines could not be loaded"});
    expect(markup).toContain("Load pipelines");
    expect(markup).toContain("Sales · 1");
    expect(markup).toContain(`<option value="3" selected="">Qualified · 3</option>`);
    expect(markup).toContain("Negotiation · 4");
    expect(markup).toContain("Pipeline ID fallback");
    expect(markup).toContain("Pipedrive pipelines could not be loaded. Enter the ID below instead.");

    const halfFilled = renderUnit({unitId: "dealStagePicker", required: true, value: {pipelineId: "1"}});
    expect(halfFilled).toContain("Choose both a pipeline and a stage.");
    expect(halfFilled).toMatch(/<button[^>]*disabled=""[^>]*>Save<\/button>/);
    expect(renderUnit({unitId: "dealStagePicker", value: {pipelineId: "sales", stageId: "3"}})).toContain("Pipeline and stage IDs contain only digits.");
  });

  it("renders the loaded custom fields of the chosen object type and blank semantics", () => {
    const markup = renderUnit({
      unitId: "customFieldPicker", label: "Lead source field", description: "Select deals and a text custom field.",
      value: {objectType: "deals", fieldKey: customFieldKey},
    }, {customFieldObjectType: "deals", customFields: [
      {key: customFieldKey, name: "Lead source", fieldType: "varchar", isText: true},
      {key: tierFieldKey, name: "Tier", fieldType: "enum", isText: false},
    ]});
    expect(markup).toContain("Load custom fields");
    expect(markup).toContain("Company settings &gt; Data fields");
    expect(markup).toContain(`<option value="${customFieldKey}" selected="">Lead source (varchar) · ${customFieldKey}</option>`);
    expect(markup).toContain("Tier (enum, not text)");
    expect(markup).toContain("Blank writes no custom field.");
    for (const objectType of ["persons", "organizations", "deals"]) expect(markup).toContain(`· ${objectType}`);
    expect(renderUnit({unitId: "customFieldPicker", value: {objectType: "deals", fieldKey: "Lead source"}})).toContain("40 lowercase hexadecimal characters");
    expect(renderUnit({unitId: "customFieldPicker", value: {objectType: "leads"}})).toContain("The saved object type is not supported");
  });

  it("offers only manual IDs on an OAuth connection and shows saved IDs before lists load", () => {
    const manual = renderUnit({unitId: "ownerPicker", value: {ownerId: "7"}}, {canListFromPipedrive: false});
    expect(manual).toContain("Lists load only for a Personal API token connection");
    expect(manual).not.toContain("Load users");
    expect(manual).toContain(`<option value="7" selected="">Saved user · 7</option>`);
    const stage = renderUnit({unitId: "dealStagePicker", value: {pipelineId: "1", stageId: "3"}}, {canListFromPipedrive: false});
    expect(stage).not.toContain("Load pipelines");
    expect(stage).toContain(`<option value="1" selected="">Saved pipeline · 1</option>`);
  });

  it("never renders credential material", () => {
    for (const unitId of ["ownerPicker", "dealStagePicker", "customFieldPicker"]) {
      const markup = renderUnit({unitId, value: {}});
      for (const secret of ["api_token", "access_token", "refresh_token", "client_secret", "x-api-token"]) expect(markup).not.toContain(secret);
    }
    expect(renderUnit({unitId: "unknownUnit"})).toContain("Unsupported Pipedrive configuration unit: unknownUnit");
  });
});
