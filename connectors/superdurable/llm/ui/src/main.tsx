// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mountModelPickerBundle } from "@superdurable/dex-connectors-react";
import { loadLLMModels, validateProviderQualifiedModel } from "./model-selection.js";

mountModelPickerBundle({
  connectorId: "llm", providerName: "LLM", iconUrl: "./icon.svg",
  manualModelPlaceholder: "provider/model-id", validateManualModel: validateProviderQualifiedModel,
  loadModels: loadLLMModels,
});
