// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { FrontSetupView } from "../src/setup.js";
import { FrontConfigurationUnit } from "../src/units.js";

const sentinelToken = "SENTINEL-FRONT-API-TOKEN";
const operationScope = {kind: "operation" as const, operationId: "updateConversation", flowType: "FrontConversationTriage", stepType: "AssignAndTagConversation"};

function target(unitId: string, port: string, value: Record<string, unknown>, required: boolean): ConnectorStudioConfigurationUnitTarget {
  return {
    kind: "configurationUnit", scope: operationScope, instanceId: unitId, unitId, label: `Label of ${unitId}`,
    description: `Description of ${unitId}; leave it empty to keep the current value.`, required, bindings: [{port, jsonPointer: `/${port}`}], value,
  };
}

function render(unitTarget: ConnectorStudioConfigurationUnitTarget, resources = [{id: "tag_13o8r1", label: "Triaged", detail: ""}]) {
  return renderToStaticMarkup(<FrontConfigurationUnit onLoadResources={() => undefined} onSave={() => undefined} resources={resources} target={unitTarget}/>);
}

describe("Front configuration units", () => {
  it("renders the tag picker with its description, loaded tags, saved ID, and manual fallback guidance", () => {
    const markup = render(target("tagPicker", "tagId", {tagId: "tag_13o8r1"}, true));
    expect(markup).toContain("Label of tagPicker");
    expect(markup).toContain("Description of tagPicker");
    expect(markup).toContain("Load tags");
    expect(markup).toContain("Triaged · tag_13o8r1");
    expect(markup).toContain("Tag ID fallback");
    expect(markup).toContain("GET https://api2.frontapp.com/tags");
    expect(markup).toContain("a tag name does not work");
    expect(markup).toContain('value="tag_13o8r1"');
    expect(markup).not.toContain("disabled");
  });

  it("blocks saving a required picker without a value and rejects an ID of another resource", () => {
    const unset = render(target("tagPicker", "tagId", {}, true), []);
    expect(unset).toContain('disabled=""');
    expect(unset).toContain("Select a tag");
    const invalid = render(target("teammatePicker", "teammateId", {teammateId: "tag_13o8r1"}, false), []);
    expect(invalid).toContain("A Front teammate ID looks like tea_2thf.");
    expect(invalid).toContain('disabled=""');
  });

  it("lets an optional inbox or teammate picker stay empty", () => {
    const inbox = render(target("inboxPicker", "inboxId", {}, false), []);
    expect(inbox).toContain("No inbox");
    expect(inbox).toContain("Load inboxes");
    expect(inbox).not.toContain("disabled");
    expect(render(target("teammatePicker", "teammateId", {}, false), [])).toContain("No teammate");
  });

  it("reports an unknown unit instead of rendering a guess", () => {
    expect(render(target("statusPicker", "statusId", {}, false))).toContain("Unsupported Front configuration unit: statusPicker");
  });
});

describe("Front connection surface", () => {
  it("points the API token to the host form and never renders a credential field", () => {
    const notConfigured = renderToStaticMarkup(<FrontSetupView connection={{state: "not_configured", grantedScopes: []}}/>);
    expect(notConfigured).toContain("Settings &gt; Developers &gt; API Tokens");
    expect(notConfigured).toContain("Not connected yet.");
    expect(notConfigured).not.toContain("<input");
    expect(renderToStaticMarkup(<FrontSetupView connection={{state: "connected", grantedScopes: []}}/>)).toContain("Connected.");
    const failed = renderToStaticMarkup(<FrontSetupView connection={{state: "error", grantedScopes: [], detail: "Connection file is unreadable"}}/>);
    expect(failed).toContain("Connection file is unreadable");
    expect(failed).not.toContain(sentinelToken);
  });
});
