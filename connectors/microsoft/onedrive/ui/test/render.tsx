// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { renderToStaticMarkup } from "react-dom/server";
import { OneDriveConfigurationUnit, type OneDriveUnitProps } from "../src/units.js";

export function unitTarget(unitId: string, overrides: Partial<ConnectorStudioConfigurationUnitTarget> = {}): ConnectorStudioConfigurationUnitTarget {
  return {
    kind: "configurationUnit",
    scope: {kind: "operation", operationId: "createFolder", flowType: "OneDriveTextCopy", stepType: "EnsureCopyFolder"},
    instanceId: "destination", unitId, label: "Destination", required: false,
    description: "Choose where the copy folder is ensured; leave blank to use your own OneDrive root.",
    bindings: [], value: {}, ...overrides,
  };
}

export function renderUnit(props: Partial<OneDriveUnitProps> & {target: ConnectorStudioConfigurationUnitTarget}): string {
  return renderToStaticMarkup(<OneDriveConfigurationUnit
    onListDrives={() => undefined} onListFolders={() => undefined} onSave={() => undefined} onSearchSites={() => undefined} {...props}/>);
}
