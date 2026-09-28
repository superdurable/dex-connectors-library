// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  collectProviderPages,
  openAICompatibleModelOptions,
  type ConnectorStudioClient,
  type ModelListing,
  type ModelOption,
} from "@superdurable/dex-connectors-react";

/** claudeModelsListCapability is the manifest capability of the listModels command. */
export const claudeModelsListCapability = "claude.models-list";

/**
 * loadClaudeModels lists Claude models through the host broker, following
 * `has_more` and `last_id` with the command's `afterId` parameter, and
 * projects them for ModelPicker in the order Claude returns them, newest first.
 */
export async function loadClaudeModels(client: ConnectorStudioClient): Promise<ModelListing> {
  const {items, isTruncated} = await collectProviderPages(async (cursor) => {
    const page = await client.executeProviderCommand(
      "listModels", claudeModelsListCapability, cursor === "" ? {} : {afterId: cursor},
    );
    return {items: projectClaudeModelPage(page), nextCursor: readNextClaudeModelCursor(page)};
  });
  const models: ModelOption[] = [];
  const seenIDs = new Set<string>();
  for (const model of items) {
    if (seenIDs.has(model.id)) continue;
    seenIDs.add(model.id);
    models.push(model);
  }
  return {models, isTruncated};
}

/**
 * projectClaudeModelPage reads one `GET /v1/models` page. Every listed Claude
 * model serves the Messages API, so none is hidden. The label is the
 * `display_name`, the detail is the context window and output limit, and the
 * badges come from `capabilities`.
 */
export function projectClaudeModelPage(value: Record<string, unknown>): ModelOption[] {
  return openAICompatibleModelOptions(value, {
    label: (item) => typeof item.display_name === "string" && item.display_name.trim() !== "" ? item.display_name.trim() : undefined,
    detail: describeTokenLimits,
    badges: readCapabilityBadges,
  });
}

/** readNextClaudeModelCursor returns `last_id` while `has_more` is true, else an empty cursor. */
export function readNextClaudeModelCursor(value: Record<string, unknown>): string {
  return value.has_more === true && typeof value.last_id === "string" ? value.last_id : "";
}

function describeTokenLimits(item: Record<string, unknown>): string | undefined {
  const limits = [
    isPositiveNumber(item.max_input_tokens) ? `${formatTokenCount(item.max_input_tokens)} context` : "",
    isPositiveNumber(item.max_tokens) ? `${formatTokenCount(item.max_tokens)} max output` : "",
  ].filter((limit) => limit !== "");
  return limits.length > 0 ? limits.join(" · ") : undefined;
}

function readCapabilityBadges(item: Record<string, unknown>): string[] {
  const capabilities = isRecord(item.capabilities) ? item.capabilities : {};
  const badges: string[] = [];
  if (isSupported(capabilities.structured_outputs)) badges.push("structured output");
  if (isSupported(capabilities.effort)) badges.push("effort");
  if (isSupported(capabilities.thinking)) badges.push("thinking");
  return badges;
}

function isSupported(capability: unknown): boolean {
  return isRecord(capability) && capability.supported === true;
}

function formatTokenCount(count: number): string {
  if (count >= 1_000_000) return `${Number((count / 1_000_000).toFixed(1))}M`;
  if (count >= 1_000) return `${Math.round(count / 1_000)}K`;
  return String(count);
}

function isPositiveNumber(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value) && value > 0;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
