// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mountModelPickerBundle } from "@superdurable/dex-connectors-react";
import { loadClaudeModels } from "./provider.js";

mountModelPickerBundle({connectorId: "claude", providerName: "Claude", iconUrl: "./icon.svg", loadModels: loadClaudeModels});
