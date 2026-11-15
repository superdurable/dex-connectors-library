// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { channelError, savedChannel } from "../src/channel.js";
import { ReamazeSetupView } from "../src/setup.js";
import { ReamazeConfigurationUnit } from "../src/units.js";

const triggerScope = {kind: "trigger" as const, triggerName: "conversationCreated", bindingName: "new-conversations", flowType: "ReamazeNewConversation"};

function channelTarget(value: Record<string, unknown>, required = false): ConnectorStudioConfigurationUnitTarget {
  return {
    kind: "configurationUnit", scope: triggerScope, instanceId: "watchedChannel", unitId: "channelSlugEntry", label: "Watched channel",
    description: "Enter the slug of the channel whose new conversations start this Flow.", required,
    bindings: [{port: "channel", jsonPointer: "/channel"}], value,
  };
}

function render(target: ConnectorStudioConfigurationUnitTarget) {
  return renderToStaticMarkup(<ReamazeConfigurationUnit onSave={() => undefined} target={target}/>);
}

describe("Re:amaze configuration units", () => {
  it("renders the guided channel slug entry with the saved slug", () => {
    const markup = render(channelTarget({channel: "support"}));
    expect(markup).toContain("Watched channel");
    expect(markup).toContain("Enter the slug of the channel whose new conversations start this Flow.");
    expect(markup).toContain("Channel slug");
    expect(markup).toContain('value="support"');
    expect(markup).toContain("GET https://{brand}.reamaze.io/api/v1/channels");
    expect(markup).toContain("Settings &gt; Channels");
    expect(markup).toContain("Leave it blank to receive conversations from every channel of the brand.");
    expect(markup).toContain("The slug is not secret");
    expect(markup).not.toContain("disabled");
    expect(markup.match(/<input/g)).toHaveLength(1);
    expect(markup).not.toContain('type="password"');
  });

  it("allows a blank optional entry and blocks a blank required one", () => {
    expect(render(channelTarget({}))).not.toContain("disabled");
    const required = render(channelTarget({}, true));
    expect(required).toContain('disabled=""');
    expect(required).toContain("Enter a channel slug.");
  });

  it("rejects URLs and characters a Re:amaze slug cannot hold", () => {
    expect(channelError("support", false)).toBeUndefined();
    expect(channelError("", false)).toBeUndefined();
    expect(channelError("https://acme.reamaze.io/admin", false)).toContain("not a URL");
    expect(channelError("acme.reamaze.io", false)).toContain("not a URL");
    expect(channelError("sales team", false)).toContain("letters, digits, hyphens, and underscores");
    expect(render(channelTarget({channel: "sales team"}))).toContain('aria-invalid="true"');
    expect(savedChannel(3)).toBe("");
    expect(savedChannel("billing")).toBe("billing");
  });

  it("reports an unknown unit instead of rendering a guess", () => {
    expect(render({...channelTarget({}), unitId: "channelPicker"})).toContain("Unsupported Re:amaze configuration unit: channelPicker");
  });
});

describe("Re:amaze connection surface", () => {
  it("points credentials to the host form and never renders a credential field", () => {
    const markup = renderToStaticMarkup(<ReamazeSetupView connection={{state: "connected", grantedScopes: []}}/>);
    expect(markup).toContain("API token");
    expect(markup).toContain("Connected.");
    expect(markup).not.toContain("<input");
    expect(renderToStaticMarkup(<ReamazeSetupView connection={{state: "not_configured", grantedScopes: []}}/>)).toContain("Not connected yet.");
  });
});
