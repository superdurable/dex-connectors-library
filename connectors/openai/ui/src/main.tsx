// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mountModelPickerBundle } from "@superdurable/dex-connectors-react";
import { loadOpenAIModels } from "./provider.js";

mountModelPickerBundle({connectorId: "openai", providerName: "OpenAI", iconUrl: "./icon.svg", loadModels: loadOpenAIModels});
