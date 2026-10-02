// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StudioButton, StudioField, StudioNotice, type ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { useState } from "react";
import { canBrowseItem, driveKeyFromDriveID, isGraphID, type GraphFolder } from "./provider.js";
import { SaveAction, TruncatedNotice, UnitFrame, stringValue, type PickedList } from "./unit-frame.js";

export interface FolderPickerProps {
  target: ConnectorStudioConfigurationUnitTarget;
  folders?: PickedList<GraphFolder>;
  busy?: boolean;
  loadError?: string;
  /** onListFolders lists the subfolders of parentFolderId, or of the drive root when it is blank. */
  onListFolders(driveId: string, parentFolderId: string): void;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function FolderPickerUnit({target, folders, busy, loadError, onListFolders, onSave}: FolderPickerProps) {
  const driveId = stringValue(target.value.driveId);
  const [folderId, setFolderId] = useState(stringValue(target.value.folderId));
  const [folderName, setFolderName] = useState(stringValue(target.value.folderName));
  const [openedPath, setOpenedPath] = useState<GraphFolder[]>([]);
  const listed = folders?.items ?? [];
  const canBrowse = driveKeyFromDriveID(driveId) !== "";
  const isFolderIDInvalid = folderId.trim() !== "" && !isGraphID(folderId);
  const openedFolder = openedPath.at(-1);
  const chooseFolder = (value: string) => {
    setFolderId(value);
    setFolderName([...listed, ...openedPath].find((folder) => folder.id === value.trim())?.name ?? "");
  };
  const openFolder = (folder: GraphFolder | undefined) => {
    const path = folder ? [...openedPath, folder] : [];
    setOpenedPath(path);
    onListFolders(driveId, folder?.id ?? "");
  };
  const selectedFolder = listed.find((folder) => folder.id === folderId.trim());
  return <UnitFrame loadError={loadError} target={target}>
    {!driveId && <StudioNotice tone="info">Save a drive in this Step first; folders are listed from the saved drive.</StudioNotice>}
    {driveId && !canBrowse && <StudioNotice tone="attention">This drive ID cannot be browsed from Dex Web. Paste a folder ID instead.</StudioNotice>}
    <div className="studio-actions">
      <StudioButton disabled={busy || !canBrowse} onClick={() => openFolder(undefined)}>List folders</StudioButton>
      <StudioButton disabled={busy || !canBrowse || !selectedFolder || !canBrowseItem(selectedFolder.id)} onClick={() => openFolder(selectedFolder)}>Open selected folder</StudioButton>
      {folderName && <span className="studio-muted"><strong>Folder:</strong> {folderName}</span>}
    </div>
    {openedPath.length > 0 && <span className="studio-muted">Listing inside: {openedPath.map((folder) => folder.name).join(" / ")}</span>}
    {listed.length > 0 && <StudioField label="Folder">
      <select onChange={(event) => chooseFolder(event.target.value)} value={folderId.trim()}>
        <option value="">Drive root</option>
        {openedFolder && <option value={openedFolder.id}>This folder: {openedFolder.name}</option>}
        {listed.map((folder) => <option key={folder.id} value={folder.id}>{folder.name}</option>)}
      </select>
    </StudioField>}
    {folders && listed.length === 0 && <StudioNotice tone="info">This folder has no subfolders.</StudioNotice>}
    <TruncatedNotice isTruncated={folders?.isTruncated ?? false} noun="folders"/>
    <StudioField hint="Paste a folder item ID when the folder is not listed. Leave blank for the root of the drive." label="Folder ID">
      <input onChange={(event) => chooseFolder(event.target.value)} type="text" value={folderId}/>
    </StudioField>
    {isFolderIDInvalid && <StudioNotice tone="error">Enter a Microsoft Graph item ID: letters, digits, and ! _ . - only.</StudioNotice>}
    <SaveAction
      disabled={busy || isFolderIDInvalid || (target.required && folderId.trim() === "")}
      onSave={() => onSave({folderId: folderId.trim(), folderName: folderId.trim() === "" ? "" : folderName})}
    />
  </UnitFrame>;
}
