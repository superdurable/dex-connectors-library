// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Entry point of the "@superdurable/dex-connectors-react/provider-model-lists" subpath export.

/**
 * ProviderModelListCommand names one manifest-declared Studio provider
 * command that lists a provider's models, and the capability the command,
 * the unit, and `studio.setup` declare. Each connector passes its own IDs, so
 * one projection serves every connector that declares the same provider
 * request.
 */
export interface ProviderModelListCommand {
  /** capability is the command's manifest capability, such as "openai.models-list". */
  capability: string;
  /** commandId is the command's manifest ID, such as "listModels". */
  commandId: string;
}

/**
 * GeminiModelListCommands names the two Gemini model list commands a
 * connector declares under one capability: the native models.list, whose key
 * travels in the x-goog-api-key header, and the OpenAI-compatible list, whose
 * key is a bearer token.
 */
export interface GeminiModelListCommands {
  /** capability is the manifest capability both commands declare, such as "gemini.models-list". */
  capability: string;
  /** nativeCommandId is the GET /v1beta/models command, which declares a pageToken query parameter. */
  nativeCommandId: string;
  /** openAICompatibleCommandId is the GET /v1beta/openai/models command. */
  openAICompatibleCommandId: string;
}

export { loadClaudeModelListing, projectClaudeModelPage, readNextClaudeModelCursor } from "./claude-model-list.js";
export { loadGeminiModelListing, projectNativeGeminiModels, projectOpenAICompatibleGeminiModelList } from "./gemini-model-list.js";
export { loadOpenAIModelListing, projectOpenAIModelList } from "./openai-model-list.js";
