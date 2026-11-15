// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { MailchimpSetupView } from "../src/setup.js";
import { audienceIdProblem, selectedSupportedStatuses } from "../src/statuses.js";
import { MailchimpConfigurationUnit } from "../src/units.js";

const triggerScope = {kind: "trigger" as const, triggerName: "memberChanged", bindingName: "audience-member-changes", flowType: "MailchimpMemberChange"};

function target(unitId: string, value: Record<string, unknown>, required = false): ConnectorStudioConfigurationUnitTarget {
  return {
    kind: "configurationUnit", scope: triggerScope, instanceId: unitId, unitId, label: "Watched audience",
    description: "Enter the audience whose contact changes start this Flow.", required,
    bindings: unitId === "audienceIdEntry" ? [{port: "audienceId", jsonPointer: "/audienceId"}] : [{port: "statuses", jsonPointer: "/statuses"}],
    value,
  };
}

function render(unit: ConnectorStudioConfigurationUnitTarget) {
  return renderToStaticMarkup(<MailchimpConfigurationUnit onSave={() => undefined} target={unit}/>);
}

describe("Mailchimp audience ID entry", () => {
  it("renders the saved ID with the binding description, the provider links, and the menu path", () => {
    const markup = render(target("audienceIdEntry", {audienceId: "57afe96172"}, true));
    expect(markup).toContain("Watched audience");
    expect(markup).toContain("Enter the audience whose contact changes start this Flow.");
    expect(markup).toContain('value="57afe96172"');
    expect(markup).toContain('placeholder="57afe96172"');
    expect(markup).toContain('href="https://login.mailchimp.com/"');
    expect(markup).toContain('href="https://mailchimp.com/help/find-audience-id/"');
    expect(markup).toContain("More options &gt; Audience settings");
    expect(markup).toContain("It is not secret.");
    expect(markup.match(/<input/g)).toHaveLength(1);
    expect(markup).not.toContain("disabled");
  });

  it("blocks saving a required unit without an ID", () => {
    expect(render(target("audienceIdEntry", {}, true))).toContain('disabled=""');
  });

  it("explains an ID that is not letters and digits", () => {
    expect(audienceIdProblem("")).toBe("Enter the audience ID.");
    expect(audienceIdProblem("Spring audience")).toContain("1 to 64 letters and digits");
    expect(audienceIdProblem("57afe96172")).toBeUndefined();
    expect(render(target("audienceIdEntry", {audienceId: "57afe/96172"}, true))).toContain("1 to 64 letters and digits");
  });
});

describe("Mailchimp contact status picker", () => {
  it("renders every status with its meaning and the saved selection", () => {
    const markup = render(target("memberStatusPicker", {statuses: ["unsubscribed", "deleted"]}));
    expect(markup.match(/type="checkbox"/g)).toHaveLength(6);
    expect(markup).toContain("subscribed");
    expect(markup).toContain("Cleaned: removed from sending after hard bounces");
    expect(markup.match(/checked=""/g)).toHaveLength(1);
    expect(markup).toContain("Select none to deliver changed contacts in every status.");
    expect(markup).not.toContain("disabled");
  });

  it("blocks saving a required picker without a status", () => {
    expect(render(target("memberStatusPicker", {}, true))).toContain('disabled=""');
  });

  it("keeps only supported statuses in their supported order", () => {
    expect(selectedSupportedStatuses(["pending", "deleted", "subscribed", 3])).toEqual(["subscribed", "pending"]);
    expect(selectedSupportedStatuses(undefined)).toEqual([]);
  });

  it("reports an unknown unit instead of rendering a guess", () => {
    expect(render({...target("memberStatusPicker", {}), unitId: "audiencePicker"})).toContain("Unsupported Mailchimp configuration unit: audiencePicker");
  });
});

describe("Mailchimp connection surface", () => {
  it("points the API key to the host form and never renders a credential field", () => {
    const markup = renderToStaticMarkup(<MailchimpSetupView connection={{state: "connected", grantedScopes: []}}/>);
    expect(markup).toContain("Mailchimp API key");
    expect(markup).toContain("-us6");
    expect(markup).toContain("Connected.");
    expect(markup).not.toContain("<input");
    expect(renderToStaticMarkup(<MailchimpSetupView connection={{state: "not_configured", grantedScopes: []}}/>)).toContain("Not connected yet.");
  });
});
