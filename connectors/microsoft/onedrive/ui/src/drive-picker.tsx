// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StudioButton, StudioField, StudioNotice, type ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { useState } from "react";
import { isGraphID, type GraphDrive } from "./provider.js";
import { SaveAction, TruncatedNotice, UnitFrame, stringValue, type PickedList } from "./unit-frame.js";

export interface DrivePickerProps {
  target: ConnectorStudioConfigurationUnitTarget;
  drives?: PickedList<GraphDrive>;
  busy?: boolean;
  loadError?: string;
  /** onListDrives lists the site's document libraries, or the signed-in user's OneDrive for a blank site. */
  onListDrives(siteId: string): void;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function DrivePickerUnit({target, drives, busy, loadError, onListDrives, onSave}: DrivePickerProps) {
  const siteId = stringValue(target.value.siteId);
  const [driveId, setDriveId] = useState(stringValue(target.value.driveId));
  const [driveName, setDriveName] = useState(stringValue(target.value.driveName));
  const listed = drives?.items ?? [];
  const isDriveIDInvalid = driveId.trim() !== "" && !isGraphID(driveId);
  const editDriveID = (value: string) => {
    setDriveId(value);
    setDriveName(listed.find((drive) => drive.id === value.trim())?.name ?? "");
  };
  return <UnitFrame loadError={loadError} target={target}>
    <div className="studio-actions">
      <StudioButton disabled={busy} onClick={() => onListDrives(siteId)}>{siteId ? "List document libraries" : "Use my OneDrive"}</StudioButton>
      {driveName && <span className="studio-muted"><strong>Drive:</strong> {driveName}</span>}
    </div>
    {!siteId && <span className="studio-muted">No site is saved for this Step, so the list holds your own OneDrive. Save a site first to list its libraries.</span>}
    {listed.length > 0 && <StudioField label="Drive">
      <select onChange={(event) => editDriveID(event.target.value)} value={driveId.trim()}>
        <option value="">My OneDrive (signed-in user)</option>
        {listed.map((drive) => <option key={drive.id} value={drive.id}>{drive.name}</option>)}
      </select>
    </StudioField>}
    <TruncatedNotice isTruncated={drives?.isTruncated ?? false} noun="libraries"/>
    <StudioField hint="Paste a drive ID such as b!Abc123 when the drive is not listed. Leave blank for the signed-in user's OneDrive; app-only connections need a drive ID." label="Drive ID">
      <input onChange={(event) => editDriveID(event.target.value)} type="text" value={driveId}/>
    </StudioField>
    {isDriveIDInvalid && <StudioNotice tone="error">Enter a Microsoft Graph drive ID: letters, digits, and ! _ . - only.</StudioNotice>}
    <SaveAction
      disabled={busy || isDriveIDInvalid || (target.required && driveId.trim() === "")}
      onSave={() => onSave({driveId: driveId.trim(), driveName: driveId.trim() === "" ? "" : driveName})}
    />
  </UnitFrame>;
}
