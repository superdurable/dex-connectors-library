// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LLMProvider, LLMRegion } from "./model-selection.js";

/** LLMRegionChoice is one regional API platform a provider serves. */
export interface LLMRegionChoice {
  region: LLMRegion;
  label: string;
}

/** LLMProviderSetup is what the connection setup shows for one provider. */
export interface LLMProviderSetup {
  provider: LLMProvider;
  /** label names the provider in the dropdown and messages. */
  label: string;
  /** defaultModel is the model the connector uses when the connection names none. */
  defaultModel: string;
  /** keyPage is where the API key is created, and keyPagePath how to reach it there. */
  keyPage: string;
  keyPagePath: string;
  /** keyFormat describes the key's shape, such as "an sk-ant- key". */
  keyFormat: string;
  /** apiHost is the only host the key is sent to. */
  apiHost: string;
  /** regions lists the provider's platforms; a provider with one region shows no region field. */
  regions: LLMRegionChoice[];
}

const globalOnly: LLMRegionChoice[] = [{region: "global", label: "Global"}];

/** llmProviderSetups lists the providers in dropdown order, with the facts the connector README documents. */
export const llmProviderSetups: readonly LLMProviderSetup[] = [
  {
    provider: "openai", label: "OpenAI", defaultModel: "gpt-6-sol", keyPage: "https://platform.openai.com/api-keys",
    keyPagePath: "Platform > API keys", keyFormat: "an sk- project key", apiHost: "api.openai.com", regions: globalOnly,
  },
  {
    provider: "anthropic", label: "Claude", defaultModel: "claude-sonnet-5", keyPage: "https://platform.claude.com/settings/keys",
    keyPagePath: "Claude Console > Settings > API keys", keyFormat: "an sk-ant- key", apiHost: "api.anthropic.com", regions: globalOnly,
  },
  {
    provider: "gemini", label: "Gemini", defaultModel: "gemini-3.5-flash-lite", keyPage: "https://aistudio.google.com/api-keys",
    keyPagePath: "Google AI Studio > API keys", keyFormat: "an AIza key", apiHost: "generativelanguage.googleapis.com", regions: globalOnly,
  },
  {
    provider: "qwen", label: "Qwen", defaultModel: "qwen3.7-plus", keyPage: "https://bailian.console.aliyun.com/",
    keyPagePath: "Model Studio > API Key, in the key's region", keyFormat: "an sk- key of the region below",
    apiHost: "the Model Studio host of the region below",
    regions: [
      {region: "global", label: "Global (Singapore)"},
      {region: "hong-kong", label: "China (Hong Kong)"},
      {region: "china", label: "China (Beijing)"},
    ],
  },
  {
    provider: "deepseek", label: "DeepSeek", defaultModel: "deepseek-flash", keyPage: "https://platform.deepseek.com/api_keys",
    keyPagePath: "DeepSeek Platform > API keys", keyFormat: "an sk- key", apiHost: "api.deepseek.com", regions: globalOnly,
  },
  {
    provider: "meta", label: "Meta", defaultModel: "muse-spark-1.3", keyPage: "https://dev.meta.ai/",
    keyPagePath: "Meta Model API dashboard > API keys", keyFormat: "an LLM|<id>|<secret> team key", apiHost: "api.meta.ai",
    regions: globalOnly,
  },
  {
    provider: "mistral", label: "Mistral", defaultModel: "mistral-large-2512", keyPage: "https://console.mistral.ai/api-keys",
    keyPagePath: "Mistral AI Studio > API Keys", keyFormat: "a workspace key", apiHost: "the Mistral API host of the region below",
    regions: [
      {region: "global", label: "Global"},
      {region: "eu", label: "EU and EFTA inference (1.1 times list price)"},
      {region: "us", label: "US inference (1.1 times list price)"},
    ],
  },
  {
    provider: "kimi", label: "Kimi", defaultModel: "kimi-k2.6", keyPage: "https://platform.kimi.ai/",
    keyPagePath: "Kimi Platform > Console > API Keys", keyFormat: "an sk- key from the platform below",
    apiHost: "the Kimi API host of the platform below",
    regions: [
      {region: "global", label: "Global (platform.kimi.ai)"},
      {region: "china", label: "China (platform.kimi.com)"},
    ],
  },
  {
    provider: "xai", label: "xAI", defaultModel: "grok-4.3", keyPage: "https://console.x.ai/",
    keyPagePath: "xAI Console > API Keys", keyFormat: "an xai- team key", apiHost: "the xAI API host of the region below",
    regions: [
      {region: "global", label: "Global"},
      {region: "us", label: "US (serves only grok-4.7 and grok-4.6)"},
    ],
  },
];

/** findLLMProviderSetup returns the setup of provider, or undefined for an unknown or blank value. */
export function findLLMProviderSetup(provider: unknown): LLMProviderSetup | undefined {
  return llmProviderSetups.find((setup) => setup.provider === provider);
}
