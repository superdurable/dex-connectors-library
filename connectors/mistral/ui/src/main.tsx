// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mountModelPickerBundle } from "@superdurable/dex-connectors-react";
import { loadMistralModels } from "./provider.js";

mountModelPickerBundle({connectorId: "mistral", providerName: "Mistral AI", iconUrl: "./icon.svg", loadModels: loadMistralModels});
