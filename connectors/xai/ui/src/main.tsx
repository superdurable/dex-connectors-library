// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mountModelPickerBundle } from "@superdurable/dex-connectors-react";
import { loadGrokModels } from "./provider.js";

mountModelPickerBundle({connectorId: "grok", providerName: "xAI", iconUrl: "./icon.svg", loadModels: loadGrokModels});
