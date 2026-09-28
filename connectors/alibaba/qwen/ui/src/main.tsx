// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mountModelPickerBundle } from "@superdurable/dex-connectors-react";
import { loadQwenModels } from "./provider.js";

mountModelPickerBundle({connectorId: "qwen", providerName: "Alibaba Cloud Model Studio", iconUrl: "./icon.svg", loadModels: loadQwenModels});
