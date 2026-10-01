// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { TypeformSetupView } from "../src/setup.js";
import { TypeformConfigurationUnit } from "../src/units.js";

const sentinelToken = "SENTINEL-TYPEFORM-ACCESS-TOKEN";

describe("Typeform setup", () => {
  it("keeps the token in the host form and explains each connection state", () => {
    const notConfigured = renderToStaticMarkup(<TypeformSetupView connection={{state: "not_configured", grantedScopes: [], authMethodIds: ["personal-access-token"]}}/>);
    expect(notConfigured).toContain("Paste the personal access token, and the webhook secret");
    expect(notConfigured).toContain("never sends them to this panel");
    const connected = renderToStaticMarkup(<TypeformSetupView connection={{state: "connected", grantedScopes: [], authMethodIds: ["personal-access-token"]}}/>);
    expect(connected).toContain("Forms are listed live from Typeform");
    const revoked = renderToStaticMarkup(<TypeformSetupView connection={{state: "revoked", grantedScopes: [], authMethodIds: ["personal-access-token"]}}/>);
    expect(revoked).toContain("https://admin.typeform.com/user/tokens");
    for (const markup of [notConfigured, connected, revoked]) expect(markup).not.toContain(sentinelToken);
  });

  it("renders the form picker for a Trigger binding with its description, blank meaning, and fallback", () => {
    const scope = {kind: "trigger" as const, triggerName: "responseSubmitted", bindingName: "response-submitted", flowType: "TypeformResponseRecorder"};
    const description = "Select the Typeform form whose submissions start this Flow; leave it empty to record submissions of every form.";
    const markup = renderToStaticMarkup(<TypeformConfigurationUnit
      target={{kind: "configurationUnit", scope, instanceId: "form", unitId: "formPicker", label: "Form", description, required: false,
        bindings: [{port: "formId", jsonPointer: "/formId"}], value: {formId: "u6nXL7"}}}
      forms={[
        {id: "u6nXL7", title: "Lead intake", isPublic: true},
        {id: "lT4Z3j", title: "Closed survey", isPublic: false},
      ]}
      onLoadForms={() => undefined} onSave={() => undefined}/>);
    expect(markup).toContain(description);
    expect(markup).toContain("Load forms");
    expect(markup).toContain("Every form");
    expect(markup).toContain("Lead intake · u6nXL7");
    expect(markup).toContain("Closed survey · lT4Z3j (closed)");
    expect(markup).toContain("Form ID fallback");
    expect(markup).toContain("https://form.typeform.com/to/u6nXL7");
    expect(markup).not.toContain(sentinelToken);
  });

  it("asks for a form when the unit is required, and rejects an unknown unit", () => {
    const scope = {kind: "operation" as const, operationId: "upsertWebhook", flowType: "Flow", stepType: "RegisterWebhook"};
    const required = renderToStaticMarkup(<TypeformConfigurationUnit
      target={{kind: "configurationUnit", scope, instanceId: "form", unitId: "formPicker", label: "Form", description: "Pick the form.", required: true,
        bindings: [{port: "formId", jsonPointer: "/formId"}], value: {}}}
      forms={[]} onLoadForms={() => undefined} onSave={() => undefined}/>);
    expect(required).toContain("Select a form");
    const unknown = renderToStaticMarkup(<TypeformConfigurationUnit
      target={{kind: "configurationUnit", scope, instanceId: "x", unitId: "textInput", label: "X", required: false, bindings: [], value: {}}}
      forms={[]} onLoadForms={() => undefined} onSave={() => undefined}/>);
    expect(unknown).toContain("Unsupported Typeform configuration unit: textInput");
  });
});
