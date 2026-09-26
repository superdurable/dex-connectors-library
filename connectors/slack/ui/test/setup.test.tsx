import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { SlackSetupView } from "../src/setup.js";
import { SlackConfigurationUnit } from "../src/units.js";

describe("Slack setup", () => {
  it("keeps connection authorization separate from Flow configuration", () => {
    const markup = renderToStaticMarkup(<SlackSetupView connection={{state: "connected", grantedScopes: []}} onConnect={() => undefined} onReconnect={() => undefined}/>);
    expect(markup).toContain("Connection ready");
    expect(markup).not.toContain("Approval channel");
  });

  it("renders channel and member pickers as independent units", () => {
    const scope = {kind: "operation" as const, operationId: "postChannelMessage", flowType: "ApprovalFlow", stepType: "RequestApproval"};
    const markup = renderToStaticMarkup(<>
      <SlackConfigurationUnit target={{kind: "configurationUnit", scope, instanceId: "channel", unitId: "channelPicker", label: "Approval channel", required: true, bindings: [{port: "channelId", jsonPointer: "/channelId"}], value: {channelId: "C123"}}} channels={[{id: "C123", name: "approvals", isPrivate: true}]} users={[]} onLoadChannels={() => undefined} onLoadUsers={() => undefined} onSave={() => undefined}/>
      <SlackConfigurationUnit target={{kind: "configurationUnit", scope, instanceId: "members", unitId: "memberPicker", label: "Approvers", required: true, bindings: [{port: "memberIds", jsonPointer: "/memberIds"}], value: {memberIds: ["U123"]}}} channels={[]} users={[{id: "U123", displayName: "Ada"}]} onLoadChannels={() => undefined} onLoadUsers={() => undefined} onSave={() => undefined}/>
    </>);
    expect(markup).toContain("#approvals (private) · C123");
    expect(markup).toContain("Ada");
    expect(markup).toContain("U123");
    expect(markup).not.toContain("bot-token");
  });
});
