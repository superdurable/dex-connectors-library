// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mountModelPickerBundle } from "@superdurable/dex-connectors-react";
import { loadMetaModels } from "./provider.js";

mountModelPickerBundle({connectorId: "meta", providerName: "Meta Model API", iconUrl: "./icon.svg", loadModels: loadMetaModels});
