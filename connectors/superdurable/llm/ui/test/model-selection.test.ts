// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import type { ConnectorStudioClient, ModelListing } from "@superdurable/dex-connectors-react";

import {
  combineModelListings,
  llmModelsListCapability,
  loadLLMModels,
  validateProviderQualifiedModel,
  type ProviderModelSource,
} from "../src/model-selection.js";

type CommandCall = {commandId: string; capability: string; parameters: Record<string, string> | undefined};

/** fakeStudioClient answers each command from responses, or rejects like the host when a command has none. */
function fakeStudioClient(responses: Record<string, (parameters: Record<string, string> | undefined) => Record<string, unknown>>): {
  client: ConnectorStudioClient; calls: CommandCall[];
} {
  const calls: CommandCall[] = [];
  const client = {
    ready: undefined, busy: false,
    send: async () => ({}),
    executeProviderCommand: async (commandId: string, capability: string, parameters?: Record<string, string>) => {
      calls.push({commandId, capability, parameters});
      const respond = responses[commandId];
      if (!respond) throw new Error("Connector provider command failed");
      return respond(parameters);
    },
  } satisfies ConnectorStudioClient;
  return {client, calls};
}

function source(prefix: ProviderModelSource["prefix"], label: string, load: () => Promise<ModelListing>): ProviderModelSource {
  return {prefix, label, keyName: `the ${label} key`, load};
}

// Hosts that predate the connection context report no auth methods, so every provider is listed.
const oldHostConnection = {authMethodIds: []};

describe("combineModelListings", () => {
  it("puts one default option per provider first, then prefixed models badged by provider in source order", async () => {
    const listing = await combineModelListings([
      source("openai", "OpenAI", async () => ({models: [{id: "gpt-6-sol", detail: "Shuts down on 2027-01-01."}]})),
      source("anthropic", "Claude", async () => ({models: [{id: "claude-sonnet-5", label: "Claude Sonnet 5", badges: ["effort"]}], isTruncated: true})),
      source("gemini", "Gemini", async () => ({models: [{id: "text-embedding-005", isHiddenByDefault: true}]})),
    ]);
    expect(listing.models).toEqual([
      {id: "openai", label: "OpenAI default model", detail: "The model the OpenAI connector uses when a Step names none.", badges: ["OpenAI"]},
      {id: "anthropic", label: "Claude default model", detail: "The model the Claude connector uses when a Step names none.", badges: ["Claude"]},
      {id: "gemini", label: "Gemini default model", detail: "The model the Gemini connector uses when a Step names none.", badges: ["Gemini"]},
      {id: "openai/gpt-6-sol", detail: "Shuts down on 2027-01-01.", badges: ["OpenAI"]},
      {id: "anthropic/claude-sonnet-5", label: "Claude Sonnet 5", badges: ["Claude", "effort"]},
      {id: "gemini/text-embedding-005", isHiddenByDefault: true, badges: ["Gemini"]},
    ]);
    expect(listing.isTruncated).toBe(true);
    expect(listing.notices).toEqual([]);
  });

  it("keeps the other providers and every default option when one list fails, with one notice per failure", async () => {
    const listing = await combineModelListings([
      source("openai", "OpenAI", async () => ({models: [{id: "gpt-6-luna"}]})),
      {...source("anthropic", "Claude", async () => { throw new Error("anthropic-version: header is required"); }),
        hostLimitation: "Dex Web releases before cli-v0.13.10 cannot list Claude models."},
      source("gemini", "Gemini", async () => { throw new Error("credential is unavailable"); }),
    ]);
    expect(listing.models.map((model) => model.id)).toEqual(["openai", "anthropic", "gemini", "openai/gpt-6-luna"]);
    expect(listing.notices).toEqual([
      {tone: "attention", message: "Claude models could not be listed. Check the Claude key in the connection, or choose Claude default model, or enter anthropic/<model-id>. " +
        "Dex Web releases before cli-v0.13.10 cannot list Claude models. " +
        "Dex Web releases before cli-v0.14.2 also show a 'Connector provider command failed' banner at the top of the page for this list; it does not affect generation."},
      {tone: "attention", message: "Gemini models could not be listed. Check the Gemini key in the connection, or choose Gemini default model, or enter gemini/<model-id>. " +
        "Dex Web releases before cli-v0.14.2 also show a 'Connector provider command failed' banner at the top of the page for this list; it does not affect generation."},
    ]);
    expect(JSON.stringify(listing)).not.toContain("header is required");
  });

  it("keeps every default option when every list fails, with an overall notice before the per-provider notices", async () => {
    const listing = await combineModelListings([
      source("openai", "OpenAI", async () => { throw new Error("no key"); }),
      source("anthropic", "Claude", async () => { throw new Error("no key"); }),
      source("gemini", "Gemini", async () => { throw new Error("no key"); }),
    ]);
    expect(listing.models.map((model) => model.id)).toEqual(["openai", "anthropic", "gemini"]);
    expect(listing.isTruncated).toBe(false);
    expect(listing.notices?.map((notice) => notice.tone)).toEqual(["attention", "attention", "attention", "attention"]);
    expect(listing.notices?.map((notice) => notice.message.split(".")[0])).toEqual([
      "No provider's models could be listed",
      "OpenAI models could not be listed", "Claude models could not be listed", "Gemini models could not be listed",
    ]);
    expect(listing.notices?.[0]?.message).toBe("No provider's models could be listed. Choose a provider's default model, or enter provider/model-id.");
    expect(JSON.stringify(listing)).not.toContain("no key");
  });
});

describe("loadLLMModels", () => {
  it("runs each provider's own commands under the llm capability and pages Claude with afterId", async () => {
    const {client, calls} = fakeStudioClient({
      listOpenAIModels: () => ({data: [{id: "gpt-6-luna", created: 2}, {id: "gpt-6-sol", created: 3}, {id: "text-embedding-4", created: 4}]}),
      listAnthropicModels: (parameters) => parameters?.afterId === "claude-sonnet-5"
        ? {data: [{id: "claude-haiku-4-5"}], has_more: false, last_id: "claude-haiku-4-5"}
        : {data: [{id: "claude-opus-5-5"}, {id: "claude-sonnet-5"}], has_more: true, last_id: "claude-sonnet-5"},
      listGeminiModels: () => ({models: [{name: "models/gemini-3.8-flash", supportedGenerationMethods: ["generateContent"]}]}),
    });
    const listing = await loadLLMModels(client, oldHostConnection);
    expect(listing.models.filter((model) => !model.isHiddenByDefault).map((model) => model.id)).toEqual([
      "openai", "anthropic", "gemini",
      "openai/gpt-6-sol", "openai/gpt-6-luna",
      "anthropic/claude-opus-5-5", "anthropic/claude-sonnet-5", "anthropic/claude-haiku-4-5",
      "gemini/gemini-3.8-flash",
    ]);
    expect(listing.models.find((model) => model.id === "openai/text-embedding-4")?.isHiddenByDefault).toBe(true);
    expect(listing.notices).toEqual([]);
    expect(calls).toEqual([
      {commandId: "listOpenAIModels", capability: llmModelsListCapability, parameters: undefined},
      {commandId: "listAnthropicModels", capability: llmModelsListCapability, parameters: {}},
      {commandId: "listGeminiModels", capability: llmModelsListCapability, parameters: {}},
      {commandId: "listAnthropicModels", capability: llmModelsListCapability, parameters: {afterId: "claude-sonnet-5"}},
    ]);
  });

  it("falls back to the OpenAI-compatible Gemini list when the host rejects the native header scheme", async () => {
    const {client, calls} = fakeStudioClient({
      listOpenAIModels: () => ({data: []}),
      listGeminiOpenAICompatibleModels: () => ({data: [{id: "models/gemini-3.5-flash-lite"}]}),
    });
    const listing = await loadLLMModels(client, oldHostConnection);
    expect(listing.models.map((model) => model.id)).toEqual(["openai", "anthropic", "gemini", "gemini/gemini-3.5-flash-lite"]);
    expect(calls.map((call) => call.commandId)).toEqual(["listOpenAIModels", "listAnthropicModels", "listGeminiModels", "listGeminiOpenAICompatibleModels"]);
    expect(listing.notices?.map((notice) => notice.message.split(".")[0])).toEqual(["Claude models could not be listed"]);
  });

  it("lists only the providers the connection adds, in picker order, with a default option for each", async () => {
    const {client, calls} = fakeStudioClient({
      listOpenAIModels: () => ({data: [{id: "gpt-6-sol", created: 1}]}),
      listAnthropicModels: () => ({data: [{id: "claude-sonnet-5"}], has_more: false}),
      listGeminiModels: () => ({models: [{name: "models/gemini-3.8-flash", supportedGenerationMethods: ["generateContent"]}]}),
    });
    const listing = await loadLLMModels(client, {authMethodIds: ["gemini", "anthropic"]});
    expect(listing.models.map((model) => model.id)).toEqual([
      "anthropic", "gemini", "anthropic/claude-sonnet-5", "gemini/gemini-3.8-flash",
    ]);
    expect(calls.map((call) => call.commandId)).toEqual(["listAnthropicModels", "listGeminiModels"]);
    expect(listing.notices).toEqual([]);
  });

  it("keeps an added provider's default option when its only list fails", async () => {
    const {client, calls} = fakeStudioClient({listOpenAIModels: () => ({data: [{id: "gpt-6-sol", created: 1}]})});
    const listing = await loadLLMModels(client, {authMethodIds: ["anthropic"]});
    expect(listing.models.map((model) => model.id)).toEqual(["anthropic"]);
    expect(calls.map((call) => call.commandId), "a provider the connection has not added runs no list").toEqual(["listAnthropicModels"]);
    expect(listing.notices?.map((notice) => notice.message.split(".")[0])).toEqual([
      "No provider's models could be listed", "Claude models could not be listed",
    ]);
  });
});

describe("validateProviderQualifiedModel", () => {
  it("accepts a provider alone or provider/model that passes that provider's model-ID rule", () => {
    for (const model of [
      "openai", "anthropic", "gemini", "anthropic/claude-sonnet-5", "openai/ft:gpt-6-sol:acme::abc123",
      "gemini/gemini-3.8-flash", "gemini/models/gemini-3.8-flash", "anthropic/claude/with-slash",
    ]) {
      expect(validateProviderQualifiedModel(model), model).toBeUndefined();
    }
  });

  it("rejects what the llm connector would reject as defect, without repeating the value", () => {
    const selection = "Enter provider/model, where provider is openai, anthropic, or gemini, such as anthropic/claude-sonnet-5, or a provider alone for its default model.";
    for (const model of ["claude-canary-model", "Anthropic/claude-canary", "anthropic/", "anthropic/   ", "mistral/canary-large", "/claude-canary"]) {
      expect(validateProviderQualifiedModel(model), model).toBe(selection);
    }
    expect(validateProviderQualifiedModel("openai/gpt canary")).toBe("The model ID must be 1 to 256 printable ASCII characters without spaces.");
    expect(validateProviderQualifiedModel("gemini/gemini:canary!")).toMatch(/^The model ID must start with a letter or digit/);
  });
});
