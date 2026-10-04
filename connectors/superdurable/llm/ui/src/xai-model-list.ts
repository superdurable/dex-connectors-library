// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  hasAnyModelIDFragment,
  openAICompatibleModelOptions,
  readProviderModelArray,
  type ConnectorStudioClient,
  type ModelListing,
  type ModelOption,
} from "@superdurable/dex-connectors-react";

import { llmModelsListCapability } from "./models-list-capability.js";

// The US list has no modalities, so IDs that name a non-chat family are hidden.
const nonChatModelFragments = ["multi-agent", "imagine", "image", "video", "voice", "tts", "transcribe", "embed"] as const;

/**
 * loadXAIModels lists Grok models from the connection's region: the global
 * language-models list with listXAIModels, or the US regional list with
 * listXAIModelsUS, and projects them for ModelPicker.
 */
export async function loadXAIModels(client: ConnectorStudioClient, isUSRegion: boolean): Promise<ModelListing> {
  const commandId = isUSRegion ? "listXAIModelsUS" : "listXAIModels";
  return projectXAIModelList(await client.executeProviderCommand(commandId, llmModelsListCapability));
}

/**
 * projectXAIModelList reads either xAI list in the order xAI returns it: the
 * global `{models: [...]}` language-models list, which carries output
 * modalities, or the US regional `{data: [...]}` list, which does not. A
 * model's aliases join its label, so searching for an alias finds the model,
 * and its reasoning efforts become the detail line. Models that output no
 * text, and multi-agent models, which fail on Chat Completions, stay behind
 * "Show all models".
 */
export function projectXAIModelList(value: Record<string, unknown>): ModelListing {
  if (Array.isArray(value.models)) {
    return {models: projectLanguageModels(readProviderModelArray(value, "models"))};
  }
  return {models: openAICompatibleModelOptions(value, {
    isHiddenByDefault: (id) => hasAnyModelIDFragment(id, nonChatModelFragments),
    label: aliasLabel,
    detail: modelDetail,
  })};
}

function projectLanguageModels(items: Record<string, unknown>[]): ModelOption[] {
  const seenIDs = new Set<string>();
  return items.flatMap((item) => {
    const id = typeof item.id === "string" ? item.id.trim() : "";
    if (id === "" || seenIDs.has(id)) return [];
    seenIDs.add(id);
    const isTextModel = readStrings(item.output_modalities).includes("text");
    return [{
      id,
      label: aliasLabel(item),
      detail: isTextModel ? modelDetail(item) : "Does not output text.",
      isHiddenByDefault: !isTextModel || isMultiAgentModel(id),
    }];
  });
}

/** aliasLabel names the model with its aliases, or returns undefined when it has none. */
function aliasLabel(item: Record<string, unknown>): string | undefined {
  const id = typeof item.id === "string" ? item.id.trim() : "";
  const aliases = [...new Set(readStrings(item.aliases))].filter((alias) => alias !== id);
  return aliases.length > 0 ? `${id} (${aliases.join(", ")})` : undefined;
}

function modelDetail(item: Record<string, unknown>): string | undefined {
  const id = typeof item.id === "string" ? item.id : "";
  if (isMultiAgentModel(id)) return "Multi-agent models do not serve Chat Completions.";
  const capabilities = isRecord(item.capabilities) ? item.capabilities : {};
  const efforts = readStrings(capabilities.reasoning_effort);
  if (efforts.length === 0) return undefined;
  const defaultEffort = typeof capabilities.default_reasoning_effort === "string" ? capabilities.default_reasoning_effort : "";
  return `Reasoning effort: ${efforts.join(", ")}${defaultEffort === "" ? "" : `; default ${defaultEffort}`}`;
}

function isMultiAgentModel(id: string): boolean {
  return hasAnyModelIDFragment(id, ["multi-agent"]);
}

function readStrings(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((entry) => typeof entry === "string" && entry.trim() !== "" ? [entry.trim()] : []);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
