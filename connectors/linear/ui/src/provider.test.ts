// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import { isLinearTeamUUID, parseLinearTeamList } from "./provider.js";

describe("Linear provider responses", () => {
  it("keeps teams with a UUID, key, and name from the GraphQL answer", () => {
    expect(parseLinearTeamList({data: {teams: {nodes: [
      {id: "2F6B7C1E-3D4A-4B5C-8D6E-7F8091A2B3C4", key: "ENG", name: "Engineering"},
      {id: "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d", key: "OPS", name: "Operations"},
      {id: "not-a-uuid", key: "BAD", name: "Bad"},
      {id: "1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d", name: "No key"},
    ], pageInfo: {hasNextPage: true}}}})).toEqual({
      teams: [
        {id: "2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4", key: "ENG", name: "Engineering"},
        {id: "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d", key: "OPS", name: "Operations"},
      ],
      isTruncated: true,
    });
    expect(parseLinearTeamList({data: {teams: {nodes: [], pageInfo: {hasNextPage: false}}}})).toEqual({teams: [], isTruncated: false});
  });

  it("reports a GraphQL error in connector words, never Linear's text", () => {
    const answer = {errors: [{message: "secret detail", extensions: {code: "AUTHENTICATION_ERROR"}}]};
    expect(() => parseLinearTeamList(answer)).toThrow("Linear returned an error instead of the team list");
    expect(() => parseLinearTeamList(answer)).not.toThrow("secret detail");
    expect(() => parseLinearTeamList({data: null})).toThrow("Linear did not return a team list");
  });

  it("accepts only team UUIDs as a manual fallback", () => {
    expect(isLinearTeamUUID("2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4")).toBe(true);
    expect(isLinearTeamUUID("ENG")).toBe(false);
    expect(isLinearTeamUUID("2f6b7c1e3d4a4b5c8d6e7f8091a2b3c4")).toBe(false);
  });
});
