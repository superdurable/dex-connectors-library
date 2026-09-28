// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  validateModelIDForRule,
  type ConnectorStudioClient,
  type ModelIDRule,
  type ModelListing,
  type ModelListingNotice,
  type ModelOption,
} from "@superdurable/dex-connectors-react";
import {
  loadClaudeModelListing,
  loadGeminiModelListing,
  loadOpenAIModelListing,
} from "@superdurable/dex-connectors-react/provider-model-lists";

/** llmModelsListCapability is the manifest capability every llm list command declares. */
export const llmModelsListCapability = "llm.models-list";

/** ProviderPrefix is the provider part of a provider/model selection. */
export type ProviderPrefix = "openai" | "anthropic" | "gemini";

/** ProviderModelSource lists one provider's models for the combined picker. */
export interface ProviderModelSource {
  /** prefix is prepended to every listed model ID, as in anthropic/claude-sonnet-5. */
  prefix: ProviderPrefix;
  /** label names the provider in the option badge and notices, such as "Claude". */
  label: string;
  /** keyName is how the notice names the connection's key for this provider, such as "an Anthropic key". */
  keyName: string;
  /** hostLimitation is an optional sentence about a Dex Web release that cannot list this provider. */
  hostLimitation?: string;
  /** load lists the provider's models through the Studio broker. */
  load(): Promise<ModelListing>;
}

// The llm connector validates each provider's model with that provider connector's model-ID rule.
const providerModelIDRules: Record<ProviderPrefix, ModelIDRule> = {openai: "body", anthropic: "body", gemini: "pathSegment"};

const selectionFormatMessage =
  "Enter provider/model, where provider is openai, anthropic, or gemini, such as anthropic/claude-sonnet-5, or a provider alone for its default model.";

const noProviderListedMessage =
  "No provider's models could be listed. Choose a provider's default model, or enter provider/model-id.";

const providerCommandBanner =
  "Dex Web releases before cli-v0.14.2 also show a 'Connector provider command failed' banner at the top of the page for this list; it does not affect generation.";

/**
 * llmModelSources declares the three provider lists in picker order: OpenAI,
 * Claude, and Gemini. Each loader runs only its own manifest commands, which
 * Dex Web authorizes with only that provider's key field.
 */
export function llmModelSources(client: ConnectorStudioClient): ProviderModelSource[] {
  return [
    {
      prefix: "openai", label: "OpenAI", keyName: "an OpenAI key",
      load: () => loadOpenAIModelListing(client, {capability: llmModelsListCapability, commandId: "listOpenAIModels"}),
    },
    {
      prefix: "anthropic", label: "Claude", keyName: "an Anthropic key",
      hostLimitation: "Dex Web releases before cli-v0.13.10 cannot list Claude models.",
      load: () => loadClaudeModelListing(client, {capability: llmModelsListCapability, commandId: "listAnthropicModels"}),
    },
    {
      prefix: "gemini", label: "Gemini", keyName: "a Gemini key",
      load: () => loadGeminiModelListing(client, {
        capability: llmModelsListCapability,
        nativeCommandId: "listGeminiModels", openAICompatibleCommandId: "listGeminiOpenAICompatibleModels",
      }),
    },
  ];
}

/** loadLLMModels is the bundle's ModelPicker loader: the combined live lists of every provider. */
export function loadLLMModels(client: ConnectorStudioClient): Promise<ModelListing> {
  return combineModelListings(llmModelSources(client));
}

/**
 * combineModelListings loads every source at once and merges the results.
 * The listing starts with one default-model option per source, whose value is
 * the provider alone, so a provider stays selectable when its list fails.
 * Listed models follow in source order with IDs written prefix/id and the
 * provider label as their first badge. Each failed source adds one attention
 * notice, after one overall notice when every source fails. It never rejects,
 * so the default-model options stay selectable when every list fails.
 */
export async function combineModelListings(sources: ProviderModelSource[]): Promise<ModelListing> {
  const settled = await Promise.allSettled(sources.map((source) => source.load()));
  const models: ModelOption[] = sources.map((source) => ({
    id: source.prefix, label: `${source.label} default model`,
    detail: `The model the ${source.label} connector uses when a Step names none.`, badges: [source.label],
  }));
  const notices: ModelListingNotice[] = [];
  if (settled.length > 0 && settled.every((outcome) => outcome.status === "rejected")) {
    notices.push({tone: "attention", message: noProviderListedMessage});
  }
  let isTruncated = false;
  settled.forEach((outcome, index) => {
    const source = sources[index];
    if (outcome.status === "rejected") {
      notices.push({tone: "attention", message: providerListFailureMessage(source)});
      return;
    }
    isTruncated ||= outcome.value.isTruncated === true;
    for (const model of outcome.value.models) {
      models.push({...model, id: `${source.prefix}/${model.id}`, badges: [source.label, ...(model.badges ?? [])]});
    }
  });
  return {models, isTruncated, notices};
}

/**
 * validateProviderQualifiedModel checks a typed model the way the llm
 * connector does: a provider alone, or provider/model whose model passes that
 * provider's model-ID rule. It returns a message for an invalid entry, or
 * undefined to accept it.
 */
export function validateProviderQualifiedModel(value: string): string | undefined {
  const separator = value.indexOf("/");
  const prefix = separator === -1 ? value : value.slice(0, separator);
  if (!isProviderPrefix(prefix)) return selectionFormatMessage;
  if (separator === -1) return undefined;
  const model = value.slice(separator + 1);
  if (model.trim() === "") return selectionFormatMessage;
  const validation = validateModelIDForRule(providerModelIDRules[prefix], model);
  return validation.isValid ? undefined : validation.message;
}

function providerListFailureMessage(source: ProviderModelSource): string {
  return [
    `${source.label} models could not be listed. Add ${source.keyName}, or choose ${source.label} default model, or enter ${source.prefix}/<model-id>.`,
    source.hostLimitation, providerCommandBanner,
  ].filter((sentence) => sentence !== undefined).join(" ");
}

function isProviderPrefix(value: string): value is ProviderPrefix {
  return value === "openai" || value === "anthropic" || value === "gemini";
}
