// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";

import { projectMistralModelList } from "../src/provider.js";

const chatCapabilities = {completion_chat: true, completion_fim: false, function_calling: true, fine_tuning: false, vision: true, classification: false};

describe("projectMistralModelList", () => {
  it("shows chat models, badges deprecation and capabilities, and hides non-chat and archived models", () => {
    const listing = projectMistralModelList({object: "list", data: [
      {id: "mistral-medium-3-5", object: "model", owned_by: "mistralai", type: "base", capabilities: chatCapabilities,
        aliases: ["mistral-medium-3", "mistral-medium-latest"], deprecation: null, deprecation_replacement_model: null},
      {id: "mistral-large-2512", object: "model", type: "base", capabilities: chatCapabilities, aliases: ["mistral-large-latest"]},
      {id: "ministral-14b-2512", object: "model", type: "base", capabilities: {completion_chat: true, function_calling: true, vision: false}},
      {id: "mistral-medium-2508", object: "model", type: "base", capabilities: chatCapabilities,
        deprecation: "2026-08-31T12:00:00Z", deprecation_replacement_model: "mistral-medium-3-5"},
      {id: "mistral-small-2506", object: "model", type: "base", capabilities: {completion_chat: true}, deprecation: "2026-07-31"},
      {id: "mistral-embed-2312", object: "model", type: "base", capabilities: {completion_chat: false, classification: false}},
      {id: "mistral-ocr-2512", object: "model", type: "base", capabilities: {ocr: true}},
      {id: "ft:open-mistral-7b:587a6b29:20240514:7e773925", object: "model", type: "fine-tuned", job: "job-1",
        root: "open-mistral-7b", capabilities: {completion_chat: true}, archived: true},
      {id: "ft:ministral-8b-latest:587a6b29:20260101:1a2b3c4d", object: "model", type: "fine-tuned", job: "job-2",
        root: "ministral-8b-latest", capabilities: {completion_chat: true}, archived: false},
      {id: "card-without-capabilities", object: "model", type: "base"},
      {id: "mistral-large-2512", object: "model", type: "base", capabilities: chatCapabilities},
    ]});
    expect(listing.models.map((model) => [model.id, model.isHiddenByDefault, model.detail, model.badges])).toEqual([
      ["mistral-medium-3-5", false, undefined, ["function calling", "vision"]],
      ["mistral-large-2512", false, undefined, ["function calling", "vision"]],
      ["ministral-14b-2512", false, undefined, ["function calling"]],
      ["mistral-medium-2508", false, "Deprecation date 2026-08-31. Replacement: mistral-medium-3-5.", ["deprecated", "function calling", "vision"]],
      ["mistral-small-2506", false, "Deprecation date 2026-07-31.", ["deprecated"]],
      ["mistral-embed-2312", true, undefined, []],
      ["mistral-ocr-2512", true, undefined, []],
      ["ft:open-mistral-7b:587a6b29:20240514:7e773925", true, undefined, []],
      ["ft:ministral-8b-latest:587a6b29:20260101:1a2b3c4d", false, undefined, []],
      ["card-without-capabilities", true, undefined, []],
    ]);
  });

  it("returns no models for a shape other than the documented data array", () => {
    expect(projectMistralModelList({models: []}).models).toEqual([]);
    expect(projectMistralModelList({data: "not-an-array"}).models).toEqual([]);
  });
});
