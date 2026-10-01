// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import { isAsanaGID, offsetParameters, parseResourcePage } from "./provider.js";

describe("Asana provider responses", () => {
  it("keeps entries with a gid and returns the next offset", () => {
    expect(parseResourcePage({
      data: [
        {gid: "1100000000000001", resource_type: "workspace", name: "Operations"},
        {gid: "1100000000000002", resource_type: "workspace"},
        {gid: "ops", name: "Not a gid"},
        {name: "Missing gid"},
        "not an object",
      ],
      next_page: {offset: "eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.page2", path: "/workspaces?offset=x", uri: "https://app.asana.com/api/1.0/workspaces?offset=x"},
    })).toEqual({
      resources: [{id: "1100000000000001", name: "Operations"}, {id: "1100000000000002", name: "1100000000000002"}],
      nextOffset: "eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.page2",
    });
  });

  it("ends the list on a null, missing, or unusable next_page", () => {
    expect(parseResourcePage({data: [], next_page: null})).toEqual({resources: [], nextOffset: ""});
    expect(parseResourcePage({})).toEqual({resources: [], nextOffset: ""});
    expect(parseResourcePage({data: [], next_page: {offset: "x".repeat(4097)}}).nextOffset).toBe("");
  });

  it("sends an offset only after the first page", () => {
    expect(offsetParameters("", {workspace: "1100000000000001"})).toEqual({workspace: "1100000000000001"});
    expect(offsetParameters("page-2", {workspace: "1100000000000001"})).toEqual({workspace: "1100000000000001", offset: "page-2"});
    expect(offsetParameters("")).toEqual({});
  });

  it("accepts only decimal gids", () => {
    expect(isAsanaGID(" 1204567890123456 ")).toBe(true);
    expect(isAsanaGID("https://app.asana.com/0/1/2")).toBe(false);
    expect(isAsanaGID("")).toBe(false);
  });
});
