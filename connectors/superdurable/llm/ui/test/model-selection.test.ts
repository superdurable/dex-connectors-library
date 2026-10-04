// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import type { ConnectorStudioClient } from "@superdurable/dex-connectors-react";

import { llmModelsListCapability } from "../src/models-list-capability.js";
import { loadLLMModels, validateLLMModelID, type LLMProvider, type LLMRegion } from "../src/model-selection.js";

type CommandCall = {commandId: string; capability: string};

// providerListResponses answers each list command as its provider does, with one model each.
const providerListResponses: Record<string, Record<string, unknown>> = {
  listOpenAIModels: {data: [{id: "gpt-6-sol", created: 1}]},
  listAnthropicModels: {data: [{id: "claude-sonnet-5", display_name: "Claude Sonnet 5"}], has_more: false},
  listGeminiModels: {models: [{name: "models/gemini-3.8-flash", supportedGenerationMethods: ["generateContent"]}]},
  listQwenModels: {output: {total: 1, page_no: 1, page_size: 100, models: [{model: "qwen3.7-plus", provider: "qwen"}]}},
  listQwenReasoningModels: {output: {total: 0, page_no: 1, page_size: 100, models: []}},
  listQwenModelsHongKong: {output: {total: 1, page_no: 1, page_size: 100, models: [{model: "qwen3.8-flash", provider: "qwen"}]}},
  listQwenReasoningModelsHongKong: {output: {total: 0, page_no: 1, page_size: 100, models: []}},
  listDeepSeekModels: {data: [{id: "deepseek-flash"}]},
  listMetaModels: {data: [{id: "muse-spark-1.3"}]},
  listMistralModels: {data: [{id: "mistral-large-2512", capabilities: {completion_chat: true}}]},
  listKimiModels: {data: [{id: "kimi-k2.6"}]},
  listKimiModelsChina: {data: [{id: "kimi-k3"}]},
  listXAIModels: {models: [{id: "grok-4.3", output_modalities: ["text"], aliases: []}]},
  listXAIModelsUS: {data: [{id: "grok-4.7"}]},
};

/** fakeStudioClient answers from providerListResponses, or rejects like the host for a command without one. */
function fakeStudioClient(failingCommandIDs: string[] = []): {client: ConnectorStudioClient; calls: CommandCall[]} {
  const calls: CommandCall[] = [];
  const client = {
    ready: undefined, busy: false,
    send: async () => ({}),
    executeProviderCommand: async (commandId: string, capability: string) => {
      calls.push({commandId, capability});
      const response = providerListResponses[commandId];
      if (!response || failingCommandIDs.includes(commandId)) throw new Error("Connector provider command failed");
      return response;
    },
  } satisfies ConnectorStudioClient;
  return {client, calls};
}

function savedConnection(configuration: Record<string, unknown>) {
  return {configuration, isConfigurationReported: true};
}

describe("loadLLMModels", () => {
  const routes: Array<{provider: LLMProvider; region?: LLMRegion; commandIDs: string[]; modelIDs: string[]}> = [
    {provider: "openai", commandIDs: ["listOpenAIModels"], modelIDs: ["gpt-6-sol"]},
    {provider: "anthropic", commandIDs: ["listAnthropicModels"], modelIDs: ["claude-sonnet-5"]},
    {provider: "gemini", commandIDs: ["listGeminiModels"], modelIDs: ["gemini-3.8-flash"]},
    {provider: "qwen", commandIDs: ["listQwenModels", "listQwenReasoningModels"], modelIDs: ["qwen3.7-plus"]},
    {provider: "qwen", region: "hong-kong", commandIDs: ["listQwenModelsHongKong", "listQwenReasoningModelsHongKong"], modelIDs: ["qwen3.8-flash"]},
    {provider: "deepseek", commandIDs: ["listDeepSeekModels"], modelIDs: ["deepseek-flash"]},
    {provider: "meta", commandIDs: ["listMetaModels"], modelIDs: ["muse-spark-1.3"]},
    {provider: "mistral", commandIDs: ["listMistralModels"], modelIDs: ["mistral-large-2512"]},
    {provider: "mistral", region: "eu", commandIDs: ["listMistralModels"], modelIDs: ["mistral-large-2512"]},
    {provider: "kimi", commandIDs: ["listKimiModels"], modelIDs: ["kimi-k2.6"]},
    {provider: "kimi", region: "china", commandIDs: ["listKimiModelsChina"], modelIDs: ["kimi-k3"]},
    {provider: "xai", commandIDs: ["listXAIModels"], modelIDs: ["grok-4.3"]},
    {provider: "xai", region: "us", commandIDs: ["listXAIModelsUS"], modelIDs: ["grok-4.7"]},
  ];
  for (const route of routes) {
    it(`runs only the ${route.provider} commands in region ${route.region ?? "global"} and lists bare model IDs`, async () => {
      const {client, calls} = fakeStudioClient();
      const configuration: Record<string, unknown> = {provider: route.provider, model: ""};
      if (route.region !== undefined) configuration.region = route.region;
      const listing = await loadLLMModels(client, savedConnection(configuration));
      expect(calls.map((call) => call.commandId)).toEqual(route.commandIDs);
      expect(calls.every((call) => call.capability === llmModelsListCapability)).toBe(true);
      expect(listing.models.map((model) => model.id)).toEqual(route.modelIDs);
      expect(listing.models.every((model) => !model.id.includes("/"))).toBe(true);
    });
  }

  it("names the provider and points at the key and region when its list fails", async () => {
    const {client, calls} = fakeStudioClient(["listKimiModels"]);
    await expect(loadLLMModels(client, savedConnection({provider: "kimi"}))).rejects.toThrow(
      "The Kimi model list failed (Connector provider command failed). Check the connection's api_key and region.",
    );
    expect(calls.map((call) => call.commandId), "the key never reaches the other platform").toEqual(["listKimiModels"]);
  });

  it("lists nothing for Model Studio China (Beijing) and explains model ID entry", async () => {
    const {client, calls} = fakeStudioClient();
    const listing = await loadLLMModels(client, savedConnection({provider: "qwen", region: "china"}));
    expect(calls).toEqual([]);
    expect(listing.models).toEqual([]);
    expect(listing.notices?.[0]?.message).toContain("Enter the model ID");
  });

  it("runs no command before the connection saves a provider", async () => {
    for (const configuration of [{}, {provider: ""}, {provider: "llama"}, {provider: 1}]) {
      const {client, calls} = fakeStudioClient();
      const listing = await loadLLMModels(client, savedConnection(configuration));
      expect(calls, JSON.stringify(configuration)).toEqual([]);
      expect(listing.models).toEqual([]);
      expect(listing.notices).toEqual([{tone: "attention", message: expect.stringContaining("Save the connection with its provider")}]);
    }
  });

  it("runs no command on a Dex Web release that reports no connection configuration", async () => {
    const {client, calls} = fakeStudioClient();
    const listing = await loadLLMModels(client, {configuration: {}, isConfigurationReported: false});
    expect(calls).toEqual([]);
    expect(listing.notices).toEqual([{tone: "attention", message: expect.stringContaining("does not tell the picker the connection's provider")}]);
  });

  it("uses the global lists for a blank or unknown region", async () => {
    for (const region of ["", "mars", undefined]) {
      const {client, calls} = fakeStudioClient();
      await loadLLMModels(client, savedConnection({provider: "xai", region}));
      expect(calls.map((call) => call.commandId)).toEqual(["listXAIModels"]);
    }
  });
});

describe("validateLLMModelID", () => {
  it("accepts any provider model ID the body rule accepts and never requires a provider prefix", () => {
    for (const model of ["gpt-6-sol", "ft:gpt-6-sol:acme::abc123", "models/gemini-3.8-flash", "claude-opus-5-5", "LLM|model"]) {
      expect(validateLLMModelID(model), model).toBeUndefined();
    }
  });

  it("rejects an ID with spaces or more than 256 characters", () => {
    for (const model of ["claude sonnet 5", "x".repeat(257), "gpté"]) {
      expect(validateLLMModelID(model), model).toBe("The model ID must be 1 to 256 printable ASCII characters without spaces.");
    }
  });
});
