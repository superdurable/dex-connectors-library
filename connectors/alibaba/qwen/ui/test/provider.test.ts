// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";

import type { ConnectorStudioClient } from "@superdurable/dex-connectors-react";

import { loadQwenModels, projectQwenModelList, qwenModelsListCapability } from "../src/provider.js";

function textModel(model: string, overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    model, name: model.toUpperCase(), provider: "qwen", capabilities: ["TG"], features: [],
    inference_metadata: {request_modality: ["Text"], response_modality: ["Text"]},
    model_info: {context_window: 1_000_000},
    ...overrides,
  };
}

function page(models: Record<string, unknown>[], pageNumber: number, total: number): Record<string, unknown> {
  return {code: null, message: null, success: true, output: {total, page_no: pageNumber, page_size: 100, models}, request_id: "req"};
}

describe("projectQwenModelList", () => {
  it("shows Qwen text models and hides third-party, realtime, and non-text models", () => {
    const listing = projectQwenModelList(page([
      textModel("qwen3.7-plus", {name: "Qwen3.7-Plus", capabilities: ["TG", "Reasoning"], features: ["structured-outputs"]}),
      textModel("deepseek-v4-pro", {provider: "deepseek"}),
      textModel("qwen3.8-omni-flash-realtime", {capabilities: ["Realtime-Omni"]}),
      textModel("qwen-image-3.0-pro", {inference_metadata: {request_modality: ["Text"], response_modality: ["Image"]}}),
      textModel("qwen3-tts-flash", {inference_metadata: {}}),
      textModel("qwen-plus", {inference_offline_info: {offline_time: "2027-01-15"}, model_info: {context_window: null}}),
      textModel("qwen3.7-plus"),
      {name: "missing model ID", provider: "qwen"},
    ], 1, 8), page([
      textModel("qwen3.7-plus", {capabilities: ["TG", "Reasoning"]}),
      textModel("qwen3.8-2.4t-a95b", {name: "Qwen3.8-2.4T-A95B", capabilities: ["Reasoning"]}),
      textModel("deepseek-r1", {provider: "deepseek", capabilities: ["Reasoning"]}),
    ], 1, 3));
    expect(listing.models.map((model) => [model.id, model.label, model.isHiddenByDefault, model.detail, model.badges])).toEqual([
      ["qwen3.7-plus", "Qwen3.7-Plus", false, "1,000,000-token context", ["reasoning", "structured output"]],
      ["deepseek-v4-pro", "DEEPSEEK-V4-PRO", true, "1,000,000-token context", []],
      ["qwen3.8-omni-flash-realtime", "QWEN3.8-OMNI-FLASH-REALTIME", true, "1,000,000-token context", []],
      ["qwen-image-3.0-pro", "QWEN-IMAGE-3.0-PRO", true, "1,000,000-token context", []],
      ["qwen3-tts-flash", "QWEN3-TTS-FLASH", true, "1,000,000-token context", []],
      ["qwen-plus", "QWEN-PLUS", false, "Scheduled to go offline 2027-01-15.", []],
      ["qwen3.8-2.4t-a95b", "Qwen3.8-2.4T-A95B", false, "1,000,000-token context", ["reasoning"]],
      ["deepseek-r1", "DEEPSEEK-R1", true, "1,000,000-token context", ["reasoning"]],
    ]);
  });

  it("returns no models for an unexpected shape", () => {
    expect(projectQwenModelList({data: []}).models).toEqual([]);
    expect(projectQwenModelList({output: {models: "not-an-array"}}).models).toEqual([]);
  });
});

describe("loadQwenModels", () => {
  it("merges the Reasoning list into the text generation list without duplicates", async () => {
    const calls: string[] = [];
    const client = fakeClient(async (commandId, capability, parameters) => {
      calls.push(`${commandId}:${parameters?.pageNumber}`);
      expect(capability).toBe(qwenModelsListCapability);
      if (commandId === "listReasoningModels") {
        return page([textModel("qwen3.7-plus", {capabilities: ["TG", "Reasoning"]}), textModel("qwq-plus", {capabilities: ["Reasoning"]})], 1, 2);
      }
      return page([textModel("qwen3.7-plus"), textModel("qwen3.7-flash")], 1, 2);
    });
    const listing = await loadQwenModels(client);
    expect(calls).toEqual(["listModels:1", "listReasoningModels:1"]);
    expect(listing.models.map((model) => model.id)).toEqual(["qwen3.7-plus", "qwen3.7-flash", "qwq-plus"]);
    expect(listing.isTruncated).toBe(false);
  });

  it("falls back to China (Hong Kong) once and keeps it for later pages and the Reasoning list", async () => {
    const calls: string[] = [];
    const client = fakeClient(async (commandId, _capability, parameters) => {
      calls.push(`${commandId}:${parameters?.pageNumber}`);
      if (!commandId.endsWith("HongKong")) throw new Error("Connector provider response is invalid");
      const pageNumber = Number(parameters?.pageNumber);
      if (commandId === "listReasoningModelsHongKong") return page([textModel("qwq-plus", {capabilities: ["Reasoning"]})], pageNumber, 1);
      return page([textModel(`qwen-page-${pageNumber}-a`), textModel(`qwen-page-${pageNumber}-b`)], pageNumber, 150);
    });
    const listing = await loadQwenModels(client);
    expect(calls).toEqual(["listModels:1", "listModelsHongKong:1", "listModelsHongKong:2", "listReasoningModelsHongKong:1"]);
    expect(listing.models.map((model) => model.id)).toEqual(["qwen-page-1-a", "qwen-page-1-b", "qwen-page-2-a", "qwen-page-2-b", "qwq-plus"]);
    expect(listing.isTruncated).toBe(false);
  });

  it("reports a later page's own error instead of trying another region", async () => {
    const calls: string[] = [];
    const client = fakeClient(async (commandId, _capability, parameters) => {
      calls.push(`${commandId}:${parameters?.pageNumber}`);
      const pageNumber = Number(parameters?.pageNumber);
      if (pageNumber === 2) throw new Error("Singapore page 2 is unavailable");
      return page([textModel(`qwen-page-${pageNumber}`)], pageNumber, 150);
    });
    await expect(loadQwenModels(client)).rejects.toThrow("Singapore page 2 is unavailable");
    expect(calls).toEqual(["listModels:1", "listModels:2"]);
  });

  it("reads later pages and reports truncation at the page limit", async () => {
    const client = fakeClient(async (commandId, _capability, parameters) => {
      const pageNumber = Number(parameters?.pageNumber);
      if (commandId === "listReasoningModels") return page([textModel("qwq-plus", {capabilities: ["Reasoning"]})], pageNumber, 1);
      return {output: {total: 1000, page_no: pageNumber, page_size: 100, models: [textModel(`qwen-page-${pageNumber}`)]}};
    });
    const listing = await loadQwenModels(client);
    expect(listing.models.map((model) => model.id)).toEqual(["qwen-page-1", "qwen-page-2", "qwen-page-3", "qwen-page-4", "qwen-page-5", "qwq-plus"]);
    expect(listing.isTruncated).toBe(true);
  });

  it("rethrows when no region accepts the key, so ModelPicker offers manual entry", async () => {
    const client = fakeClient(async () => {
      throw new Error("Connector provider response is invalid");
    });
    await expect(loadQwenModels(client)).rejects.toThrow("Connector provider response is invalid");
  });
});

function fakeClient(
  executeProviderCommand: (commandId: string, capability: string, parameters?: Record<string, string>) => Promise<Record<string, unknown>>,
): ConnectorStudioClient {
  return {
    ready: undefined, busy: false,
    send: async () => ({}),
    executeProviderCommand,
  };
}
