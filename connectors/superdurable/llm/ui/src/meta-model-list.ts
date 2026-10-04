// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  openAICompatibleModelOptions,
  type ConnectorStudioClient,
  type ModelListing,
} from "@superdurable/dex-connectors-react";

import { llmModelsListCapability } from "./models-list-capability.js";

/** loadMetaModels lists Meta models with the listMetaModels command and projects them for ModelPicker. */
export async function loadMetaModels(client: ConnectorStudioClient): Promise<ModelListing> {
  return projectMetaModelList(await client.executeProviderCommand("listMetaModels", llmModelsListCapability));
}

/**
 * projectMetaModelList reads the OpenAI-compatible `{data: [{id}]}` list in
 * the order Meta returns it. Only Muse Spark models serve Chat Completions, so
 * Muse Image, Muse Voice Transcribe, and SAM stay behind "Show all models".
 * Contributor variants are flagged because Meta may train on their prompts and
 * completions.
 */
export function projectMetaModelList(value: Record<string, unknown>): ModelListing {
  return {models: openAICompatibleModelOptions(value, {
    isHiddenByDefault: (id) => !id.startsWith("muse-spark-"),
    detail: (item) => typeof item.id === "string" && item.id.endsWith("-contributor")
      ? "Contributor tier: Meta may train on prompts and completions."
      : undefined,
  })};
}
