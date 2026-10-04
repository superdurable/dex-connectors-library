// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  collectProviderPages,
  hasAnyModelIDFragment,
  readProviderModelArray,
  type ConnectorStudioClient,
  type ModelListing,
  type ModelOption,
} from "@superdurable/dex-connectors-react";

import { llmModelsListCapability } from "./models-list-capability.js";

/**
 * QwenModelListRegion names one DashScope host's two model-list commands:
 * the `capabilities=TG` list and the `capabilities=Reasoning` list.
 */
export interface QwenModelListRegion {
  /** textGenerationCommandId lists models with the TG capability. */
  textGenerationCommandId: string;
  /** reasoningCommandId lists models with the Reasoning capability, including thinking-only models. */
  reasoningCommandId: string;
}

/** qwenSingaporeModelLists are the commands of the global region, Model Studio Singapore. */
export const qwenSingaporeModelLists: QwenModelListRegion = {
  textGenerationCommandId: "listQwenModels", reasoningCommandId: "listQwenReasoningModels",
};

/** qwenHongKongModelLists are the commands of the hong-kong region, Model Studio China (Hong Kong). */
export const qwenHongKongModelLists: QwenModelListRegion = {
  textGenerationCommandId: "listQwenModelsHongKong", reasoningCommandId: "listQwenReasoningModelsHongKong",
};

// The manifest's page_size is 100, so five pages list up to 500 models per list before isTruncated.
const maxModelListPages = 5;

// Fallback for entries without modality metadata: these families do not serve Chat Completions.
const nonChatModelFragments = ["realtime", "livetranslate", "tts", "asr", "embedding", "rerank"] as const;

/**
 * loadQwenModels lists Model Studio text generation and reasoning models from
 * the connection's region, page by page, and projects them for ModelPicker. It
 * rethrows the first failed page's error.
 */
export async function loadQwenModels(client: ConnectorStudioClient, region: QwenModelListRegion): Promise<ModelListing> {
  const textGenerationPages = await collectModelListPages(
    (pageNumber) => executeModelListPage(client, region.textGenerationCommandId, pageNumber),
  );
  const reasoningPages = await collectModelListPages(
    (pageNumber) => executeModelListPage(client, region.reasoningCommandId, pageNumber),
  );
  const listing = projectQwenModelList(...textGenerationPages.items, ...reasoningPages.items);
  return {models: listing.models, isTruncated: textGenerationPages.isTruncated || reasoningPages.isTruncated};
}

function collectModelListPages(
  fetchModelListPage: (pageNumber: number) => Promise<Record<string, unknown>>,
): Promise<{items: Record<string, unknown>[]; isTruncated: boolean}> {
  return collectProviderPages(async (cursor) => {
    const pageNumber = cursor === "" ? 1 : Number(cursor);
    const page = await fetchModelListPage(pageNumber);
    return {items: [page], nextCursor: hasNextModelPage(page, pageNumber) ? String(pageNumber + 1) : ""};
  }, maxModelListPages);
}

function executeModelListPage(client: ConnectorStudioClient, commandId: string, pageNumber: number): Promise<Record<string, unknown>> {
  return client.executeProviderCommand(commandId, llmModelsListCapability, {pageNumber: String(pageNumber)});
}

/**
 * projectQwenModelList reads the native `GET /api/v1/models` pages
 * (`{output: {models: [{model, name, provider, ...}]}}`) in the order given,
 * keeping the first entry for a model ID that several lists return. Qwen
 * models with text input and output are shown; third-party models, models
 * without text input and output, and realtime models stay behind "Show all
 * models". A model with a scheduled offline time names it in its detail line.
 */
export function projectQwenModelList(...pages: Record<string, unknown>[]): ModelListing {
  const seenIDs = new Set<string>();
  const models: ModelOption[] = [];
  for (const page of pages) {
    for (const item of readProviderModelArray(readRecord(page.output), "models")) {
      const id = typeof item.model === "string" ? item.model.trim() : "";
      if (id === "" || seenIDs.has(id)) continue;
      seenIDs.add(id);
      models.push({
        id,
        label: typeof item.name === "string" && item.name.trim() !== "" ? item.name.trim() : undefined,
        detail: modelDetail(item),
        badges: modelBadges(item),
        isHiddenByDefault: !isQwenChatModel(id, item),
      });
    }
  }
  return {models};
}

function hasNextModelPage(page: Record<string, unknown>, pageNumber: number): boolean {
  const output = readRecord(page.output);
  const total = typeof output.total === "number" ? output.total : 0;
  const pageSize = typeof output.page_size === "number" && output.page_size > 0 ? output.page_size : 0;
  return pageSize > 0 && readProviderModelArray(output, "models").length > 0 && pageNumber * pageSize < total;
}

function isQwenChatModel(id: string, item: Record<string, unknown>): boolean {
  if (item.provider !== "qwen" || hasAnyModelIDFragment(id, nonChatModelFragments)) return false;
  if (readStrings(item.capabilities).some((capability) => capability.startsWith("Realtime"))) return false;
  const metadata = readRecord(item.inference_metadata);
  const requestModalities = readStrings(metadata.request_modality);
  const responseModalities = readStrings(metadata.response_modality);
  return (requestModalities.length === 0 || requestModalities.includes("Text"))
    && (responseModalities.length === 0 || responseModalities.includes("Text"));
}

function modelDetail(item: Record<string, unknown>): string | undefined {
  const offlineTime = readRecord(item.inference_offline_info).offline_time;
  if (typeof offlineTime === "string" && offlineTime.trim() !== "") return `Scheduled to go offline ${offlineTime.trim()}.`;
  const contextWindow = readRecord(item.model_info).context_window;
  return typeof contextWindow === "number" && contextWindow > 0
    ? `${contextWindow.toLocaleString("en-US")}-token context`
    : undefined;
}

function modelBadges(item: Record<string, unknown>): string[] {
  const badges: string[] = [];
  if (readStrings(item.capabilities).includes("Reasoning")) badges.push("reasoning");
  if (readStrings(item.features).includes("structured-outputs")) badges.push("structured output");
  return badges;
}

function readRecord(value: unknown): Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value) ? value as Record<string, unknown> : {};
}

function readStrings(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : [];
}
