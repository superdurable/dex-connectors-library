import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { SlackSetupView } from "../src/setup.js";

describe("Slack setup", () => {
  it("shows channel and member names with stable IDs", () => {
    const markup = renderToStaticMarkup(<SlackSetupView connection={{state: "connected", grantedScopes: []}} channels={[{id: "C123", name: "approvals", isPrivate: true}]} users={[{id: "U123", displayName: "Ada"}]} selections={{channelId: "C123", threadReplyMatcher: {messageContains: "approve", posterUserIds: ["U123"]}}} onConnect={() => undefined} onReconnect={() => undefined} onLoadChannels={() => undefined} onLoadUsers={() => undefined} onSelectionsChange={() => undefined} onSave={() => undefined}/>);
    expect(markup).toContain("#approvals (private) · C123");
    expect(markup).toContain("Ada");
    expect(markup).toContain("U123");
    expect(markup).toContain("Start when the top-level message contains");
    expect(markup).not.toContain("bot-token");
  });
});
