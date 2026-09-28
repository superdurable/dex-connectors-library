// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectorStudioClient } from "@superdurable/dex-connectors-react";
import { describe, expect, it } from "vitest";

import { kimiModelsListCapability, loadKimiModels, projectKimiModelList } from "../src/provider.js";

describe("projectKimiModelList", () => {
  it("shows current models with their context window and reasoning flag, and hides retired families", () => {
    const listing = projectKimiModelList({object: "list", data: [
      {id: "kimi-k3", object: "model", owned_by: "moonshot", context_length: 1048576, supports_reasoning: true, supports_image_in: true},
      {id: "kimi-k2.7-code", object: "model", owned_by: "moonshot", context_length: 262144, supports_reasoning: true},
      {id: "kimi-k2.6", object: "model", owned_by: "moonshot", context_length: 262144, supports_reasoning: true, supports_video_in: true},
      {id: "kimi-k2.5", object: "model", owned_by: "moonshot", context_length: 262144},
      {id: "kimi-k2-turbo-preview", object: "model", owned_by: "moonshot", context_length: 262144},
      {id: "moonshot-v1-8k", object: "model", owned_by: "moonshot", context_length: 8192, supports_reasoning: false},
      {id: "kimi-k4-preview", object: "model", owned_by: "moonshot", context_length: 2000000},
      {id: "kimi-k3", object: "model", owned_by: "moonshot", context_length: 1048576},
    ]});
    expect(listing.models.map((model) => [model.id, model.isHiddenByDefault, model.detail, model.badges])).toEqual([
      ["kimi-k3", false, "1M-token context", ["reasoning"]],
      ["kimi-k2.7-code", false, "256K-token context", ["reasoning"]],
      ["kimi-k2.6", false, "256K-token context", ["reasoning"]],
      ["kimi-k2.5", true, "256K-token context", []],
      ["kimi-k2-turbo-preview", true, "256K-token context", []],
      ["moonshot-v1-8k", true, "8K-token context", []],
      ["kimi-k4-preview", false, "2000000-token context", []],
    ]);
  });

  it("omits the detail when the context length is missing or invalid", () => {
    const listing = projectKimiModelList({data: [{id: "kimi-k3"}, {id: "kimi-k2.6", context_length: "256k"}, {id: "kimi-k2.7-code", context_length: -1}]});
    expect(listing.models.map((model) => model.detail)).toEqual([undefined, undefined, undefined]);
  });

  it("returns no models for an unexpected shape", () => {
    expect(projectKimiModelList({models: []}).models).toEqual([]);
  });
});

describe("loadKimiModels", () => {
  it("lists from the China platform when the global platform rejects the key", async () => {
    const calls: string[] = [];
    const client = fakeClient(async (commandId, capability) => {
      calls.push(`${commandId}:${capability}`);
      if (commandId === "listModels") throw new Error("provider rejected the credential");
      return {data: [{id: "kimi-k2.6", context_length: 262144}]};
    });
    const listing = await loadKimiModels(client);
    expect(calls).toEqual([`listModels:${kimiModelsListCapability}`, `listModelsChina:${kimiModelsListCapability}`]);
    expect(listing.models.map((model) => model.id)).toEqual(["kimi-k2.6"]);
  });

  it("does not call the China platform when the global platform accepts the key", async () => {
    const calls: string[] = [];
    const client = fakeClient(async (commandId) => {
      calls.push(commandId);
      return {data: [{id: "kimi-k3"}]};
    });
    await loadKimiModels(client);
    expect(calls).toEqual(["listModels"]);
  });

  it("rejects when neither platform accepts the key, so the picker offers manual entry", async () => {
    const client = fakeClient(async (commandId) => {
      throw new Error(`${commandId} rejected`);
    });
    await expect(loadKimiModels(client)).rejects.toThrow("listModelsChina rejected");
  });
});

function fakeClient(
  executeProviderCommand: (commandId: string, capability: string) => Promise<Record<string, unknown>>,
): ConnectorStudioClient {
  return {
    ready: undefined, busy: false,
    send: async () => ({}),
    executeProviderCommand,
  };
}
