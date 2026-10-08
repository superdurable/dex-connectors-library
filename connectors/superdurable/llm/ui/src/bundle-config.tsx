// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ModelPickerBundleConfig } from "@superdurable/dex-connectors-react";

import { LLMConnectionSetup } from "./connection-setup.js";
import { llmDefaultModelDescription, loadLLMModels, validateLLMModelID } from "./model-selection.js";

/**
 * llmModelPickerBundleConfig is the llm Studio bundle: the whole connection
 * setup where the host grants connection.write, and the modelPicker unit on
 * each Step and on the connection's model field, listing the live models of
 * the connection's provider.
 */
export const llmModelPickerBundleConfig: ModelPickerBundleConfig = {
  connectorId: "llm", providerName: "LLM", iconUrl: "./icon.svg",
  manualModelPlaceholder: "model-id", validateManualModel: validateLLMModelID,
  loadModels: loadLLMModels, defaultModelDescription: llmDefaultModelDescription,
  renderConnectionSetup: (props) => <LLMConnectionSetup {...props}/>,
};
