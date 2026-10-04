// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import { filterModelOptions, type ConnectorStudioClient } from "@superdurable/dex-connectors-react";

import { projectDeepSeekModelList } from "../src/deepseek-model-list.js";
import { projectKimiModelList } from "../src/kimi-model-list.js";
import { projectMetaModelList } from "../src/meta-model-list.js";
import { projectMistralModelList } from "../src/mistral-model-list.js";
import { llmModelsListCapability } from "../src/models-list-capability.js";
import { loadQwenModels, projectQwenModelList, qwenHongKongModelLists, qwenSingaporeModelLists } from "../src/qwen-model-list.js";
import { projectXAIModelList } from "../src/xai-model-list.js";

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

describe("projectMetaModelList", () => {
  it("shows Muse Spark models, flags the Contributor tier, and hides other families", () => {
    const listing = projectMetaModelList({object: "list", data: [
      {id: "muse-spark-1.3", object: "model", created: 1790000300, owned_by: "meta"},
      {id: "muse-spark-1.3-contributor", object: "model", created: 1790000300, owned_by: "meta"},
      {id: "muse-image-1.0", object: "model", created: 1790000200, owned_by: "meta"},
      {id: "muse-voice-transcribe-1.0", object: "model", created: 1790000100, owned_by: "meta"},
      {id: "sam-3.1", object: "model", created: 1790000050, owned_by: "meta"},
      {id: "muse-spark-1.2", object: "model", created: 1780000000, owned_by: "meta"},
      {id: "muse-spark-1.3", object: "model", created: 1790000300, owned_by: "meta"},
    ]});
    expect(listing.models.map((model) => [model.id, model.isHiddenByDefault, model.detail])).toEqual([
      ["muse-spark-1.3", false, undefined],
      ["muse-spark-1.3-contributor", false, "Contributor tier: Meta may train on prompts and completions."],
      ["muse-image-1.0", true, undefined],
      ["muse-voice-transcribe-1.0", true, undefined],
      ["sam-3.1", true, undefined],
      ["muse-spark-1.2", false, undefined],
    ]);
  });

  it("returns no models for an unexpected shape", () => {
    expect(projectMetaModelList({models: []}).models).toEqual([]);
  });
});

const mistralChatCapabilities = {
  completion_chat: true, completion_fim: false, function_calling: true, fine_tuning: false, vision: true, classification: false,
};

describe("projectMistralModelList", () => {
  it("shows chat models, badges deprecation and capabilities, and hides non-chat and archived models", () => {
    const listing = projectMistralModelList({object: "list", data: [
      {id: "mistral-medium-3-5", object: "model", owned_by: "mistralai", type: "base", capabilities: mistralChatCapabilities,
        aliases: ["mistral-medium-3", "mistral-medium-latest"], deprecation: null, deprecation_replacement_model: null},
      {id: "mistral-large-2512", object: "model", type: "base", capabilities: mistralChatCapabilities, aliases: ["mistral-large-latest"]},
      {id: "ministral-14b-2512", object: "model", type: "base", capabilities: {completion_chat: true, function_calling: true, vision: false}},
      {id: "mistral-medium-2508", object: "model", type: "base", capabilities: mistralChatCapabilities,
        deprecation: "2026-08-31T12:00:00Z", deprecation_replacement_model: "mistral-medium-3-5"},
      {id: "mistral-small-2506", object: "model", type: "base", capabilities: {completion_chat: true}, deprecation: "2026-07-31"},
      {id: "mistral-embed-2312", object: "model", type: "base", capabilities: {completion_chat: false, classification: false}},
      {id: "mistral-ocr-2512", object: "model", type: "base", capabilities: {ocr: true}},
      {id: "ft:open-mistral-7b:587a6b29:20240514:7e773925", object: "model", type: "fine-tuned", job: "job-1",
        root: "open-mistral-7b", capabilities: {completion_chat: true}, archived: true},
      {id: "ft:ministral-8b-latest:587a6b29:20260101:1a2b3c4d", object: "model", type: "fine-tuned", job: "job-2",
        root: "ministral-8b-latest", capabilities: {completion_chat: true}, archived: false},
      {id: "card-without-capabilities", object: "model", type: "base"},
      {id: "mistral-large-2512", object: "model", type: "base", capabilities: mistralChatCapabilities},
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

function qwenTextModel(model: string, overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    model, name: model.toUpperCase(), provider: "qwen", capabilities: ["TG"], features: [],
    inference_metadata: {request_modality: ["Text"], response_modality: ["Text"]},
    model_info: {context_window: 1_000_000},
    ...overrides,
  };
}

function qwenPage(models: Record<string, unknown>[], pageNumber: number, total: number): Record<string, unknown> {
  return {code: null, message: null, success: true, output: {total, page_no: pageNumber, page_size: 100, models}, request_id: "req"};
}

describe("projectQwenModelList", () => {
  it("shows Qwen text models and hides third-party, realtime, and non-text models", () => {
    const listing = projectQwenModelList(qwenPage([
      qwenTextModel("qwen3.7-plus", {name: "Qwen3.7-Plus", capabilities: ["TG", "Reasoning"], features: ["structured-outputs"]}),
      qwenTextModel("deepseek-v4-pro", {provider: "deepseek"}),
      qwenTextModel("qwen3.8-omni-flash-realtime", {capabilities: ["Realtime-Omni"]}),
      qwenTextModel("qwen-image-3.0-pro", {inference_metadata: {request_modality: ["Text"], response_modality: ["Image"]}}),
      qwenTextModel("qwen3-tts-flash", {inference_metadata: {}}),
      qwenTextModel("qwen-plus", {inference_offline_info: {offline_time: "2027-01-15"}, model_info: {context_window: null}}),
      qwenTextModel("qwen3.7-plus"),
      {name: "missing model ID", provider: "qwen"},
    ], 1, 8), qwenPage([
      qwenTextModel("qwen3.7-plus", {capabilities: ["TG", "Reasoning"]}),
      qwenTextModel("qwen3.8-2.4t-a95b", {name: "Qwen3.8-2.4T-A95B", capabilities: ["Reasoning"]}),
      qwenTextModel("deepseek-r1", {provider: "deepseek", capabilities: ["Reasoning"]}),
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
      expect(capability).toBe(llmModelsListCapability);
      if (commandId === "listQwenReasoningModels") {
        return qwenPage([qwenTextModel("qwen3.7-plus", {capabilities: ["TG", "Reasoning"]}), qwenTextModel("qwq-plus", {capabilities: ["Reasoning"]})], 1, 2);
      }
      return qwenPage([qwenTextModel("qwen3.7-plus"), qwenTextModel("qwen3.7-flash")], 1, 2);
    });
    const listing = await loadQwenModels(client, qwenSingaporeModelLists);
    expect(calls).toEqual(["listQwenModels:1", "listQwenReasoningModels:1"]);
    expect(listing.models.map((model) => model.id)).toEqual(["qwen3.7-plus", "qwen3.7-flash", "qwq-plus"]);
    expect(listing.isTruncated).toBe(false);
  });

  it("reads every page of the China (Hong Kong) lists for the hong-kong region", async () => {
    const calls: string[] = [];
    const client = fakeClient(async (commandId, _capability, parameters) => {
      calls.push(`${commandId}:${parameters?.pageNumber}`);
      const pageNumber = Number(parameters?.pageNumber);
      if (commandId === "listQwenReasoningModelsHongKong") return qwenPage([qwenTextModel("qwq-plus", {capabilities: ["Reasoning"]})], pageNumber, 1);
      return qwenPage([qwenTextModel(`qwen-page-${pageNumber}-a`), qwenTextModel(`qwen-page-${pageNumber}-b`)], pageNumber, 150);
    });
    const listing = await loadQwenModels(client, qwenHongKongModelLists);
    expect(calls).toEqual(["listQwenModelsHongKong:1", "listQwenModelsHongKong:2", "listQwenReasoningModelsHongKong:1"]);
    expect(listing.models.map((model) => model.id)).toEqual(["qwen-page-1-a", "qwen-page-1-b", "qwen-page-2-a", "qwen-page-2-b", "qwq-plus"]);
    expect(listing.isTruncated).toBe(false);
  });

  it("reports a later page's own error", async () => {
    const client = fakeClient(async (_commandId, _capability, parameters) => {
      const pageNumber = Number(parameters?.pageNumber);
      if (pageNumber === 2) throw new Error("Singapore page 2 is unavailable");
      return qwenPage([qwenTextModel(`qwen-page-${pageNumber}`)], pageNumber, 150);
    });
    await expect(loadQwenModels(client, qwenSingaporeModelLists)).rejects.toThrow("Singapore page 2 is unavailable");
  });

  it("reads later pages and reports truncation at the page limit", async () => {
    const client = fakeClient(async (commandId, _capability, parameters) => {
      const pageNumber = Number(parameters?.pageNumber);
      if (commandId === "listQwenReasoningModels") return qwenPage([qwenTextModel("qwq-plus", {capabilities: ["Reasoning"]})], pageNumber, 1);
      return {output: {total: 1000, page_no: pageNumber, page_size: 100, models: [qwenTextModel(`qwen-page-${pageNumber}`)]}};
    });
    const listing = await loadQwenModels(client, qwenSingaporeModelLists);
    expect(listing.models.map((model) => model.id)).toEqual(["qwen-page-1", "qwen-page-2", "qwen-page-3", "qwen-page-4", "qwen-page-5", "qwq-plus"]);
    expect(listing.isTruncated).toBe(true);
  });
});

const xaiLanguageModels = {models: [
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

describe("projectXAIModelList", () => {
  it("shows text models in xAI's order with aliases and reasoning efforts, and hides multi-agent and non-text models", () => {
    const listing = projectXAIModelList(xaiLanguageModels);
    expect(listing.models.map((model) => [model.id, model.label, model.detail, model.isHiddenByDefault])).toEqual([
      ["grok-4.7", undefined, "Reasoning effort: low, medium, high, xhigh; default high", false],
      ["grok-4.3", "grok-4.3 (grok-4.3-latest)", "Reasoning effort: none, low, medium, high, xhigh; default low", false],
      ["grok-4.20-0309-reasoning", "grok-4.20-0309-reasoning (grok-4.20, grok-4.20-reasoning, grok-4.20-reasoning-latest)", undefined, false],
      ["grok-4.20-multi-agent-0309", "grok-4.20-multi-agent-0309 (grok-4.20-multi-agent)", "Multi-agent models do not serve Chat Completions.", true],
      ["grok-future-image-model", undefined, "Does not output text.", true],
    ]);
  });

  it("finds a model by one of its aliases", () => {
    const models = projectXAIModelList(xaiLanguageModels).models;
    expect(filterModelOptions(models, "grok-4.20-reasoning-latest", false).map((model) => model.id)).toEqual(["grok-4.20-0309-reasoning"]);
    expect(filterModelOptions(models, "grok-4.3-latest", false).map((model) => model.id)).toEqual(["grok-4.3"]);
  });

  it("reads the US regional data list and hides IDs that name a non-chat family", () => {
    const listing = projectXAIModelList({object: "list", data: [
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
    expect(projectXAIModelList({error: "forbidden"}).models).toEqual([]);
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
