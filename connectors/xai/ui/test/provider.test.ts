// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";

import { ConnectorStudioCommandError, filterModelOptions, type ConnectorStudioClient } from "@superdurable/dex-connectors-react";

import { grokModelsListCapability, loadGrokModels, projectGrokModelList } from "../src/provider.js";

// languageModels follows the GET https://api.x.ai/v1/language-models reference shape.
const languageModels = {models: [
  {
    id: "grok-4.7", object: "model", owned_by: "xai", input_modalities: ["text", "image"], output_modalities: ["text"], aliases: [],
    capabilities: {reasoning_effort: ["low", "medium", "high", "xhigh"], default_reasoning_effort: "high"},
  },
  {
    id: "grok-4.3", object: "model", owned_by: "xai", input_modalities: ["text", "image"], output_modalities: ["text"],
    aliases: ["grok-4.3-latest", "grok-4.3"],
    capabilities: {reasoning_effort: ["none", "low", "medium", "high", "xhigh"], default_reasoning_effort: "low"},
  },
  {
    id: "grok-4.20-0309-reasoning", object: "model", owned_by: "xai", input_modalities: ["text", "image"], output_modalities: ["text"],
    aliases: ["grok-4.20", "grok-4.20-reasoning", "grok-4.20-reasoning-latest"],
  },
  {
    id: "grok-4.20-multi-agent-0309", object: "model", owned_by: "xai", input_modalities: ["text", "image"], output_modalities: ["text"],
    aliases: ["grok-4.20-multi-agent"],
  },
  {id: "grok-future-image-model", object: "model", owned_by: "xai", input_modalities: ["text"], output_modalities: ["image"], aliases: []},
  {id: "grok-4.7", object: "model", owned_by: "xai", output_modalities: ["text"], aliases: []},
  {id: " ", object: "model", owned_by: "xai", output_modalities: ["text"], aliases: []},
]};

describe("projectGrokModelList", () => {
  it("shows text models in xAI's order with aliases and reasoning efforts, and hides multi-agent and non-text models", () => {
    const listing = projectGrokModelList(languageModels);
    expect(listing.models.map((model) => [model.id, model.label, model.detail, model.isHiddenByDefault])).toEqual([
      ["grok-4.7", undefined, "Reasoning effort: low, medium, high, xhigh; default high", false],
      ["grok-4.3", "grok-4.3 (grok-4.3-latest)", "Reasoning effort: none, low, medium, high, xhigh; default low", false],
      ["grok-4.20-0309-reasoning", "grok-4.20-0309-reasoning (grok-4.20, grok-4.20-reasoning, grok-4.20-reasoning-latest)", undefined, false],
      ["grok-4.20-multi-agent-0309", "grok-4.20-multi-agent-0309 (grok-4.20-multi-agent)", "Multi-agent models do not serve Chat Completions.", true],
      ["grok-future-image-model", undefined, "Does not output text.", true],
    ]);
  });

  it("finds a model by one of its aliases", () => {
    const models = projectGrokModelList(languageModels).models;
    expect(filterModelOptions(models, "grok-4.20-reasoning-latest", false).map((model) => model.id)).toEqual(["grok-4.20-0309-reasoning"]);
    expect(filterModelOptions(models, "grok-4.3-latest", false).map((model) => model.id)).toEqual(["grok-4.3"]);
  });

  it("reads the US regional data list and hides IDs that name a non-chat family", () => {
    const listing = projectGrokModelList({object: "list", data: [
      {id: "grok-4.7", object: "model", owned_by: "xai", aliases: [], capabilities: {reasoning_effort: ["low", "medium", "high", "xhigh"]}},
      {id: "grok-4.6", object: "model", owned_by: "xai", aliases: ["grok-4.6-latest"]},
      {id: "grok-imagine-image-2.0", object: "model", owned_by: "xai", aliases: []},
    ]});
    expect(listing.models.map((model) => [model.id, model.label, model.detail, model.isHiddenByDefault])).toEqual([
      ["grok-4.7", undefined, "Reasoning effort: low, medium, high, xhigh", false],
      ["grok-4.6", "grok-4.6 (grok-4.6-latest)", undefined, false],
      ["grok-imagine-image-2.0", undefined, undefined, true],
    ]);
  });

  it("returns no models for an unexpected shape", () => {
    expect(projectGrokModelList({error: "forbidden"}).models).toEqual([]);
  });
});

describe("loadGrokModels", () => {
  it("falls back to the US regional list when the global list fails", async () => {
    const calls: string[] = [];
    const client = clientAnswering({
      listModels: new ConnectorStudioCommandError("PROVIDER_ERROR", "403"),
      listModelsUS: {object: "list", data: [{id: "grok-4.7", aliases: []}]},
    }, calls);
    await expect(loadGrokModels(client)).resolves.toEqual({models: [
      {id: "grok-4.7", label: undefined, detail: undefined, badges: undefined, isHiddenByDefault: false},
    ]});
    expect(calls).toEqual([`listModels:${grokModelsListCapability}`, `listModelsUS:${grokModelsListCapability}`]);
  });

  it("rejects when both lists fail, so the picker offers manual model entry", async () => {
    const client = clientAnswering({
      listModels: new ConnectorStudioCommandError("PROVIDER_ERROR", "403"),
      listModelsUS: new ConnectorStudioCommandError("PROVIDER_ERROR", "403 on the US host"),
    }, []);
    await expect(loadGrokModels(client)).rejects.toThrow("403 on the US host");
  });
});

function clientAnswering(answers: Record<string, Record<string, unknown> | Error>, calls: string[]): ConnectorStudioClient {
  return {
    ready: undefined, busy: false,
    send: async () => ({}),
    executeProviderCommand: async (commandId, capability) => {
      calls.push(`${commandId}:${capability}`);
      const answer = answers[commandId];
      if (answer instanceof Error) throw answer;
      return answer;
    },
  };
}
