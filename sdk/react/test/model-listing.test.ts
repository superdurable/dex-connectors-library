// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";

import {
  ConnectorStudioCommandError,
  executeFirstAcceptedProviderCommand,
  hasAnyModelIDFragment,
  openAICompatibleModelOptions,
  readProviderModelArray,
  type ConnectorStudioClient,
} from "../src/index.js";

function clientAnswering(answers: Record<string, Record<string, unknown> | Error>, calls: string[]): ConnectorStudioClient {
  return {
    ready: undefined, busy: false,
    send: async () => ({}),
    executeProviderCommand: async (commandId) => {
      calls.push(commandId);
      const answer = answers[commandId];
      if (answer instanceof Error) throw answer;
      return answer;
    },
  };
}

describe("executeFirstAcceptedProviderCommand", () => {
  it("falls back to the next pinned host when the first rejects the key", async () => {
    const calls: string[] = [];
    const client = clientAnswering({listModels: new ConnectorStudioCommandError("PROVIDER_ERROR", "rejected"), listModelsChina: {data: []}}, calls);
    await expect(executeFirstAcceptedProviderCommand(client, "kimi.models-list", ["listModels", "listModelsChina"])).resolves.toEqual({data: []});
    expect(calls).toEqual(["listModels", "listModelsChina"]);
  });

  it("rethrows the last error when every host fails", async () => {
    const client = clientAnswering({first: new Error("first"), second: new Error("second")}, []);
    await expect(executeFirstAcceptedProviderCommand(client, "x", ["first", "second"])).rejects.toThrow("second");
  });
});

describe("openAICompatibleModelOptions", () => {
  it("projects ids, drops duplicates and blanks, and applies the connector filter", () => {
    const options = openAICompatibleModelOptions({data: [
      {id: "gpt-6-astra"}, {id: "text-embedding-4"}, {id: "gpt-6-astra"}, {id: " "}, {name: "no id"}, "not an object",
    ]}, {isHiddenByDefault: (id) => hasAnyModelIDFragment(id, ["embedding"])});
    expect(options).toEqual([
      {id: "gpt-6-astra", label: undefined, detail: undefined, badges: undefined, isHiddenByDefault: false},
      {id: "text-embedding-4", label: undefined, detail: undefined, badges: undefined, isHiddenByDefault: true},
    ]);
  });

  it("tolerates a response without a data array", () => {
    expect(readProviderModelArray({models: []})).toEqual([]);
    expect(readProviderModelArray({models: [{id: "a"}]}, "models")).toEqual([{id: "a"}]);
  });
});
