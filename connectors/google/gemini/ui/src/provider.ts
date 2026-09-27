// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  hasAnyModelIDFragment,
  openAICompatibleModelOptions,
  type ConnectorStudioClient,
  type ModelListing,
} from "@superdurable/dex-connectors-react";

/** geminiModelsListCapability is the manifest capability of the listModels command. */
export const geminiModelsListCapability = "gemini.models-list";

// The OpenAI-compatible list has no supportedGenerationMethods, so model IDs
// that name a non-text family are hidden behind "Show all models".
const nonTextModelFragments = ["embedding", "imagen", "veo", "aqa", "tts", "image", "audio", "live", "lyria", "robotics"] as const;

/** loadGeminiModels lists Gemini models through the host broker and projects them for ModelPicker. */
export async function loadGeminiModels(client: ConnectorStudioClient): Promise<ModelListing> {
  return projectGeminiModelList(await client.executeProviderCommand("listModels", geminiModelsListCapability));
}

/**
 * projectGeminiModelList reads the OpenAI-compatible `{data: [{id: "models/…"}]}`
 * list, strips the "models/" prefix the connector accepts either way, and
 * hides non-text models.
 */
export function projectGeminiModelList(value: Record<string, unknown>): ModelListing {
  const models = openAICompatibleModelOptions(value, {
    isHiddenByDefault: (id) => hasAnyModelIDFragment(id, nonTextModelFragments),
  }).map((model) => ({...model, id: model.id.replace(/^models\//, "")}));
  return {models: models.filter((model, index) => model.id !== "" && models.findIndex((other) => other.id === model.id) === index)};
}
