// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  validateModelIDForRule,
  type ConnectorStudioClient,
  type ConnectorStudioConnection,
  type ModelListing,
} from "@superdurable/dex-connectors-react";
import {
  loadClaudeModelListing,
  loadGeminiModelListing,
  loadOpenAIModelListing,
} from "@superdurable/dex-connectors-react/provider-model-lists";

import { loadDeepSeekModels } from "./deepseek-model-list.js";
import { loadKimiModels } from "./kimi-model-list.js";
import { loadMetaModels } from "./meta-model-list.js";
import { loadMistralModels } from "./mistral-model-list.js";
import { llmModelsListCapability } from "./models-list-capability.js";
import { loadQwenModels, qwenHongKongModelLists, qwenSingaporeModelLists } from "./qwen-model-list.js";
import { loadXAIModels } from "./xai-model-list.js";

/** LLMProvider is one value of the connection's provider field. */
export type LLMProvider = "openai" | "anthropic" | "gemini" | "qwen" | "deepseek" | "meta" | "mistral" | "kimi" | "xai";

/** LLMRegion is one value of the connection's region field; a blank region is global. */
export type LLMRegion = "global" | "us" | "eu" | "china" | "hong-kong";

/**
 * llmDefaultModelDescription names the model the llm connector runs when
 * neither a Step nor the connection picks one.
 */
export const llmDefaultModelDescription = "the provider's default model";

/** LLMModelSource lists one provider's models for the picker. */
export interface LLMModelSource {
  /** label names the provider in messages, such as "Claude". */
  label: string;
  /** load lists the provider's models in region through the Studio broker. */
  load(client: ConnectorStudioClient, region: LLMRegion): Promise<ModelListing>;
}

const llmProviders: readonly LLMProvider[] = ["openai", "anthropic", "gemini", "qwen", "deepseek", "meta", "mistral", "kimi", "xai"];
const llmRegions: readonly LLMRegion[] = ["global", "us", "eu", "china", "hong-kong"];

const unsavedProviderMessage =
  "Save the connection with its provider and api_key first; the picker then lists that provider's models. Until then, enter the provider's model ID.";

const unreportedConfigurationMessage =
  "This Dex Web release does not tell the picker the connection's provider, so it lists no models. Enter a model ID of the connection's provider.";

const qwenChinaMessage =
  "Model Studio China (Beijing) has no model list here. Enter the model ID from the Model Studio console, such as qwen3.7-plus.";

/**
 * llmModelSources declares each provider's list. Each loader runs only its own
 * provider's manifest commands for the connection's region, so Dex Web sends
 * api_key only to that provider's hosts.
 */
export const llmModelSources: Record<LLMProvider, LLMModelSource> = {
  openai: {
    label: "OpenAI",
    load: (client) => loadOpenAIModelListing(client, {capability: llmModelsListCapability, commandId: "listOpenAIModels"}),
  },
  anthropic: {
    label: "Claude",
    load: (client) => loadClaudeModelListing(client, {capability: llmModelsListCapability, commandId: "listAnthropicModels"}),
  },
  gemini: {
    label: "Gemini",
    load: (client) => loadGeminiModelListing(client, {
      capability: llmModelsListCapability,
      nativeCommandId: "listGeminiModels", openAICompatibleCommandId: "listGeminiOpenAICompatibleModels",
    }),
  },
  qwen: {
    label: "Qwen",
    load: (client, region) => region === "china"
      ? Promise.resolve({models: [], notices: [{tone: "info", message: qwenChinaMessage}]})
      : loadQwenModels(client, region === "hong-kong" ? qwenHongKongModelLists : qwenSingaporeModelLists),
  },
  deepseek: {label: "DeepSeek", load: (client) => loadDeepSeekModels(client)},
  meta: {label: "Meta", load: (client) => loadMetaModels(client)},
  mistral: {label: "Mistral", load: (client) => loadMistralModels(client)},
  kimi: {label: "Kimi", load: (client, region) => loadKimiModels(client, region === "china")},
  xai: {label: "xAI", load: (client, region) => loadXAIModels(client, region === "us")},
};

/**
 * loadLLMModels is the bundle's ModelPicker loader: the live list of the
 * connection's saved provider in its saved region. Without a saved provider
 * it runs no command, so the key never reaches a provider the connection does
 * not name, and resolves with a notice; the picker still offers the default
 * option and model ID entry. A failed list rejects with a message that names
 * the provider, so ModelPicker offers retry and model ID entry.
 */
export async function loadLLMModels(
  client: ConnectorStudioClient, connection: Pick<ConnectorStudioConnection, "configuration" | "isConfigurationReported">,
): Promise<ModelListing> {
  const provider = readConfiguredProvider(connection.configuration);
  if (provider === undefined) {
    const message = connection.isConfigurationReported ? unsavedProviderMessage : unreportedConfigurationMessage;
    return {models: [], notices: [{tone: "attention", message}]};
  }
  const source = llmModelSources[provider];
  try {
    return await source.load(client, readConfiguredRegion(connection.configuration));
  } catch (error) {
    const reason = error instanceof Error ? error.message : "Unknown error";
    throw new Error(`The ${source.label} model list failed (${reason}). Check the connection's api_key and region.`);
  }
}

/**
 * validateLLMModelID checks a typed model ID the way the llm connector checks
 * a request model for most providers: 1 to 256 printable ASCII characters
 * without spaces. It returns a message for an invalid entry, or undefined to
 * accept it. Gemini's stricter path-segment rule is applied when the Worker
 * calls the model.
 */
export function validateLLMModelID(model: string): string | undefined {
  const validation = validateModelIDForRule("body", model);
  return validation.isValid ? undefined : validation.message;
}

function readConfiguredProvider(configuration: Record<string, unknown>): LLMProvider | undefined {
  const provider = configuration.provider;
  return llmProviders.find((candidate) => candidate === provider);
}

function readConfiguredRegion(configuration: Record<string, unknown>): LLMRegion {
  const region = configuration.region;
  return llmRegions.find((candidate) => candidate === region) ?? "global";
}
