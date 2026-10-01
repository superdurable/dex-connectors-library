// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import { parseHelpScoutMailboxPage, parseMailboxID } from "./provider.js";

describe("Help Scout provider responses", () => {
  it("keeps named inboxes and returns the next page number when Help Scout links one", () => {
    expect(parseHelpScoutMailboxPage({
      _embedded: {mailboxes: [
        {id: 1, name: "Help Scout", slug: "83ebc5d87292a795", email: "bearbox@acme.com"},
        {id: 2, name: "Help Pioneer"},
        {id: 0, name: "Zero ID"},
        {id: "3", name: "String ID"},
        {id: 4},
      ]},
      _links: {self: {href: "x"}, next: {href: "https://api.helpscout.net/v2/mailboxes?page=3"}},
      page: {number: 2, size: 50, totalElements: 120, totalPages: 3},
    }, 2)).toEqual({
      mailboxes: [{id: 1, name: "Help Scout", email: "bearbox@acme.com"}, {id: 2, name: "Help Pioneer", email: undefined}],
      nextPage: "3",
    });
    expect(parseHelpScoutMailboxPage({_embedded: {mailboxes: []}, _links: {self: {href: "x"}}, page: {number: 1}}, 1))
      .toEqual({mailboxes: [], nextPage: ""});
  });

  it("reports an error object in connector words, never Help Scout's text", () => {
    const helpScoutError = {logRef: "64bb72b4", message: "secret detail", _embedded: {errors: [{path: "page", message: "secret detail"}]}};
    expect(() => parseHelpScoutMailboxPage(helpScoutError, 1)).toThrow("Help Scout returned an error instead of the inbox list");
    expect(() => parseHelpScoutMailboxPage(helpScoutError, 1)).not.toThrow("secret detail");
  });

  it("accepts only positive whole-number inbox IDs as a manual fallback", () => {
    expect(parseMailboxID("12345")).toBe(12345);
    expect(parseMailboxID("0")).toBeUndefined();
    expect(parseMailboxID("-1")).toBeUndefined();
    expect(parseMailboxID("12.5")).toBeUndefined();
    expect(parseMailboxID("inbox")).toBeUndefined();
    expect(parseMailboxID("99999999999999999")).toBeUndefined();
  });
});
