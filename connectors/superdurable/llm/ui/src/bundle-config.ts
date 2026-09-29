// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ModelPickerBundleConfig } from "@superdurable/dex-connectors-react";

import { llmDefaultModelDescription, loadLLMModels, validateProviderQualifiedModel } from "./model-selection.js";

/**
 * llmModelPickerBundleConfig is the llm Studio bundle: the modelPicker unit on
 * each Step and on the connection's model field, listing only the providers the
 * connection adds.
 */
export const llmModelPickerBundleConfig: ModelPickerBundleConfig = {
  connectorId: "llm", providerName: "LLM", iconUrl: "./icon.svg",
  manualModelPlaceholder: "provider/model-id", validateManualModel: validateProviderQualifiedModel,
  loadModels: loadLLMModels, defaultModelDescription: llmDefaultModelDescription,
};
