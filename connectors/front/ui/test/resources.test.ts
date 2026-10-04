// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import { frontResourcePickers, isFrontResourceID, loadFrontResources, parseFrontResourcePage } from "../src/resources.js";

const {inboxPicker, teammatePicker, tagPicker} = frontResourcePickers;

describe("Front resource lists", () => {
  it("keeps inboxes with a valid ID and marks private ones", () => {
    const page = parseFrontResourcePage(inboxPicker, {_results: [
      {id: "inb_41w25", name: "Support", is_private: false},
      {id: "inb_9", name: "Leela's inbox", is_private: true},
      {id: "Support", name: "not an ID"},
      "garbage",
    ]});
    expect(page.resources).toEqual([
      {id: "inb_41w25", label: "Support", detail: ""},
      {id: "inb_9", label: "Leela's inbox", detail: "private"},
    ]);
    expect(page.nextPageToken).toBe("");
  });

  it("labels teammates by name, then username, then email, and flags blocked ones", () => {
    const page = parseFrontResourcePage(teammatePicker, {_results: [
      {id: "tea_2thf", first_name: "Leela", last_name: "Turanga", email: "leela@planet-express.example.com", is_blocked: false},
      {id: "tea_3", username: "bender", email: "bender@planet-express.example.com", is_blocked: true},
      {id: "tea_4", first_name: "", last_name: "", email: "fry@planet-express.example.com"},
    ]});
    expect(page.resources.map((resource) => [resource.label, resource.detail])).toEqual([
      ["Leela Turanga", "leela@planet-express.example.com"],
      ["bender", "bender@planet-express.example.com · blocked"],
      ["fry@planet-express.example.com", "fry@planet-express.example.com"],
    ]);
  });

  it("reads the next tag page token only from Front's API hosts", () => {
    const next = (link: unknown) => parseFrontResourcePage(tagPicker, {_results: [], _pagination: {next: link}}).nextPageToken;
    expect(next("https://acme.api.frontapp.com/tags?limit=100&page_token=2d018a5809eb")).toBe("2d018a5809eb");
    expect(next("https://api2.frontapp.com/tags?page_token=abc")).toBe("abc");
    expect(next("https://attacker.example.com/tags?page_token=abc")).toBe("");
    expect(next("https://acme.api.frontapp.com.attacker.example/tags?page_token=abc")).toBe("");
    expect(next("http://api2.frontapp.com/tags?page_token=abc")).toBe("");
    expect(next("https://api2.frontapp.com/contacts?page_token=abc")).toBe("");
    expect(next(null)).toBe("");
  });

  it("rejects a Front error object instead of an empty list", () => {
    expect(() => parseFrontResourcePage(tagPicker, {_error: {status: 401, title: "Unauthorized"}})).toThrow("Front did not return a tag list");
  });

  it("follows tag page tokens through the broker and sorts by label", async () => {
    const calls: Array<{commandId: string; capability: string; parameters?: Record<string, string>}> = [];
    const pages: Record<string, Record<string, unknown>> = {
      "": {_results: [{id: "tag_b", name: "Refund"}], _pagination: {next: "https://acme.api.frontapp.com/tags?page_token=second"}},
      second: {_results: [{id: "tag_a", name: "Billing"}, {id: "tag_b", name: "Refund"}], _pagination: {next: null}},
    };
    const listed = await loadFrontResources({
      executeProviderCommand: async (commandId, capability, parameters) => {
        calls.push({commandId, capability, parameters});
        return pages[parameters?.pageToken ?? ""];
      },
    }, tagPicker);
    expect(listed.resources.map((resource) => resource.id)).toEqual(["tag_a", "tag_b"]);
    expect(listed.isTruncated).toBe(false);
    expect(calls).toEqual([
      {commandId: "listTags", capability: "front.tags-list", parameters: {}},
      {commandId: "listTags", capability: "front.tags-list", parameters: {pageToken: "second"}},
    ]);
  });

  it("lists inboxes with one command and never pages them", async () => {
    let callCount = 0;
    const listed = await loadFrontResources({
      executeProviderCommand: async () => {
        callCount++;
        return {_results: [{id: "inb_1", name: "Support"}], _pagination: {next: "https://api2.frontapp.com/tags?page_token=x"}};
      },
    }, inboxPicker);
    expect(listed.resources).toHaveLength(1);
    expect(callCount).toBe(1);
  });

  it("validates manually entered IDs by resource prefix", () => {
    expect(isFrontResourceID(tagPicker, "tag_13o8r1")).toBe(true);
    expect(isFrontResourceID(tagPicker, "billing")).toBe(false);
    expect(isFrontResourceID(teammatePicker, "tag_13o8r1")).toBe(false);
    expect(isFrontResourceID(inboxPicker, "inb_41w25")).toBe(true);
  });
});
