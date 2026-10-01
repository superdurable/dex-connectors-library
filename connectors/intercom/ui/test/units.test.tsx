// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { IntercomSetupView } from "../src/setup.js";
import { selectedSupportedTopics } from "../src/topics.js";
import { IntercomConfigurationUnit } from "../src/units.js";

const operationScope = {kind: "operation" as const, operationId: "replyToConversation", flowType: "IntercomAnswerDuplicateConversation", stepType: "ReplyToDuplicate"};
const triggerScope = {kind: "trigger" as const, triggerName: "conversationEvent", bindingName: "inbound-conversation", flowType: "IntercomAnswerDuplicateConversation"};

function adminTarget(value: Record<string, unknown>): ConnectorStudioConfigurationUnitTarget {
  return {
    kind: "configurationUnit", scope: operationScope, instanceId: "replyingAdmin", unitId: "adminPicker", label: "Replying admin",
    description: "Select the Intercom teammate whose name the customer sees on the duplicate reply.", required: true,
    bindings: [{port: "adminId", jsonPointer: "/adminId"}], value,
  };
}

function render(target: ConnectorStudioConfigurationUnitTarget, admins = [{id: "5017691", name: "Ada", email: "ada@example.com", isAway: false}]) {
  return renderToStaticMarkup(<IntercomConfigurationUnit admins={admins} onLoadAdmins={() => undefined} onSave={() => undefined} target={target}/>);
}

describe("Intercom configuration units", () => {
  it("renders the admin picker with its description, loaded admins, saved ID, and manual fallback guidance", () => {
    const markup = render(adminTarget({adminId: "5017691"}));
    expect(markup).toContain("Replying admin");
    expect(markup).toContain("Select the Intercom teammate whose name the customer sees on the duplicate reply.");
    expect(markup).toContain("Load admins");
    expect(markup).toContain("Ada · ada@example.com · 5017691");
    expect(markup).toContain("Admin ID fallback");
    expect(markup).toContain("…/admins/5017691");
    expect(markup).toContain('value="5017691"');
    expect(markup).not.toContain("disabled");
  });

  it("blocks saving a required admin picker without an admin and rejects a non-numeric ID", () => {
    expect(render(adminTarget({}), [])).toContain('disabled=""');
    const invalid = render(adminTarget({adminId: "ada"}), []);
    expect(invalid).toContain("An Intercom admin ID is a string of digits.");
    expect(invalid).toContain('disabled=""');
  });

  it("renders every supported conversation topic with the saved selection", () => {
    const markup = render({
      kind: "configurationUnit", scope: triggerScope, instanceId: "topics", unitId: "conversationTopicPicker", label: "Topics that start the Flow",
      description: "Select conversation.user.created.", required: false,
      bindings: [{port: "topics", jsonPointer: "/topics"}], value: {topics: ["conversation.user.created", "unknown.topic"]},
    }, []);
    expect(markup).toContain("Topics that start the Flow");
    expect(markup.match(/type="checkbox"/g)).toHaveLength(14);
    expect(markup).toContain("conversation.user.created");
    expect(markup).toContain("conversation.rating.added");
    expect(markup.match(/checked=""/g)).toHaveLength(1);
    expect(markup).toContain("Select none to accept every topic listed.");
    expect(markup).toContain("Configure &gt; Webhooks");
  });

  it("keeps only supported topics in their supported order", () => {
    expect(selectedSupportedTopics(["conversation.admin.closed", "ping", "conversation.user.created", 3])).toEqual([
      "conversation.user.created", "conversation.admin.closed",
    ]);
    expect(selectedSupportedTopics(undefined)).toEqual([]);
  });

  it("reports an unknown unit instead of rendering a guess", () => {
    expect(render({...adminTarget({}), unitId: "teamPicker"})).toContain("Unsupported Intercom configuration unit: teamPicker");
  });
});

describe("Intercom connection surface", () => {
  it("points credentials to the host form and never renders a credential field", () => {
    const markup = renderToStaticMarkup(<IntercomSetupView connection={{state: "connected", grantedScopes: []}}/>);
    expect(markup).toContain("access token");
    expect(markup).toContain("Connected.");
    expect(markup).not.toContain("<input");
    expect(renderToStaticMarkup(<IntercomSetupView connection={{state: "not_configured", grantedScopes: []}}/>)).toContain("Not connected yet.");
  });
});
