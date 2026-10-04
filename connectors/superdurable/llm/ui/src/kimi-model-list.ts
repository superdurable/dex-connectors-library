// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  hasAnyModelIDFragment,
  openAICompatibleModelOptions,
  type ConnectorStudioClient,
  type ModelListing,
} from "@superdurable/dex-connectors-react";

import { llmModelsListCapability } from "./models-list-capability.js";

// Kimi retired these families, and chat requests for them return 404 model not found.
const retiredModelFragments = ["moonshot-v1", "kimi-k2-", "kimi-k2.5", "kimi-latest", "kimi-thinking-preview"] as const;

/**
 * loadKimiModels lists Kimi models from the platform that issued the key:
 * listKimiModels for platform.kimi.ai keys, or listKimiModelsChina for
 * platform.kimi.com keys, which the connection's china region names.
 */
export async function loadKimiModels(client: ConnectorStudioClient, isChinaPlatform: boolean): Promise<ModelListing> {
  const commandId = isChinaPlatform ? "listKimiModelsChina" : "listKimiModels";
  return projectKimiModelList(await client.executeProviderCommand(commandId, llmModelsListCapability));
}

/**
 * projectKimiModelList reads the OpenAI-compatible `{data: [{id}]}` list in the
 * order Kimi returns it. Each model shows its context window and a reasoning
 * badge from Kimi's capability flags; retired families stay behind "Show all
 * models".
 */
export function projectKimiModelList(value: Record<string, unknown>): ModelListing {
  return {models: openAICompatibleModelOptions(value, {
    isHiddenByDefault: (id) => hasAnyModelIDFragment(id, retiredModelFragments),
    detail: (item) => describeContextLength(item.context_length),
    badges: (item) => item.supports_reasoning === true ? ["reasoning"] : [],
  })};
}

/** describeContextLength renders a positive token count such as 262144 as "256K-token context". */
function describeContextLength(contextLength: unknown): string | undefined {
  if (typeof contextLength !== "number" || !Number.isSafeInteger(contextLength) || contextLength <= 0) return undefined;
  const mebi = 1024 * 1024;
  if (contextLength % mebi === 0) return `${contextLength / mebi}M-token context`;
  if (contextLength % 1024 === 0) return `${contextLength / 1024}K-token context`;
  return `${contextLength}-token context`;
}
