// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mountModelPickerBundle } from "@superdurable/dex-connectors-react";
import { loadDeepSeekModels } from "./provider.js";

mountModelPickerBundle({connectorId: "deepseek", providerName: "DeepSeek", iconUrl: "./icon.svg", loadModels: loadDeepSeekModels});
