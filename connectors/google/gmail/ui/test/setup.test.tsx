// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { GmailSetupView } from "../src/setup.js";
import { GmailConfigurationUnit } from "../src/units.js";

describe("Gmail setup", () => {
  it("shows only the verified primary sender", () => {
    const markup = renderToStaticMarkup(<GmailSetupView connection={{state: "connected", accountEmail: "owner@example.com", grantedScopes: ["openid", "email"]}} onConnect={() => undefined} onReconnect={() => undefined}/>);
    expect(markup).toContain("owner@example.com");
    expect(markup).not.toContain("requester@example.com");
    expect(markup).not.toContain("access_token");
    expect(markup).not.toContain("Revoke connection");
  });

  it.each([
    ["revoked", "Connection revoked"],
    ["insufficient_scope", "Additional permission required"],
  ] as const)("shows a reconnect action for %s", (state, label) => {
    const markup = renderToStaticMarkup(<GmailSetupView connection={{state, grantedScopes: []}} onConnect={() => undefined} onReconnect={() => undefined}/>);
    expect(markup).toContain(label);
    expect(markup).toContain("Reconnect");
    expect(markup).not.toContain("Primary sender");
  });

  it("renders email lists as an isolated reusable unit", () => {
    const markup = renderToStaticMarkup(<GmailConfigurationUnit target={{kind: "configurationUnit", scope: {kind: "trigger", triggerName: "messageReceived", bindingName: "start", flowType: "MailFlow"}, instanceId: "senders", unitId: "emailListInput", label: "Allowed senders", required: false, bindings: [{port: "emails", jsonPointer: "/emails"}], value: {emails: ["requester@example.com"]}}} onSave={() => undefined}/>);
    expect(markup).toContain("Allowed senders");
    expect(markup).toContain("requester@example.com");
  });

  it("renders units on the shared Studio surface with a primary Save", () => {
    const markup = renderToStaticMarkup(<GmailConfigurationUnit target={{kind: "configurationUnit", scope: {kind: "trigger", triggerName: "messageReceived", bindingName: "start", flowType: "MailFlow"}, instanceId: "query", unitId: "searchQueryInput", label: "Inbox query", required: true, bindings: [{port: "query", jsonPointer: "/query"}], value: {}}} onSave={() => undefined}/>);
    expect(markup).toContain('class="studio-surface"');
    expect(markup).toContain('placeholder="label:inbox"');
    expect(markup).toContain('class="studio-button studio-button-primary" type="button" disabled="">Save');
    expect(markup).not.toContain("<style");
  });
});
