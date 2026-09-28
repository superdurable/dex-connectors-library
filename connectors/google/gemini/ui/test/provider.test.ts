// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ConnectorStudioCommandError, type ConnectorStudioClient } from "@superdurable/dex-connectors-react";
import { describe, expect, it } from "vitest";

import {
  geminiModelsListCapability,
  loadGeminiModels,
  projectNativeGeminiModels,
  projectOpenAICompatibleGeminiModelList,
} from "../src/provider.js";

interface RecordedCommand {
  commandId: string;
  capability: string;
  parameters: Record<string, string>;
}

/** fakeClient answers provider commands by ID, rejecting any ID without a reply as an old host does. */
function fakeClient(replies: Record<string, Array<Record<string, unknown>>>): {client: ConnectorStudioClient; commands: RecordedCommand[]} {
  const commands: RecordedCommand[] = [];
  const client: ConnectorStudioClient = {
    ready: undefined,
    busy: false,
    send: async () => ({}),
    executeProviderCommand: async (commandId, capability, parameters = {}) => {
      commands.push({commandId, capability, parameters});
      const reply = replies[commandId]?.shift();
      if (!reply) throw new ConnectorStudioCommandError("COMMAND_UNSUPPORTED", "unsupported credential scheme");
      return reply;
    },
  };
  return {client, commands};
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

describe("loadGeminiModels", () => {
  it("pages through the native list with pageToken", async () => {
    const {client, commands} = fakeClient({listModels: [
      {models: [nativeFlash], nextPageToken: "page-2"},
      {models: [{name: "models/gemini-3.5-flash-lite", supportedGenerationMethods: ["generateContent"]}]},
    ]});
    const listing = await loadGeminiModels(client);
    expect(listing.models.map((model) => model.id)).toEqual(["gemini-3.8-flash", "gemini-3.5-flash-lite"]);
    expect(listing.isTruncated).toBe(false);
    expect(commands).toEqual([
      {commandId: "listModels", capability: geminiModelsListCapability, parameters: {}},
      {commandId: "listModels", capability: geminiModelsListCapability, parameters: {pageToken: "page-2"}},
    ]);
  });

  it("falls back to the OpenAI-compatible list when the host rejects the header credential", async () => {
    const {client, commands} = fakeClient({listOpenAICompatibleModels: [
      {object: "list", data: [{id: "models/gemini-3.8-flash"}, {id: "models/gemini-embedding-002"}]},
    ]});
    const listing = await loadGeminiModels(client);
    expect(listing.models.map((model) => [model.id, model.isHiddenByDefault])).toEqual([
      ["gemini-3.8-flash", false], ["gemini-embedding-002", true],
    ]);
    expect(commands.map((command) => command.commandId)).toEqual(["listModels", "listOpenAICompatibleModels"]);
  });

  it("rejects when both lists fail, so the picker offers manual entry", async () => {
    const {client} = fakeClient({});
    await expect(loadGeminiModels(client)).rejects.toBeInstanceOf(ConnectorStudioCommandError);
  });
});
