// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import { channelIDPattern, chatIDPattern, parseChannelPage, parseChatPage, parseJoinedTeams, skipTokenOf, teamIDPattern } from "./provider.js";

const teamID = "fbe2bf47-16c8-47cf-b4a5-4b9b187c508b";
const channelID = "19:4a95f7d8db4c4e7fae857bcebe0623e6@thread.tacv2";

describe("Microsoft Graph responses", () => {
  it("keeps joined teams with a GUID and lists archived teams last", () => {
    expect(parseJoinedTeams({value: [
      {id: "11111111-2222-4333-8444-555555555555", displayName: "Archive", isArchived: true},
      {id: teamID, displayName: "Operations", isArchived: false},
      {id: "not-a-guid", displayName: "Broken"},
      {id: "22222222-3333-4444-8555-666666666666"},
    ]})).toEqual([
      {id: "22222222-3333-4444-8555-666666666666", name: "22222222-3333-4444-8555-666666666666"},
      {id: teamID, name: "Operations"},
      {id: "11111111-2222-4333-8444-555555555555", name: "Archive"},
    ]);
  });

  it("names only Graph's error code when the host returns no list", () => {
    expect(() => parseJoinedTeams({error: {code: "Forbidden", message: "secret detail"}})).toThrow("Microsoft Teams returned no team list (Forbidden)");
    expect(() => parseChannelPage({error: {code: "has spaces in it"}})).toThrow(/^Microsoft Teams returned no channel list$/);
  });

  it("pages channels by the $skiptoken of a graph.microsoft.com next link", () => {
    expect(parseChannelPage({value: [
      {id: channelID, displayName: "Incidents", membershipType: "standard"},
      {id: "19:abc@thread.tacv2", displayName: "Leads", membershipType: "private"},
      {id: "General", displayName: "Broken"},
    ], "@odata.nextLink": `https://graph.microsoft.com/v1.0/teams/${teamID}/channels?$select=id&$skiptoken=X%3D1`})).toEqual({
      items: [
        {id: channelID, name: "Incidents", membershipType: "standard"},
        {id: "19:abc@thread.tacv2", name: "Leads", membershipType: "private"},
      ],
      nextSkipToken: "X=1",
    });
    expect(skipTokenOf("https://attacker.example/v1.0/chats?$skiptoken=1")).toBe("");
    expect(skipTokenOf("http://graph.microsoft.com/v1.0/chats?$skiptoken=1")).toBe("");
    expect(skipTokenOf("not a url")).toBe("");
    expect(skipTokenOf(undefined)).toBe("");
  });

  it("labels chats by topic, then by member names", () => {
    expect(parseChatPage({value: [
      {id: "19:meeting_MjdhNjM4YzUtYzExZi00@thread.v2", topic: "On-call managers", chatType: "group", members: [{displayName: "Ada"}]},
      {id: "19:8ea0e38b_5f1c2d3e@unq.gbl.spaces", topic: null, chatType: "oneOnOne", members: [{displayName: "Ada"}, {displayName: "Grace"}]},
      {id: "19:no-members@thread.v2", chatType: "meeting"},
      {id: "bad", topic: "Broken"},
    ], "@odata.nextLink": "https://graph.microsoft.com/v1.0/chats?$expand=members&$skiptoken=abc"})).toEqual({
      items: [
        {id: "19:meeting_MjdhNjM4YzUtYzExZi00@thread.v2", label: "On-call managers", chatType: "group"},
        {id: "19:8ea0e38b_5f1c2d3e@unq.gbl.spaces", label: "Ada, Grace", chatType: "oneOnOne"},
        {id: "19:no-members@thread.v2", label: "19:no-members@thread.v2", chatType: "meeting"},
      ],
      nextSkipToken: "abc",
    });
  });

  it("validates IDs with the Go connector's rules", () => {
    expect(teamIDPattern.test(teamID)).toBe(true);
    expect(teamIDPattern.test("Operations")).toBe(false);
    expect(channelIDPattern.test(channelID)).toBe(true);
    expect(channelIDPattern.test("19:abc@thread.tacv2/messages")).toBe(false);
    expect(chatIDPattern.test("19:8ea0e38b_5f1c2d3e@unq.gbl.spaces")).toBe(true);
    expect(chatIDPattern.test("19:abc@thread.v2/../me")).toBe(false);
  });
});
