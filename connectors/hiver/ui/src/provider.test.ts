// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import { parseHiverInboxPage, parseInboxID } from "./provider.js";

describe("Hiver provider responses", () => {
  it("keeps inboxes with valid string or numeric IDs and returns Hiver's next_page token", () => {
    expect(parseHiverInboxPage({data: {
      results: [
        {id: "105902", display_name: "Support", email: "support@acme.example.com", channel_type: "email", is_authorised: true},
        {id: 202, display_name: "Billing"},
        {id: "../inboxes", display_name: "Path"},
        {id: 1.5, display_name: "Fraction"},
        {display_name: "No ID"},
        {id: "303"},
      ],
      pagination: {next_page: "eyJvZmZzZXQiOjEwMH0="},
    }})).toEqual({
      inboxes: [
        {id: "105902", name: "Support", email: "support@acme.example.com"},
        {id: "202", name: "Billing", email: undefined},
        {id: "303", name: "303", email: undefined},
      ],
      nextPageToken: "eyJvZmZzZXQiOjEwMH0=",
    });
    expect(parseHiverInboxPage({data: {results: [], pagination: {next_page: null}}})).toEqual({inboxes: [], nextPageToken: ""});
  });

  it("reports an error object in connector words, never Hiver's text", () => {
    const hiverError = {errors: [{message: "secret detail"}]};
    expect(() => parseHiverInboxPage(hiverError)).toThrow("Hiver returned an error instead of the inbox list");
    expect(() => parseHiverInboxPage({Message: "secret detail"})).not.toThrow("secret detail");
  });

  it("accepts only Hiver IDs as a manual fallback", () => {
    expect(parseInboxID("105902")).toBe("105902");
    expect(parseInboxID("")).toBeUndefined();
    expect(parseInboxID("105 902")).toBeUndefined();
    expect(parseInboxID("../inboxes")).toBeUndefined();
    expect(parseInboxID("x".repeat(65))).toBeUndefined();
  });
});
