import { describe, expect, it } from "vitest";
import { parseSlackChannelPage, parseSlackUserPage } from "./provider.js";

describe("Slack provider responses", () => {
  it("keeps joined active channels and pagination in connector-owned code", () => {
    expect(parseSlackChannelPage({ok: true, channels: [
      {id: "C1", name: "approvals", is_member: true, is_private: true},
      {id: "C2", name: "other", is_member: false},
    ], response_metadata: {next_cursor: "next"}})).toEqual({
      channels: [{id: "C1", name: "approvals", isPrivate: true}], nextCursor: "next",
    });
  });

  it("keeps human members and chooses their connector display name", () => {
    expect(parseSlackUserPage({ok: true, members: [
      {id: "U1", name: "ada", profile: {display_name: "Ada", image_48: "https://example.com/ada.png"}},
      {id: "B1", is_bot: true, profile: {display_name: "Build bot"}},
    ]})).toEqual({users: [{id: "U1", displayName: "Ada", imageUrl: "https://example.com/ada.png"}], nextCursor: ""});
  });
});
