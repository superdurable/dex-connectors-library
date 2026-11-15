// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { HiverSetupView } from "../src/setup.js";
import { HiverConfigurationUnit } from "../src/units.js";

const sentinelKey = "SENTINEL-HIVER-API-KEY";
const scope = {kind: "trigger" as const, triggerName: "conversationReceived", bindingName: "new-support-conversations", flowType: "HiverNewConversationIntake"};

describe("Hiver setup", () => {
  it("guides the API key without OAuth consent and keeps the secret in the host form", () => {
    const notConfigured = renderToStaticMarkup(<HiverSetupView connection={{state: "not_configured", grantedScopes: []}}/>);
    expect(notConfigured).toContain("API key into api_key in the form above");
    expect(notConfigured).toContain("never shows it here");
    expect(notConfigured).toContain("Not connected yet.");
    expect(notConfigured).not.toContain("Connect Hiver");
    const connected = renderToStaticMarkup(<HiverSetupView connection={{state: "connected", grantedScopes: []}}/>);
    expect(connected).toContain("Shared inboxes are listed with the saved API key");
    const failed = renderToStaticMarkup(<HiverSetupView connection={{state: "error", grantedScopes: [], detail: "Connection file is unreadable"}}/>);
    expect(failed).toContain("Connection file is unreadable");
    expect(failed).not.toContain(sentinelKey);
  });

  it("renders the inbox picker for a Trigger binding with its description, saved value, and fallback", () => {
    const description = "Choose the Hiver shared inbox whose new conversations start this Flow. It cannot be blank.";
    const markup = renderToStaticMarkup(<HiverConfigurationUnit
      target={{kind: "configurationUnit", scope, instanceId: "sharedInbox", unitId: "inboxPicker", label: "Shared inbox", description, required: true,
        bindings: [{port: "inboxId", jsonPointer: "/inboxId"}], value: {inboxId: "105902"}}}
      inboxes={[{id: "105902", name: "Support", email: "support@acme.example.com"}, {id: "202", name: "Billing"}]}
      onLoadInboxes={() => undefined} onSave={() => undefined}/>);
    expect(markup).toContain(description);
    expect(markup).toContain("Load inboxes");
    expect(markup).toContain("Select a shared inbox");
    expect(markup).toContain("Support · support@acme.example.com");
    expect(markup).toContain("Billing");
    expect(markup).toContain("Inbox ID fallback");
    expect(markup).toContain("https://api2.hiverhq.com/v1/inboxes");
    expect(markup).toContain('value="105902"');
    expect(markup).not.toContain(sentinelKey);
  });

  it("explains a blank required inbox and shows the guided fallback when the list cannot load", () => {
    const markup = renderToStaticMarkup(<HiverConfigurationUnit
      target={{kind: "configurationUnit", scope, instanceId: "sharedInbox", unitId: "inboxPicker", label: "Shared inbox", required: true,
        bindings: [{port: "inboxId", jsonPointer: "/inboxId"}], value: {}}}
      isInboxListTruncated
      loadError="Hiver shared inboxes could not be loaded. Check that the connection's API key is saved and its app toggle is on in Hiver under Admin Panel > Integrations > Developer APIs; until then, enter the inbox ID below."
      inboxes={[]} onLoadInboxes={() => undefined} onSave={() => undefined}/>);
    expect(markup).toContain("until then, enter the inbox ID below");
    expect(markup).toContain("it cannot be blank");
    expect(markup).toContain("Only the first inboxes are listed");
    expect(markup).toContain('placeholder="105902"');
  });

  it("rejects an unknown unit", () => {
    const operationScope = {kind: "operation" as const, operationId: "listConversations", flowType: "Flow", stepType: "List"};
    const markup = renderToStaticMarkup(<HiverConfigurationUnit
      target={{kind: "configurationUnit", scope: operationScope, instanceId: "x", unitId: "textInput", label: "X", required: false, bindings: [], value: {}}}
      inboxes={[]} onLoadInboxes={() => undefined} onSave={() => undefined}/>);
    expect(markup).toContain("Unsupported Hiver configuration unit: textInput");
  });
});
