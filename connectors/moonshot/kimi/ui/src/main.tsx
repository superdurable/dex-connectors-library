// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mountModelPickerBundle } from "@superdurable/dex-connectors-react";
import { loadKimiModels } from "./provider.js";

mountModelPickerBundle({connectorId: "kimi", providerName: "Kimi", iconUrl: "./icon.svg", loadModels: loadKimiModels});
