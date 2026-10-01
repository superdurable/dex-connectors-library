// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { HelpScoutSetupView } from "../src/setup.js";
import { HelpScoutConfigurationUnit } from "../src/units.js";

const sentinelToken = "SENTINEL-HELPSCOUT-ACCESS-TOKEN";

describe("Help Scout setup", () => {
  it("guides the App ID and App Secret without OAuth consent and keeps every secret in the host form", () => {
    const notConfigured = renderToStaticMarkup(<HelpScoutSetupView connection={{state: "not_configured", grantedScopes: []}}/>);
    expect(notConfigured).toContain("App ID and App Secret");
    expect(notConfigured).toContain("leave access_token blank");
    expect(notConfigured).toContain("Not connected yet.");
    expect(notConfigured).not.toContain("Connect Help Scout");
    const connected = renderToStaticMarkup(<HelpScoutSetupView connection={{state: "connected", grantedScopes: []}}/>);
    expect(connected).toContain("Inboxes are listed with the access token the application stored");
    const failed = renderToStaticMarkup(<HelpScoutSetupView connection={{state: "error", grantedScopes: [], detail: "Connection file is unreadable"}}/>);
    expect(failed).toContain("Connection file is unreadable");
    expect(failed).not.toContain(sentinelToken);
  });

  it("renders the inbox picker for a Trigger binding with its description, blank meaning, and fallback", () => {
    const scope = {kind: "trigger" as const, triggerName: "conversationEvent", bindingName: "new-conversations", flowType: "HelpScoutConversationTriage"};
    const description = "Select the Help Scout inbox whose new conversations start this Flow; leave it empty to triage every inbox.";
    const markup = renderToStaticMarkup(<HelpScoutConfigurationUnit
      target={{kind: "configurationUnit", scope, instanceId: "inbox", unitId: "mailboxPicker", label: "Inbox", description, required: false,
        bindings: [{port: "mailboxId", jsonPointer: "/mailboxId"}], value: {mailboxId: 123}}}
      mailboxes={[{id: 123, name: "Support", email: "support@acme.example.com"}, {id: 456, name: "Billing"}]}
      onLoadMailboxes={() => undefined} onSave={() => undefined}/>);
    expect(markup).toContain(description);
    expect(markup).toContain("Load inboxes");
    expect(markup).toContain("once the application has stored an access token");
    expect(markup).toContain("Every inbox");
    expect(markup).toContain("Support · support@acme.example.com");
    expect(markup).toContain("Billing");
    expect(markup).toContain("Inbox ID fallback");
    expect(markup).toContain('value="123"');
    expect(markup).not.toContain(sentinelToken);
  });

  it("shows the guided fallback when the inbox list cannot load", () => {
    const scope = {kind: "trigger" as const, triggerName: "conversationEvent", bindingName: "new-conversations", flowType: "HelpScoutConversationTriage"};
    const markup = renderToStaticMarkup(<HelpScoutConfigurationUnit
      target={{kind: "configurationUnit", scope, instanceId: "inbox", unitId: "mailboxPicker", label: "Inbox", required: false,
        bindings: [{port: "mailboxId", jsonPointer: "/mailboxId"}], value: {}}}
      loadError="Help Scout inboxes could not be loaded. The list needs the access token the application stores on its first Help Scout call; until then, enter the inbox ID below."
      mailboxes={[]} onLoadMailboxes={() => undefined} onSave={() => undefined}/>);
    expect(markup).toContain("until then, enter the inbox ID below");
    expect(markup).toContain('placeholder="12345"');
  });

  it("rejects an unknown unit", () => {
    const scope = {kind: "operation" as const, operationId: "searchConversations", flowType: "Flow", stepType: "Search"};
    const markup = renderToStaticMarkup(<HelpScoutConfigurationUnit
      target={{kind: "configurationUnit", scope, instanceId: "x", unitId: "textInput", label: "X", required: false, bindings: [], value: {}}}
      mailboxes={[]} onLoadMailboxes={() => undefined} onSave={() => undefined}/>);
    expect(markup).toContain("Unsupported Help Scout configuration unit: textInput");
  });
});
