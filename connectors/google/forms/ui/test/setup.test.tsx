// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { FormsSetupView } from "../src/setup.js";
import { FormsConfigurationUnit } from "../src/units.js";

// formPickerTarget mirrors the examples/response-recorder ReadForm unit.
function formPickerTarget(overrides: Partial<ConnectorStudioConfigurationUnitTarget> = {}): ConnectorStudioConfigurationUnitTarget {
  return {
    kind: "configurationUnit",
    scope: {kind: "operation", operationId: "getForm", flowType: "GoogleFormsResponseRecorder", stepType: "ReadForm"},
    instanceId: "form",
    unitId: "formPicker",
    label: "Form",
    description: "Choose the Google Form whose responses this Flow records; the picker lists the forms in the connected account's Google Drive and saves the form ID and title. The connected account must be able to edit the form to read its responses. Blank fails each run at its first Step.",
    required: true,
    bindings: [{port: "formId", jsonPointer: "/formId"}, {port: "formTitle", jsonPointer: "/formTitle"}],
    value: {},
    ...overrides,
  };
}

const noop = () => undefined;

describe("Google Forms setup", () => {
  it("shows the connected account without credential material", () => {
    const markup = renderToStaticMarkup(<FormsSetupView connection={{state: "connected", accountEmail: "owner@example.com", grantedScopes: [
      "https://www.googleapis.com/auth/forms.body.readonly", "https://www.googleapis.com/auth/forms.responses.readonly", "https://www.googleapis.com/auth/drive.metadata.readonly",
    ]}} onConnect={noop} onReconnect={noop}/>);
    expect(markup).toContain("owner@example.com");
    expect(markup).not.toContain("Choose form");
    expect(markup).not.toContain("access_token");
    expect(markup).not.toContain("refresh_token");
  });

  it("offers to connect an unconfigured connection", () => {
    const markup = renderToStaticMarkup(<FormsSetupView connection={{state: "not_configured", grantedScopes: []}} onConnect={noop} onReconnect={noop}/>);
    expect(markup).toContain("Connect Google Forms");
  });

  it.each([
    ["revoked", "Connection revoked"],
    ["insufficient_scope", "Additional permission required"],
  ] as const)("shows a reconnect action for %s", (state, label) => {
    const markup = renderToStaticMarkup(<FormsSetupView connection={{state, grantedScopes: []}} onConnect={noop} onReconnect={noop}/>);
    expect(markup).toContain(label);
    expect(markup).toContain("keep every requested permission checked");
    expect(markup).toContain("Reconnect Google Forms");
  });

  it("renders every form picker field with the Step's guidance", () => {
    const markup = renderToStaticMarkup(<FormsConfigurationUnit forms={[{id: "1FAIpQLintake", title: "Vendor intake"}]} onChooseForm={noop} onSave={noop}
      target={formPickerTarget({value: {formId: "1FAIpQLintake", formTitle: "Vendor intake"}})}/>);
    expect(markup).toContain('class="studio-surface"');
    expect(markup).toContain("Choose the Google Form whose responses this Flow records");
    expect(markup).toContain("Blank fails each run at its first Step.");
    expect(markup).toContain(">Choose form<");
    expect(markup).toContain("<strong>Form:</strong> Vendor intake");
    expect(markup).toContain('<option value="">Select a form</option>');
    expect(markup).toContain('<option value="1FAIpQLintake" selected="">Vendor intake</option>');
    expect(markup).toContain("Paste a form ID or its docs.google.com/forms/d/FORM_ID/edit link when the form is not listed. Leave blank for no form.");
    expect(markup).toContain('value="1FAIpQLintake"');
    expect(markup).toContain('class="studio-button studio-button-primary" type="button">Save');
    expect(markup).not.toContain("access_token");
    expect(markup).not.toContain("<style");
  });

  it("requires a form only when the Flow marks the unit required", () => {
    const required = renderToStaticMarkup(<FormsConfigurationUnit onChooseForm={noop} onSave={noop} target={formPickerTarget()}/>);
    expect(required).toContain('class="studio-button studio-button-primary" type="button" disabled="">Save');
    const optional = renderToStaticMarkup(<FormsConfigurationUnit forms={[{id: "f1", title: "Survey"}]} onChooseForm={noop} onSave={noop} target={formPickerTarget({required: false})}/>);
    expect(optional).toContain('<option value="" selected="">No form</option>');
    expect(optional).toContain('class="studio-button studio-button-primary" type="button">Save');
  });

  it("accepts an edit link and saves its form ID", () => {
    const markup = renderToStaticMarkup(<FormsConfigurationUnit onChooseForm={noop} onSave={noop}
      target={formPickerTarget({value: {formId: "https://docs.google.com/forms/d/1FAIpQLintake/edit"}})}/>);
    expect(markup).not.toContain('role="alert"');
    expect(markup).toContain('class="studio-button studio-button-primary" type="button">Save');
  });

  it("explains why a responder link cannot be used", () => {
    const markup = renderToStaticMarkup(<FormsConfigurationUnit onChooseForm={noop} onSave={noop}
      target={formPickerTarget({value: {formId: "https://docs.google.com/forms/d/e/1FAIpQLSresponder/viewform"}})}/>);
    expect(markup).toContain('role="alert">That is the link respondents open, which carries a different ID.');
    expect(markup).toContain('class="studio-button studio-button-primary" type="button" disabled="">Save');
  });

  it("rejects a value that is neither an ID nor an edit link", () => {
    const markup = renderToStaticMarkup(<FormsConfigurationUnit onChooseForm={noop} onSave={noop} target={formPickerTarget({value: {formId: "not a form"}})}/>);
    expect(markup).toContain('role="alert">Enter a Google Forms form ID or a docs.google.com/forms/d/FORM_ID/edit link.');
  });

  it("points to the form ID field when Drive returns more forms than the list shows", () => {
    const markup = renderToStaticMarkup(<FormsConfigurationUnit forms={[{id: "f1", title: "Survey"}]} isFormListTruncated onChooseForm={noop} onSave={noop} target={formPickerTarget()}/>);
    expect(markup).toContain('role="status">Google Drive returned more forms than one list can show. Enter a form ID to use one that is not listed.');
  });

  it("shows a form list error inline", () => {
    const markup = renderToStaticMarkup(<FormsConfigurationUnit loadError="The caller does not have permission" onChooseForm={noop} onSave={noop} target={formPickerTarget()}/>);
    expect(markup).toContain('role="alert">The caller does not have permission');
  });
});
