// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { isReceivingAddress } from "../src/address.js";
import { TwilioSetupView } from "../src/setup.js";
import { TwilioConfigurationUnit } from "../src/units.js";

const sentinelToken = "SENTINEL-TWILIO-AUTH-TOKEN";
const scope = {kind: "trigger" as const, triggerName: "messageReceived", bindingName: "inbound-messages", flowType: "TwilioInboundMessage"};

function renderUnit(value: Record<string, unknown>, required = false): string {
  return renderToStaticMarkup(<TwilioConfigurationUnit
    target={{kind: "configurationUnit", scope, instanceId: "receivingNumber", unitId: "receivingAddressInput", label: "Receiving number",
      description: "Enter the Twilio number whose inbound messages start this Flow; leave it blank to receive every inbound message.", required,
      bindings: [{port: "to", jsonPointer: "/to"}], value}}
    onSave={() => undefined}/>);
}

describe("Twilio Messaging Studio bundle", () => {
  it("renders the receiving number with its description, Console link, blank meaning, and saved value", () => {
    const markup = renderUnit({to: "+14155550100"});
    expect(markup).toContain("Enter the Twilio number whose inbound messages start this Flow");
    expect(markup).toContain("https://console.twilio.com/us1/develop/phone-numbers/manage/incoming");
    expect(markup).toContain("Phone Numbers &gt; Manage &gt; Active numbers");
    expect(markup).toContain("whatsapp:");
    expect(markup).toContain("Not secret. Blank receives every inbound message of the account.");
    expect(markup).toContain('value="+14155550100"');
    expect(markup).not.toContain(sentinelToken);
  });

  it("flags an invalid saved value and a blank required binding", () => {
    expect(renderUnit({to: "4155550100"})).toContain("Enter an E.164 number");
    expect(renderUnit({}, true)).toContain("This binding requires a receiving number.");
    expect(renderUnit({})).not.toContain("This binding requires");
  });

  it("accepts the addresses the Go configuration accepts", () => {
    expect(isReceivingAddress("+14155550100")).toBe(true);
    expect(isReceivingAddress("whatsapp:+14155550100")).toBe(true);
    expect(isReceivingAddress("4155550100")).toBe(false);
    expect(isReceivingAddress(" +14155550100")).toBe(false);
    expect(isReceivingAddress("MG0123456789abcdef0123456789abcdef")).toBe(false);
  });

  it("keeps credentials in the host form and rejects unknown units", () => {
    const setup = renderToStaticMarkup(<TwilioSetupView connection={{state: "not_configured", grantedScopes: [], authMethodIds: ["auth-token"]}}/>);
    expect(setup).toContain("Enter the Account SID and an Auth Token or API key in the form above");
    expect(setup).not.toContain(sentinelToken);
    const unknown = renderToStaticMarkup(<TwilioConfigurationUnit
      target={{kind: "configurationUnit", scope, instanceId: "x", unitId: "textInput", label: "X", required: false, bindings: [], value: {}}}
      onSave={() => undefined}/>);
    expect(unknown).toContain("Unsupported Twilio configuration unit: textInput");
  });
});
