// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { CalendlySetupView } from "../src/setup.js";
import { CalendlyConfigurationUnit } from "../src/units.js";

const sentinelToken = "SENTINEL-CALENDLY-ACCESS-TOKEN";

describe("Calendly setup", () => {
  it("offers OAuth consent only for the OAuth method and keeps tokens in the host form", () => {
    const tokenSetup = renderToStaticMarkup(<CalendlySetupView connection={{state: "not_configured", grantedScopes: [], authMethodIds: ["personal-access-token"]}} onConnect={() => undefined} onReconnect={() => undefined}/>);
    expect(tokenSetup).toContain("Paste a personal access token in the form above");
    expect(tokenSetup).not.toContain("Connect Calendly");
    const oauthSetup = renderToStaticMarkup(<CalendlySetupView connection={{state: "not_configured", grantedScopes: [], authMethodIds: ["calendly-oauth"]}} onConnect={() => undefined} onReconnect={() => undefined}/>);
    expect(oauthSetup).toContain("Connect Calendly");
    const expired = renderToStaticMarkup(<CalendlySetupView connection={{state: "revoked", grantedScopes: [], authMethodIds: ["personal-access-token"]}} onConnect={() => undefined} onReconnect={() => undefined}/>);
    expect(expired).toContain("Generate a new personal access token");
    expect(expired).not.toContain("Reconnect Calendly");
  });

  it("renders the event type picker for a Trigger binding with its description, blank meaning, and fallback", () => {
    const scope = {kind: "trigger" as const, triggerName: "inviteeEventReceived", bindingName: "invitee-created", flowType: "CalendlyInviteeRecorder"};
    const description = "Select the Calendly event type whose new bookings start this Flow; leave it empty to record every event type.";
    const markup = renderToStaticMarkup(<CalendlyConfigurationUnit
      target={{kind: "configurationUnit", scope, instanceId: "eventType", unitId: "eventTypePicker", label: "Event type", description, required: false,
        bindings: [{port: "eventTypeUri", jsonPointer: "/eventTypeUri"}], value: {eventTypeUri: "https://api.calendly.com/event_types/TYPE0001"}}}
      eventTypes={[
        {uri: "https://api.calendly.com/event_types/TYPE0001", name: "30 Minute Meeting", durationMinutes: 30, isActive: true},
        {uri: "https://api.calendly.com/event_types/TYPE0002", name: "Retired", isActive: false},
      ]}
      onLoadEventTypes={() => undefined} onSave={() => undefined}/>);
    expect(markup).toContain(description);
    expect(markup).toContain("Load event types");
    expect(markup).toContain("Every event type");
    expect(markup).toContain("30 Minute Meeting · 30 min");
    expect(markup).toContain("Retired (inactive)");
    expect(markup).toContain("Event type URI fallback");
    expect(markup).toContain("https://api.calendly.com/event_types/TYPE0001");
    expect(markup).not.toContain(sentinelToken);
  });

  it("rejects an unknown unit", () => {
    const scope = {kind: "operation" as const, operationId: "createSchedulingLink", flowType: "Flow", stepType: "CreateLink"};
    const markup = renderToStaticMarkup(<CalendlyConfigurationUnit
      target={{kind: "configurationUnit", scope, instanceId: "x", unitId: "textInput", label: "X", required: false, bindings: [], value: {}}}
      eventTypes={[]} onLoadEventTypes={() => undefined} onSave={() => undefined}/>);
    expect(markup).toContain("Unsupported Calendly configuration unit: textInput");
  });
});
