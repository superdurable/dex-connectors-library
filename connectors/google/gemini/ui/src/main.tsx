// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mountModelPickerBundle } from "@superdurable/dex-connectors-react";
import { loadGeminiModels } from "./provider.js";

mountModelPickerBundle({connectorId: "gemini", providerName: "Gemini", iconUrl: "./icon.svg", loadModels: loadGeminiModels});
