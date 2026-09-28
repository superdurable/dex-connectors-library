// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";

import { ConnectorStudioCommandError, type ConnectorStudioClient } from "../src/index.js";
import {
  loadGeminiModelListing,
  projectNativeGeminiModels,
  projectOpenAICompatibleGeminiModelList,
  type GeminiModelListCommands,
} from "../src/provider-model-lists.js";

interface RecordedCommand {
  commandId: string;
  capability: string;
  parameters: Record<string, string>;
}

const commands: GeminiModelListCommands = {
  capability: "llm.models-list", nativeCommandId: "listGeminiModels", openAICompatibleCommandId: "listGeminiOpenAICompatibleModels",
};

/** fakeClient answers provider commands by ID, rejecting any ID without a reply as an old host does. */
function fakeClient(replies: Record<string, Array<Record<string, unknown>>>): {client: ConnectorStudioClient; recorded: RecordedCommand[]} {
  const recorded: RecordedCommand[] = [];
  const client: ConnectorStudioClient = {
    ready: undefined,
    busy: false,
    send: async () => ({}),
    executeProviderCommand: async (commandId, capability, parameters = {}) => {
      recorded.push({commandId, capability, parameters});
      const reply = replies[commandId]?.shift();
      if (!reply) throw new ConnectorStudioCommandError("COMMAND_UNSUPPORTED", "unsupported credential scheme");
      return reply;
    },
  };
  return {client, recorded};
}

const nativeFlash = {
  name: "models/gemini-3.8-flash", displayName: "Gemini 3.8 Flash", inputTokenLimit: 1048576, outputTokenLimit: 65536,
  supportedGenerationMethods: ["generateContent", "countTokens", "createCachedContent", "batchGenerateContent"], thinking: true,
};

describe("projectNativeGeminiModels", () => {
  it("keeps generateContent text models visible and hides every other model", () => {
    const models = projectNativeGeminiModels([
      nativeFlash,
      {name: "models/gemini-3.5-flash-lite", displayName: "Gemini 3.5 Flash-Lite", supportedGenerationMethods: ["generateContent"]},
      {name: "models/gemini-embedding-001", displayName: "Gemini Embedding", supportedGenerationMethods: ["embedContent"]},
      {name: "models/gemini-3.8-live", displayName: "Gemini 3.8 Live", supportedGenerationMethods: ["bidiGenerateContent"]},
      {name: "models/gemini-3.8-flash-tts", displayName: "Gemini 3.8 Flash TTS", supportedGenerationMethods: ["generateContent"]},
      {name: "models/new-text-model", supportedGenerationMethods: ["generateContent"]},
      {name: "models/gemini-3.8-flash", supportedGenerationMethods: ["generateContent"]},
      {displayName: "No name"},
    ]);
    expect(models.map((model) => [model.id, model.isHiddenByDefault])).toEqual([
      ["gemini-3.8-flash", false], ["gemini-3.5-flash-lite", false], ["gemini-embedding-001", true],
      ["gemini-3.8-live", true], ["gemini-3.8-flash-tts", true], ["new-text-model", false],
    ]);
    expect(models[0]).toEqual({
      id: "gemini-3.8-flash", label: "Gemini 3.8 Flash", detail: "1,048,576 input / 65,536 output tokens",
      badges: ["thinking"], isHiddenByDefault: false,
    });
    expect(models[1].detail).toBeUndefined();
    expect(models[1].badges).toBeUndefined();
  });
});

describe("projectOpenAICompatibleGeminiModelList", () => {
  it("strips the models/ prefix and hides non-text models", () => {
    const listing = projectOpenAICompatibleGeminiModelList({object: "list", data: [
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
    expect(projectOpenAICompatibleGeminiModelList({models: "none"}).models).toEqual([]);
  });
});

describe("loadGeminiModelListing", () => {
  it("pages through the caller's native command with pageToken", async () => {
    const {client, recorded} = fakeClient({listGeminiModels: [
      {models: [nativeFlash], nextPageToken: "page-2"},
      {models: [{name: "models/gemini-3.5-flash-lite", supportedGenerationMethods: ["generateContent"]}]},
    ]});
    const listing = await loadGeminiModelListing(client, commands);
    expect(listing.models.map((model) => model.id)).toEqual(["gemini-3.8-flash", "gemini-3.5-flash-lite"]);
    expect(listing.isTruncated).toBe(false);
    expect(recorded).toEqual([
      {commandId: "listGeminiModels", capability: "llm.models-list", parameters: {}},
      {commandId: "listGeminiModels", capability: "llm.models-list", parameters: {pageToken: "page-2"}},
    ]);
  });

  it("falls back to the caller's OpenAI-compatible command when the host rejects the header credential", async () => {
    const {client, recorded} = fakeClient({listGeminiOpenAICompatibleModels: [
      {object: "list", data: [{id: "models/gemini-3.8-flash"}, {id: "models/gemini-embedding-002"}]},
    ]});
    const listing = await loadGeminiModelListing(client, commands);
    expect(listing.models.map((model) => [model.id, model.isHiddenByDefault])).toEqual([
      ["gemini-3.8-flash", false], ["gemini-embedding-002", true],
    ]);
    expect(recorded).toEqual([
      {commandId: "listGeminiModels", capability: "llm.models-list", parameters: {}},
      {commandId: "listGeminiOpenAICompatibleModels", capability: "llm.models-list", parameters: {}},
    ]);
  });

  it("rejects when both lists fail, so the picker offers manual entry", async () => {
    const {client} = fakeClient({});
    await expect(loadGeminiModelListing(client, commands)).rejects.toBeInstanceOf(ConnectorStudioCommandError);
  });
});
