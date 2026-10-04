// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  openAICompatibleModelOptions,
  type ConnectorStudioClient,
  type ModelListing,
} from "@superdurable/dex-connectors-react";

import { llmModelsListCapability } from "./models-list-capability.js";

/**
 * loadMistralModels lists Mistral models with the listMistralModels command and
 * projects them for ModelPicker. The command reads the global catalog, which
 * also lists the models of the eu and us regions.
 */
export async function loadMistralModels(client: ConnectorStudioClient): Promise<ModelListing> {
  return projectMistralModelList(await client.executeProviderCommand("listMistralModels", llmModelsListCapability));
}

/**
 * projectMistralModelList reads Mistral's `{object: "list", data: [...]}` model
 * cards in the order Mistral returns them. A card without
 * `capabilities.completion_chat`, such as an embedding, OCR, or moderation
 * model, or an archived fine-tuned model, stays behind "Show all models".
 * Deprecated models stay visible with a badge and their deprecation date,
 * because they serve requests until retirement.
 */
export function projectMistralModelList(value: Record<string, unknown>): ModelListing {
  return {models: openAICompatibleModelOptions(value, {
    isHiddenByDefault: (_id, item) => readCapability(item, "completion_chat") !== true || item.archived === true,
    detail: deprecationDetail,
    badges: (item) => [
      ...(isDeprecated(item) ? ["deprecated"] : []),
      ...(readCapability(item, "function_calling") === true ? ["function calling"] : []),
      ...(readCapability(item, "vision") === true ? ["vision"] : []),
    ],
  })};
}

function readCapability(item: Record<string, unknown>, name: string): unknown {
  const capabilities = item.capabilities;
  if (typeof capabilities !== "object" || capabilities === null || Array.isArray(capabilities)) return undefined;
  return (capabilities as Record<string, unknown>)[name];
}

function isDeprecated(item: Record<string, unknown>): boolean {
  return typeof item.deprecation === "string" && item.deprecation.trim() !== "";
}

function deprecationDetail(item: Record<string, unknown>): string | undefined {
  if (!isDeprecated(item)) return undefined;
  const deprecation = String(item.deprecation).trim();
  const date = /^\d{4}-\d{2}-\d{2}/.exec(deprecation)?.[0] ?? deprecation;
  const replacement = typeof item.deprecation_replacement_model === "string" ? item.deprecation_replacement_model.trim() : "";
  return replacement === "" ? `Deprecation date ${date}.` : `Deprecation date ${date}. Replacement: ${replacement}.`;
}
