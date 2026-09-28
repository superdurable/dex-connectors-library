// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ModelOption } from "./model-picker.js";
import type { ConnectorStudioClient } from "./studio-client.js";

/**
 * executeFirstAcceptedProviderCommand runs commandIds in order and returns the
 * first result the host accepts. A connector declares one command per fixed
 * provider host, such as a global and a China platform, because a key works on
 * only one of them; each host stays pinned in the manifest. The last error is
 * rethrown when every command fails.
 */
export async function executeFirstAcceptedProviderCommand(
  client: ConnectorStudioClient,
  capability: string,
  commandIds: string[],
  parameters: Record<string, string> = {},
): Promise<Record<string, unknown>> {
  let lastError: unknown = new Error("No provider command is declared");
  for (const commandId of commandIds) {
    try {
      return await client.executeProviderCommand(commandId, capability, parameters);
    } catch (error) {
      lastError = error;
    }
  }
  throw lastError;
}

/** readProviderModelArray returns value[field] when it is an array of objects, else an empty array. */
export function readProviderModelArray(value: Record<string, unknown>, field = "data"): Record<string, unknown>[] {
  const items = value[field];
  return Array.isArray(items) ? items.filter(isRecord) : [];
}

/**
 * openAICompatibleModelOptions projects an OpenAI-style `{data: [{id}]}` list.
 * isHiddenByDefault marks models the connector's filter judges unsuitable,
 * such as embedding models; label and detail come from optional mappers.
 */
export function openAICompatibleModelOptions(
  value: Record<string, unknown>,
  projection: {
    isHiddenByDefault?(id: string, item: Record<string, unknown>): boolean;
    label?(item: Record<string, unknown>): string | undefined;
    detail?(item: Record<string, unknown>): string | undefined;
    badges?(item: Record<string, unknown>): string[];
  } = {},
): ModelOption[] {
  const seenIDs = new Set<string>();
  return readProviderModelArray(value).flatMap((item) => {
    const id = typeof item.id === "string" ? item.id.trim() : "";
    if (id === "" || seenIDs.has(id)) return [];
    seenIDs.add(id);
    return [{
      id,
      label: projection.label?.(item),
      detail: projection.detail?.(item),
      badges: projection.badges?.(item),
      isHiddenByDefault: projection.isHiddenByDefault?.(id, item) ?? false,
    }];
  });
}

/**
 * hasAnyModelIDFragment reports whether id contains one of fragments, ignoring
 * case. Connectors use it to hide listed models that cannot serve chat, such
 * as "embedding", "tts", or "image", when the provider gives no capability field.
 */
export function hasAnyModelIDFragment(id: string, fragments: readonly string[]): boolean {
  const lowered = id.toLowerCase();
  return fragments.some((fragment) => lowered.includes(fragment));
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
