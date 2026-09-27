// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";

import type { ConnectorStudioClient } from "@superdurable/dex-connectors-react";

import { deepSeekModelsListCapability, loadDeepSeekModels, projectDeepSeekModelList } from "../src/provider.js";

describe("projectDeepSeekModelList", () => {
  it("labels models with their display name and token limits in DeepSeek's order", () => {
    const listing = projectDeepSeekModelList({object: "list", data: [
      {
        id: "deepseek-flash", object: "model", owned_by: "deepseek", name: "DeepSeek-V4.1-Flash",
        context_window: 1048576, max_output_tokens: 393216, input_modalities: ["text", "image"], output_modalities: ["text"],
        effort: {supported_levels: ["low", "high", "max"], default_level: "high"},
      },
      {
        id: "deepseek-v4-pro", object: "model", owned_by: "deepseek", name: "DeepSeek-V4-Pro",
        context_window: 1048576, max_output_tokens: 393216, input_modalities: ["text"], output_modalities: ["text"],
      },
      {id: "deepseek-flash", object: "model", owned_by: "deepseek", name: "Duplicate"},
    ]});
    expect(listing.models.map((model) => [model.id, model.label, model.detail, model.isHiddenByDefault])).toEqual([
      ["deepseek-flash", "DeepSeek-V4.1-Flash", "1M-token context, up to 384K output tokens", false],
      ["deepseek-v4-pro", "DeepSeek-V4-Pro", "1M-token context, up to 384K output tokens", false],
    ]);
  });

  it("hides a model that cannot return text and keeps models that omit the optional metadata", () => {
    const listing = projectDeepSeekModelList({object: "list", data: [
      {id: "deepseek-image-preview", object: "model", owned_by: "deepseek", output_modalities: ["image"]},
      {id: "deepseek-next", object: "model", owned_by: "deepseek", name: " ", context_window: 100000},
      {id: "deepseek-bare", object: "model", owned_by: "deepseek", max_output_tokens: 8192},
    ]});
    expect(listing.models.map((model) => [model.id, model.label, model.detail, model.isHiddenByDefault])).toEqual([
      ["deepseek-image-preview", undefined, undefined, true],
      ["deepseek-next", undefined, "100,000-token context", false],
      ["deepseek-bare", undefined, "Up to 8K output tokens", false],
    ]);
  });

  it("returns no models for an unexpected shape", () => {
    expect(projectDeepSeekModelList({models: []}).models).toEqual([]);
  });
});

describe("loadDeepSeekModels", () => {
  it("runs the pinned listModels command with the models-list capability", async () => {
    const calls: [string, string][] = [];
    const client = {
      executeProviderCommand: async (commandId: string, capability: string) => {
        calls.push([commandId, capability]);
        return {object: "list", data: [{id: "deepseek-flash", object: "model", owned_by: "deepseek"}]};
      },
    } as unknown as ConnectorStudioClient;
    const listing = await loadDeepSeekModels(client);
    expect(calls).toEqual([["listModels", deepSeekModelsListCapability]]);
    expect(listing.models.map((model) => model.id)).toEqual(["deepseek-flash"]);
  });
});
