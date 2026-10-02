// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StudioNotice, type ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { DrivePickerUnit } from "./drive-picker.js";
import { FolderPickerUnit } from "./folder-picker.js";
import type { GraphDrive, GraphFolder, SharePointSite } from "./provider.js";
import { SitePickerUnit } from "./site-picker.js";
import type { PickedList } from "./unit-frame.js";

export interface OneDriveUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  sites?: PickedList<SharePointSite>;
  drives?: PickedList<GraphDrive>;
  folders?: PickedList<GraphFolder>;
  busy?: boolean;
  loadError?: string;
  onSearchSites(query: string): void;
  onListDrives(siteId: string): void;
  onListFolders(driveId: string, parentFolderId: string): void;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function OneDriveConfigurationUnit(props: OneDriveUnitProps) {
  const {target} = props;
  if (target.unitId === "sitePicker") return <SitePickerUnit {...props}/>;
  if (target.unitId === "drivePicker") return <DrivePickerUnit {...props}/>;
  if (target.unitId === "folderPicker") return <FolderPickerUnit {...props}/>;
  return <StudioNotice tone="error">Unsupported OneDrive configuration unit: {target.unitId}</StudioNotice>;
}
