// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";

import { projectGeminiModelList } from "../src/provider.js";

describe("projectGeminiModelList", () => {
  it("strips the models/ prefix and hides non-text models", () => {
    const listing = projectGeminiModelList({object: "list", data: [
      {id: "models/gemini-3.8-flash", object: "model", owned_by: "google"},
      {id: "models/gemini-3.5-flash-lite", object: "model", owned_by: "google"},
      {id: "models/gemini-embedding-002", object: "model", owned_by: "google"},
      {id: "models/imagen-5.0-generate", object: "model", owned_by: "google"},
      {id: "models/gemini-3.8-flash", object: "model", owned_by: "google"},
    ]});
    expect(listing.models.map((model) => [model.id, model.isHiddenByDefault])).toEqual([
      ["gemini-3.8-flash", false], ["gemini-3.5-flash-lite", false], ["gemini-embedding-002", true], ["imagen-5.0-generate", true],
    ]);
  });

  it("returns no models for an unexpected shape", () => {
    expect(projectGeminiModelList({models: []}).models).toEqual([]);
  });
});
